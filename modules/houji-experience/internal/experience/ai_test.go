package experience

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
)

type transportFunc func(*http.Request) (*http.Response, error)

func (f transportFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
func aiConfig(base, style string) AIConfig {
	return AIConfig{Enabled: true, Style: style, BaseURL: base, Model: "test-model", Auth: "bearer", Key: "test-secret", MaxOutput: 512, Timeout: 5}
}

func TestAIAbsentDisabledNoNetwork(t *testing.T) {
	a, _ := fixture(t)
	calls := 0
	tr := transportFunc(func(*http.Request) (*http.Response, error) { calls++; t.Fatal("unexpected network"); return nil, nil })
	if r := a.askWithTransport(context.Background(), "help", false, tr); r["source"] != "local-rules" {
		t.Fatal(r)
	}
	c := aiConfig("https://example.test/v1", "responses")
	c.Enabled = false
	must(t, a.saveAI(c))
	a.askWithTransport(context.Background(), "help", false, tr)
	if calls != 0 {
		t.Fatal(calls)
	}
}
func TestAIProtocolsAndPrivacy(t *testing.T) {
	for _, style := range []string{"chat_completions", "responses"} {
		t.Run(style, func(t *testing.T) {
			a, _ := fixture(t)
			calls := 0
			server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls++
				b, _ := io.ReadAll(r.Body)
				if strings.Contains(string(b), "com.example") || strings.Contains(string(b), "test-secret") {
					t.Error("private data in body")
				}
				if r.Header.Get("Authorization") != "Bearer test-secret" {
					t.Error("missing auth")
				}
				var payload map[string]any
				if json.Unmarshal(b, &payload) != nil || payload["model"] != "test-model" {
					t.Error("invalid body")
				}
				w.Header().Set("Content-Type", "application/json")
				if style == "responses" {
					if r.URL.Path != "/v1/responses" || payload["store"] != false {
						t.Error(payload)
					}
					io.WriteString(w, `{"output":[{"type":"message","content":[{"type":"output_text","text":"<script>bad()</script> test-secret"}]}],"usage":{"input_tokens":12,"output_tokens":4}}`)
				} else {
					if r.URL.Path != "/v1/chat/completions" {
						t.Error(r.URL.Path)
					}
					io.WriteString(w, `{"choices":[{"message":{"content":"<script>bad()</script> test-secret"}}],"usage":{"total_tokens":16}}`)
				}
			}))
			defer server.Close()
			must(t, a.saveAI(aiConfig(server.URL+"/v1", style)))
			result := a.askWithTransport(context.Background(), "Có áp lực RAM không?", false, server.Client().Transport)
			if result["source"] != "api" || calls != 1 {
				t.Fatal(result, calls)
			}
			if strings.Contains(result["answer"].(string), "test-secret") {
				t.Fatal("echoed credential")
			}
			status := pretty(a.AIStatus())
			log, _ := os.ReadFile(a.state("events.log"))
			if strings.Contains(status+string(log), "test-secret") {
				t.Fatal("secret leak")
			}
		})
	}
}
func TestAITestOmitsTelemetry(t *testing.T) {
	a, _ := fixture(t)
	must(t, a.saveAI(aiConfig("https://example.test/v1", "chat_completions")))
	tr := transportFunc(func(r *http.Request) (*http.Response, error) {
		b, _ := io.ReadAll(r.Body)
		if strings.Contains(string(b), "metrics") || strings.Contains(string(b), "MemTotal") {
			t.Fatal("test sends device report")
		}
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(`{"choices":[{"message":{"content":"ok"}}]}`)), Header: http.Header{}}, nil
	})
	if r := a.askWithTransport(context.Background(), "", true, tr); r["source"] != "api" {
		t.Fatal(r)
	}
}
func TestAPIFailuresDoNotRetry(t *testing.T) {
	for _, code := range []int{301, 401, 403, 429, 500, 503} {
		t.Run(http.StatusText(code), func(t *testing.T) {
			a, f := fixture(t)
			must(t, a.saveAI(aiConfig("https://example.test/v1", "responses")))
			calls := 0
			tr := transportFunc(func(*http.Request) (*http.Response, error) {
				calls++
				return &http.Response{StatusCode: code, Body: io.NopCloser(strings.NewReader(`test-secret server internals`)), Header: http.Header{}}, nil
			})
			r := a.askWithTransport(context.Background(), "", false, tr)
			if r["source"] != "local-rules" || calls != 1 || f.writes != 0 {
				t.Fatal(r, calls)
			}
			if strings.Contains(pretty(r), "test-secret") {
				t.Fatal("error body leak")
			}
		})
	}
}
func TestInvalidAndLargeResponses(t *testing.T) {
	for _, body := range []string{`{broken`, `{}`, strings.Repeat("x", 128*1024+1)} {
		a, _ := fixture(t)
		must(t, a.saveAI(aiConfig("https://example.test/v1", "responses")))
		tr := transportFunc(func(*http.Request) (*http.Response, error) {
			return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(body)), Header: http.Header{}}, nil
		})
		if r := a.askWithTransport(context.Background(), "", false, tr); r["source"] != "local-rules" {
			t.Fatal(r)
		}
	}
}
func TestHTTPSRequiredAndRedirectNotFollowed(t *testing.T) {
	a, _ := fixture(t)
	for _, url := range []string{"http://example.test/v1", "https://user:key@example.test/v1", "https://example.test/v1?key=x", "https://example.test/v1#secret"} {
		if a.saveAI(aiConfig(url, "responses")) == nil {
			t.Fatal("accepted", url)
		}
	}
	destCalls := 0
	dest := httptest.NewTLSServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { destCalls++ }))
	defer dest.Close()
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { http.Redirect(w, r, dest.URL, 307) }))
	defer server.Close()
	must(t, a.saveAI(aiConfig(server.URL, "responses")))
	if r := a.askWithTransport(context.Background(), "", true, server.Client().Transport); r["source"] != "local-rules" || destCalls != 0 {
		t.Fatal("followed redirect", r)
	}
}
func TestUntrustedCertificateFails(t *testing.T) {
	a, _ := fixture(t)
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { t.Error("should not reach untrusted server") }))
	defer server.Close()
	must(t, a.saveAI(aiConfig(server.URL, "responses")))
	if r := a.askWithTransport(context.Background(), "", true, http.DefaultTransport); r["ai_error"] != "network-or-tls" {
		t.Fatal(r)
	}
}
func TestTimeoutAndConcurrentRequest(t *testing.T) {
	a, _ := fixture(t)
	must(t, a.saveAI(aiConfig("https://example.test/v1", "responses")))
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	tr := transportFunc(func(r *http.Request) (*http.Response, error) { return nil, r.Context().Err() })
	if r := a.askWithTransport(ctx, "", false, tr); r["ai_error"] != "timeout" {
		t.Fatal(r)
	}
	unlock, e := a.lock("ai")
	must(t, e)
	defer unlock()
	if r := a.askWithTransport(context.Background(), "", false, tr); r["ai_error"] != "busy" {
		t.Fatal(r)
	}
}
func TestChangingEndpointDoesNotReuseSecret(t *testing.T) {
	a, _ := fixture(t)
	c := aiConfig("https://one.example/v1", "responses")
	must(t, a.saveAI(c))
	c.Key = ""
	must(t, a.saveAI(c))
	c.BaseURL = "https://two.example/v1"
	if a.saveAI(c) == nil {
		t.Fatal("secret silently moved to new endpoint")
	}
}
func TestKeySpecialCharactersStayJSONData(t *testing.T) {
	a, _ := fixture(t)
	c := aiConfig("https://example.test/v1", "responses")
	c.Key = "key'$(touch /tmp/never);\"&"
	must(t, a.saveAI(c))
	if strings.Contains(pretty(a.AIStatus()), c.Key) {
		t.Fatal("key exposed")
	}
	b, _ := os.ReadFile(a.state("ai.json"))
	var stored AIConfig
	must(t, json.Unmarshal(b, &stored))
	if stored.Key != c.Key {
		t.Fatal("key mangled")
	}
}
