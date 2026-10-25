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
	DefaultsVersion int    `json:"defaults_version,omitempty"`
	Enabled         bool   `json:"enabled"`
	Style           string `json:"style"`
	BaseURL         string `json:"base_url"`
	Model           string `json:"model"`
	Auth            string `json:"auth"`
	Key             string `json:"key,omitempty"`
	MaxOutput       int    `json:"max_output_tokens"`
	Timeout         int    `json:"timeout_seconds"`
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
	if c.Timeout == 0 {
		c.Timeout = 180
	}
	if c.MaxOutput < 0 || c.Timeout < 5 || c.Timeout > 900 {
		return errors.New("output tokens must be nonnegative (0 = provider default); timeout 5..900 seconds")
	}
	c.BaseURL = strings.TrimRight(c.BaseURL, "/")
	c.DefaultsVersion = 1
	return nil
}

// One-time migration of the former small defaults. Future explicit budgets
// saved by this version remain explicit, even when they equal an old value.
func (a *App) upgradeAI() error {
	var c AIConfig
	if err := readJSON(a.state("ai.json"), &c); err != nil {
		return nil
	}
	if c.DefaultsVersion >= 1 {
		return nil
	}
	if c.MaxOutput == 512 || c.MaxOutput == 1024 || c.MaxOutput == 2048 {
		c.MaxOutput = 0
	}
	if c.Timeout == 0 || c.Timeout == 30 {
		c.Timeout = 180
	}
	if c.validate() != nil {
		return nil
	}
	return atomicJSON(a.state("ai.json"), c)
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
	return &http.Transport{Proxy: nil, TLSClientConfig: &tls.Config{RootCAs: roots, MinVersion: tls.VersionTLS12}, DisableKeepAlives: true}
}

const advisorInstruction = `Bạn tư vấn dữ liệu cho Xiaomi 14 houji 8GB, HyperOS, kernel r11 GKI 6.1.
Trả lời cuối cùng bằng tiếng Việt, dùng Markdown dễ đọc. Trả lời đủ sâu theo câu hỏi; không giới hạn số từ/gạch đầu dòng, không kể lại quá trình suy nghĩ. Mở đầu bằng kết luận trực tiếp. Dùng các mục Nhận định, Bằng chứng, Đề xuất và Cách kiểm chứng khi phù hợp. Phân biệt số đo và suy luận; trích con số, thời gian/độ phủ. Khi thiếu dữ liệu, vẫn giải thích những gì có thể xác định và chỉ rõ phép đo bổ sung cần thiết.
Chỉ dùng dữ liệu được cung cấp. MemAvailable_kib/1048576 là GiB RAM khả dụng. cpu_load_average_1m là số tác vụ chạy/chờ trung bình, có thể gồm chờ I/O; không phải phần trăm CPU và không đủ để kết luận nghẽn CPU.
psi_some_avg10 và psi_full_avg10 là phần trăm thời gian chờ do áp lực bộ nhớ trong cửa sổ trung bình 10 giây; FULL chỉ thời gian tất cả tác vụ không nhàn rỗi cùng chờ. PSI không đo điện năng hoặc pin chờ; giá trị 0 trong một mẫu không chứng minh hệ thống luôn hết áp lực.
battery_celsius đo nhiệt độ pin, không phải SoC. Một mức phần trăm pin không đủ đo tiêu hao. Không suy diễn benchmark, FPS, tiết kiệm pin hoặc nguyên nhân từ một mẫu. Ngưỡng trong báo cáo chỉ mang tính chẩn đoán.
Không đề nghị sửa WALT, HZ, thermal, MM/ZRAM, DRM, F2FS hoặc kernel pins. Không đưa lệnh shell/yêu cầu thực thi. Nêu rõ điều chưa xác định và đề xuất đo lặp cùng workload.`

func (a *App) Ask(ctx context.Context, prompt string, test bool) map[string]any {
	return a.askWithTransport(ctx, prompt, test, nil)
}

func (a *App) askWithTransport(ctx context.Context, prompt string, test bool, transport http.RoundTripper) map[string]any {
	if !test {
		return a.advisorWithTransport(ctx, prompt, "daily", nil, transport)
	}
	return a.requestAI(ctx, prompt, test, "", "", transport)
}

func (a *App) requestAI(ctx context.Context, prompt string, test bool, instruction, payload string, transport http.RoundTripper) map[string]any {
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
	instructions := "Chỉ trả lời bằng tiếng Việt: Kết nối hoạt động. Không trình bày suy nghĩ."
	if !test && instruction == "" {
		instructions = advisorInstruction
		text = "Câu hỏi: " + prompt + "\nBản tóm tắt được phép gửi:\n" + pretty(a.Report(ctx))
	}
	if instruction != "" {
		instructions = instruction
		text = payload
	}
	if c.Key != "" {
		text = strings.ReplaceAll(text, c.Key, "[REDACTED]")
	}
	var body any
	route := "/chat/completions"
	if c.Style == "responses" {
		route = "/responses"
		body = map[string]any{"model": c.Model, "instructions": instructions, "input": text, "store": false}
		if c.MaxOutput > 0 {
			body.(map[string]any)["max_output_tokens"] = c.MaxOutput
		}
	} else {
		body = map[string]any{"model": c.Model, "messages": []map[string]string{{"role": "system", "content": instructions}, {"role": "user", "content": text}}, "stream": false}
		if c.MaxOutput > 0 {
			body.(map[string]any)["max_completion_tokens"] = c.MaxOutput
		}
		endpoint, _ := url.Parse(c.BaseURL)
		if endpoint.Hostname() == "openrouter.ai" {
			// Excludes returned reasoning, not reasoning compute/token usage.
			// Do not force reasoning off: some routed models require it.
			body.(map[string]any)["reasoning"] = map[string]bool{"exclude": true}
		}
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
	b, e := io.ReadAll(io.LimitReader(res.Body, maxAIBytes+1))
	if e != nil || len(b) > maxAIBytes {
		return fallback("response-too-large")
	}
	var parsed struct {
		Model             string `json:"model"`
		Status            string `json:"status"`
		IncompleteDetails struct {
			Reason string `json:"reason"`
		} `json:"incomplete_details"`
		Choices []struct {
			FinishReason string `json:"finish_reason"`
			Message      struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
		Output []struct {
			Type    string `json:"type"`
			Status  string `json:"status"`
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
	finish := "not-provided"
	if c.Style == "chat_completions" {
		if len(parsed.Choices) > 0 {
			answer = parsed.Choices[0].Message.Content
			if parsed.Choices[0].FinishReason != "" {
				finish = parsed.Choices[0].FinishReason
			}
		}
	} else {
		switch parsed.Status {
		case "completed":
			finish = "stop"
		case "failed":
			finish = "error"
		case "incomplete":
			finish = "incomplete"
			if parsed.IncompleteDetails.Reason == "max_output_tokens" {
				finish = "length"
			}
		default:
			if parsed.Status != "" {
				finish = "unknown"
			}
		}
		for _, item := range parsed.Output {
			if item.Type == "message" {
				if item.Status == "incomplete" && finish != "length" {
					finish = "incomplete"
				}
				if item.Status != "" && item.Status != "completed" && item.Status != "incomplete" {
					finish = "unknown"
				}
				for _, part := range item.Content {
					if part.Type == "output_text" {
						answer += part.Text + "\n"
					}
				}
			}
		}
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
	for _, key := range []string{"completion_tokens_details", "output_tokens_details"} {
		var details struct {
			ReasoningTokens int `json:"reasoning_tokens"`
		}
		if v, ok := parsed.Usage[key]; ok && json.Unmarshal(v, &details) == nil && details.ReasoningTokens >= 0 {
			usage["reasoning_tokens"] = details.ReasoningTokens
		}
	}
	if !map[string]bool{"stop": true, "length": true, "incomplete": true, "error": true, "content_filter": true, "tool_calls": true, "function_call": true, "not-provided": true}[finish] {
		finish = "unknown"
	}
	model := parsed.Model
	if c.Key != "" {
		model = strings.ReplaceAll(model, c.Key, "[REDACTED]")
	}
	if len(model) > 200 || strings.ContainsAny(model, "\r\n") {
		model = ""
	}
	withMetadata := func(result map[string]any) map[string]any {
		result["elapsed_ms"] = time.Since(start).Milliseconds()
		result["usage"] = usage
		result["finish_reason"] = finish
		if model != "" {
			result["model"] = model
		}
		return result
	}
	code := ""
	switch finish {
	case "length":
		code = "response-truncated"
	case "incomplete":
		code = "response-incomplete"
	case "content_filter":
		code = "response-filtered"
	case "error", "tool_calls", "function_call", "unknown", "not-provided":
		code = "response-unusable"
	}
	if code == "" && strings.TrimSpace(answer) == "" {
		code = "response-empty"
	}
	if code != "" {
		a.logEvent("ai-" + code)
		return withMetadata(fallback(code))
	}
	a.logEvent("ai-success")
	return withMetadata(map[string]any{"source": "api", "answer": answer})
}
