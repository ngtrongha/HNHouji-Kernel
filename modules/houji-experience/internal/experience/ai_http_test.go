package experience

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"strings"
	"testing"
)

func TestHTTPDiagnosticsUseCompleteErrorEvidenceWithoutLeakingOrRetrying(t *testing.T) {
	for _, style := range []string{"chat_completions", "responses"} {
		for _, test := range []struct {
			name, body, hint string
			status           int
			broken           bool
		}{
			{"unknown-404", `{"error":{"message":"test-secret Not Found"}}`, "", 404, false},
			{"routing", `{"error":{"message":"No endpoints found that can handle the requested parameters. test-secret","code":404}}`, "provider-parameters-unavailable", 404, false},
			{"privacy", `{"error":{"message":"No endpoints found matching your data policy. test-secret"}}`, "provider-policy-unavailable", 404, false},
			{"provider", `{"error":{"message":"No endpoints found for private-model"}}`, "provider-unavailable", 404, false},
			{"model", `{"error":{"message":"test-secret","code":"model_not_found"}}`, "model-not-found", 404, false},
			{"schema", `{"error":{"message":"test-secret","param":"response_format"}}`, "schema-rejected", 400, false},
			{"schema-responses", `{"error":{"param":"text.format"}}`, "schema-rejected", 422, false},
			{"not-json", `<html>test-secret</html>`, "", 404, false},
			{"oversized", `{"error":{"param":"response_format"}}` + strings.Repeat(" ", maxAIErrorBytes), "", 400, false},
			{"partial-read", `{"error":{"param":"response_format"}}`, "", 400, true},
		} {
			t.Run(style+"/"+test.name, func(t *testing.T) {
				a, f := fixture(t)
				must(t, a.saveAI(aiConfig("https://openrouter.ai/api/v1", style)))
				calls := 0
				tr := transportFunc(func(*http.Request) (*http.Response, error) {
					calls++
					var body io.Reader = strings.NewReader(test.body)
					if test.broken {
						body = io.MultiReader(body, aiBrokenReader{io.ErrUnexpectedEOF})
					}
					return &http.Response{StatusCode: test.status, Body: io.NopCloser(body), Header: http.Header{}}, nil
				})
				r := a.askWithTransport(context.Background(), "", true, tr)
				hint, _ := r["api_error_hint"].(string)
				if hint != test.hint || r["source"] != "local-rules" || r["structured_output_verified"] != nil || r["structured_output_hint"] != nil || calls != 1 || f.writes != 0 {
					t.Fatal(r, calls)
				}
				log, _ := os.ReadFile(a.state("events.log"))
				if strings.Contains(pretty(r)+string(log), "test-secret") || strings.Contains(pretty(r), "private-model") || exists(a.state("ai-proposal.json")) {
					t.Fatal("provider error leaked or created plan")
				}
			})
		}
	}
}

func TestConnectionProbeRequiresSchemaAndCannotProposeSettings(t *testing.T) {
	for _, style := range []string{"chat_completions", "responses"} {
		for _, answer := range []string{`{"answer":"ok","actions":[]}`, `just text`, "```json\n{\"answer\":\"ok\",\"actions\":[]}\n```", `{"answer":"ok","actions":[{"kind":"kernel_preset","enabled":false,"reason":"change"}]}`} {
			a, f := fixture(t)
			c := aiConfig("https://openrouter.ai/api/v1", style)
			c.Model = "openrouter/free"
			must(t, a.saveAI(c))
			calls := 0
			tr := transportFunc(func(req *http.Request) (*http.Response, error) {
				calls++
				b, err := io.ReadAll(req.Body)
				must(t, err)
				var payload map[string]any
				must(t, json.Unmarshal(b, &payload))
				if payload["provider"].(map[string]any)["require_parameters"] != true || !strings.Contains(string(b), "json_schema") || strings.Contains(string(b), "com.example") || strings.Contains(string(b), "MemAvailable") || strings.Contains(string(b), "test-secret") {
					t.Fatal("probe omitted contract or leaked device data")
				}
				if style == "chat_completions" && (payload["max_tokens"] != float64(512) || payload["max_completion_tokens"] != nil) {
					t.Fatal("wrong OpenRouter token parameter")
				}
				return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(completedResponse(t, style, answer))), Header: http.Header{}}, nil
			})
			r := a.askWithTransport(context.Background(), "", true, tr)
			if answer == `{"answer":"ok","actions":[]}` {
				if r["source"] != "api" || r["answer"] != "ok" || r["structured_output_verified"] != true {
					t.Fatal(r)
				}
			} else if r["source"] != "local-rules" || r["ai_error"] != "schema-probe-invalid" || r["answer"] != nil {
				t.Fatal(r)
			}
			if calls != 1 || f.writes != 0 || exists(a.state("ai-proposal.json")) || exists(a.state("ai-application.json")) {
				t.Fatal("probe applied or retried")
			}
		}
	}
}
