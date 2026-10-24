package experience

import (
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"
)

type AIConfig struct {
	Enabled   bool   `json:"enabled"`
	Style     string `json:"style"`
	BaseURL   string `json:"base_url"`
	Model     string `json:"model"`
	Auth      string `json:"auth"`
	Key       string `json:"key,omitempty"`
	MaxOutput int    `json:"max_output_tokens"`
	Timeout   int    `json:"timeout_seconds"`
}

func (c *AIConfig) validate() error {
	if c.Style != "chat_completions" && c.Style != "responses" {
		return errors.New("unsupported API style")
	}
	if c.Auth != "bearer" && c.Auth != "none" {
		return errors.New("unsupported authentication")
	}
	u, e := url.Parse(c.BaseURL)
	if e != nil || u.Scheme != "https" || u.Hostname() == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || len(c.BaseURL) > 2048 {
		return errors.New("base URL must be HTTPS without credentials, query or fragment")
	}
	if strings.ContainsAny(c.Model, "\r\n") || len(c.Model) == 0 || len(c.Model) > 200 {
		return errors.New("invalid model")
	}
	if strings.ContainsAny(c.Key, "\r\n") || len(c.Key) > 4096 || (c.Auth == "bearer" && c.Key == "") {
		return errors.New("invalid or missing API key")
	}
	if c.MaxOutput == 0 {
		c.MaxOutput = 512
	}
	if c.Timeout == 0 {
		c.Timeout = 30
	}
	if c.MaxOutput < 64 || c.MaxOutput > 2048 || c.Timeout < 5 || c.Timeout > 60 {
		return errors.New("output limit 64..2048; timeout 5..60 seconds")
	}
	c.BaseURL = strings.TrimRight(c.BaseURL, "/")
	return nil
}

func (a *App) AIStatus() map[string]any {
	var c AIConfig
	e := readJSON(a.state("ai.json"), &c)
	if e != nil {
		return map[string]any{"configured": false, "enabled": false}
	}
	return map[string]any{"configured": c.validate() == nil, "enabled": c.Enabled, "style": c.Style, "base_url": c.BaseURL, "model": c.Model, "auth": c.Auth, "has_key": c.Key != "", "max_output_tokens": c.MaxOutput, "timeout_seconds": c.Timeout}
}

func (a *App) saveAI(c AIConfig) error {
	if c.Key == "" && c.Auth == "bearer" {
		var old AIConfig
		// A saved key may only be retained for the SAME endpoint and auth method.
		if readJSON(a.state("ai.json"), &old) == nil && strings.TrimRight(c.BaseURL, "/") == old.BaseURL && old.Auth == c.Auth {
			c.Key = old.Key
		}
	}
	if c.Auth == "none" {
		c.Key = ""
	}
	if e := c.validate(); e != nil {
		return e
	}
	return atomicJSON(a.state("ai.json"), c)
}

func androidTransport() *http.Transport {
	roots, e := x509.SystemCertPool()
	if e != nil {
		roots = x509.NewCertPool()
	}
	for _, dir := range []string{"/apex/com.android.conscrypt/cacerts", "/system/etc/security/cacerts"} {
		entries, _ := os.ReadDir(dir)
		for _, entry := range entries {
			if !entry.IsDir() {
				b, e := os.ReadFile(filepath.Join(dir, entry.Name()))
				if e == nil {
					roots.AppendCertsFromPEM(b)
				}
			}
		}
	}
	return &http.Transport{Proxy: nil, TLSClientConfig: &tls.Config{RootCAs: roots, MinVersion: tls.VersionTLS12}, DisableKeepAlives: true, ResponseHeaderTimeout: 30 * time.Second}
}

const advisorInstruction = "Bạn là trợ lý tư vấn cho Xiaomi 14 houji 8GB, HyperOS, kernel r11 GKI 6.1. Chỉ phân tích dữ liệu được cung cấp bằng tiếng Việt. Không suy diễn benchmark hoặc tiết kiệm pin. Không đề nghị sửa WALT, HZ, thermal, MM/ZRAM, DRM, F2FS, kernel pins. Nêu dữ liệu còn thiếu và đề nghị phép đo. Không phát lệnh shell hoặc yêu cầu thực thi. Các ngưỡng trong báo cáo chỉ mang tính chẩn đoán."

func (a *App) Ask(ctx context.Context, prompt string, test bool) map[string]any {
	return a.askWithTransport(ctx, prompt, test, nil)
}

func (a *App) askWithTransport(ctx context.Context, prompt string, test bool, transport http.RoundTripper) map[string]any {
	fallback := func(code string) map[string]any {
		return map[string]any{"source": "local-rules", "ai_error": code, "report": a.Report(ctx)}
	}
	if len(prompt) > 8192 {
		return fallback("prompt-too-long")
	}
	var c AIConfig
	if readJSON(a.state("ai.json"), &c) != nil || c.validate() != nil {
		return fallback("not-configured")
	}
	if !c.Enabled {
		return fallback("disabled")
	}
	if transport == nil {
		transport = androidTransport()
	}
	unlock, e := a.lock("ai")
	if e != nil {
		return fallback("busy")
	}
	defer unlock()
	text := "Trả lời ngắn: kết nối hoạt động."
	instructions := "Return a brief connection confirmation."
	if !test {
		instructions = advisorInstruction
		text = "Câu hỏi: " + prompt + "\nBản tóm tắt được phép gửi:\n" + pretty(a.Report(ctx))
	}
	var body any
	route := "/chat/completions"
	if c.Style == "responses" {
		route = "/responses"
		body = map[string]any{"model": c.Model, "instructions": instructions, "input": text, "max_output_tokens": c.MaxOutput, "store": false}
	} else {
		body = map[string]any{"model": c.Model, "messages": []map[string]string{{"role": "system", "content": instructions}, {"role": "user", "content": text}}, "max_completion_tokens": c.MaxOutput, "stream": false}
	}
	data, _ := json.Marshal(body)
	ctx, cancel := context.WithTimeout(ctx, time.Duration(c.Timeout)*time.Second)
	defer cancel()
	req, e := http.NewRequestWithContext(ctx, http.MethodPost, c.BaseURL+route, bytes.NewReader(data))
	if e != nil {
		return fallback("request-invalid")
	}
	req.Header.Set("Content-Type", "application/json")
	if c.Auth == "bearer" {
		req.Header.Set("Authorization", "Bearer "+c.Key)
	}
	client := &http.Client{Transport: transport, Timeout: time.Duration(c.Timeout) * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	start := time.Now()
	res, e := client.Do(req)
	if e != nil {
		code := "network-or-tls"
		if ctx.Err() != nil {
			code = "timeout"
		}
		a.logEvent("ai-" + code)
		return fallback(code)
	}
	defer res.Body.Close()
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		code := fmt.Sprintf("http-%d", res.StatusCode)
		a.logEvent("ai-" + code)
		return fallback(code)
	}
	b, e := io.ReadAll(io.LimitReader(res.Body, 128*1024+1))
	if e != nil || len(b) > 128*1024 {
		return fallback("response-too-large")
	}
	var parsed struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
		Output []struct {
			Type    string `json:"type"`
			Content []struct {
				Type string `json:"type"`
				Text string `json:"text"`
			} `json:"content"`
		} `json:"output"`
		Usage map[string]json.RawMessage `json:"usage"`
	}
	if json.Unmarshal(b, &parsed) != nil {
		return fallback("response-invalid")
	}
	answer := ""
	if c.Style == "chat_completions" {
		if len(parsed.Choices) > 0 {
			answer = parsed.Choices[0].Message.Content
		}
	} else {
		for _, item := range parsed.Output {
			if item.Type == "message" {
				for _, part := range item.Content {
					if part.Type == "output_text" {
						answer += part.Text + "\n"
					}
				}
			}
		}
	}
	if strings.TrimSpace(answer) == "" {
		return fallback("response-empty")
	}
	// Even a provider that echoes secrets cannot return them to the UI or logs.
	if c.Key != "" {
		answer = strings.ReplaceAll(answer, c.Key, "[REDACTED]")
	}
	usage := map[string]int{}
	for _, key := range []string{"input_tokens", "output_tokens", "total_tokens", "prompt_tokens", "completion_tokens"} {
		var n int
		if v, ok := parsed.Usage[key]; ok && json.Unmarshal(v, &n) == nil && n >= 0 {
			usage[key] = n
		}
	}
	a.logEvent("ai-success")
	return map[string]any{"source": "api", "answer": answer, "elapsed_ms": time.Since(start).Milliseconds(), "usage": usage}
}
