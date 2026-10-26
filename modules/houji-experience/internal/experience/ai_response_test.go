package experience

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"os"
	"strings"
	"testing"
)

func completedResponse(t *testing.T, style, answer string) string {
	t.Helper()
	var value any
	if style == "responses" {
		value = map[string]any{"status": "completed", "output": []any{map[string]any{"type": "message", "content": []any{map[string]any{"type": "output_text", "text": answer}}}}}
	} else {
		value = map[string]any{"choices": []any{map[string]any{"finish_reason": "stop", "message": map[string]any{"content": answer}}}}
	}
	b, err := json.Marshal(value)
	must(t, err)
	return string(b)
}

func TestAIResponseAboveOldByteLimitPersists(t *testing.T) {
	answer := strings.Repeat("Đủ dữ liệu. ", 180000)
	for _, style := range []string{"responses", "chat_completions"} {
		t.Run(style, func(t *testing.T) {
			a, f := fixture(t)
			c := aiConfig("https://example.test/v1", style)
			c.MaxOutput = 0
			must(t, a.saveAI(c))
			body := completedResponse(t, style, pretty(map[string]any{"answer": answer, "actions": []AIAction{}}))
			if len(body) <= 2*1024*1024 || len(body) > maxAIResponseBytes {
				t.Fatal("fixture does not exercise previous limit")
			}
			calls := 0
			tr := transportFunc(func(*http.Request) (*http.Response, error) {
				calls++
				return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(body)), Header: http.Header{}}, nil
			})
			job, err := a.startAIJob(Request{Mode: "test"}, func(string) error { return nil })
			must(t, err)
			must(t, a.RunAIJob(context.Background(), job["id"].(string), tr))
			status, err := a.AIJobStatus(job["id"].(string), false)
			must(t, err)
			result := status["result"].(map[string]any)
			if result["source"] != "api" || strings.TrimSpace(result["answer"].(string)) != strings.TrimSpace(answer) || calls != 1 || f.writes != 0 {
				t.Fatal("large completed answer lost or retried", result["ai_error"], calls)
			}
			var config AIConfig
			must(t, readJSON(a.state("ai.json"), &config))
			if config.MaxOutput != 0 {
				t.Fatal("changed provider token policy")
			}
		})
	}
}

func TestAIResponseByteBoundary(t *testing.T) {
	for _, style := range []string{"responses", "chat_completions"} {
		for _, excess := range []int{0, 1, 4096} {
			a, f := fixture(t)
			must(t, a.saveAI(aiConfig("https://example.test/v1", style)))
			prefix := completedResponse(t, style, "Hoàn tất")
			body := strings.NewReader(prefix + strings.Repeat(" ", maxAIResponseBytes-len(prefix)+excess))
			calls := 0
			tr := transportFunc(func(*http.Request) (*http.Response, error) {
				calls++
				return &http.Response{StatusCode: 200, Body: io.NopCloser(body), Header: http.Header{}}, nil
			})
			result := a.requestAI(context.Background(), "", true, "", "", nil, tr)
			if excess == 0 {
				if result["source"] != "api" {
					t.Fatal(style, "exact byte limit rejected", result["ai_error"])
				}
			} else if result["ai_error"] != "response-too-large" || result["response_limit_bytes"] != maxAIResponseBytes || result["answer"] != nil || body.Len() != excess-1 {
				t.Fatal("oversized body accepted or read past bound", result["ai_error"], body.Len())
			}
			if calls != 1 || f.writes != 0 {
				t.Fatal("retried or applied settings")
			}
		}
	}
}

type aiBrokenReader struct{ err error }

func (r aiBrokenReader) Read([]byte) (int, error) { return 0, r.err }

func TestAIReadErrorsAreNotSizeErrorsOrCompletedAdvice(t *testing.T) {
	for _, test := range []struct {
		err  error
		code string
	}{
		{io.ErrUnexpectedEOF, "response-read-failed"},
		{errors.New("test-secret network details"), "response-read-failed"},
		{context.DeadlineExceeded, "timeout"},
		{&net.DNSError{IsTimeout: true}, "timeout"},
		{context.Canceled, "cancelled"},
	} {
		a, f := fixture(t)
		must(t, a.saveAI(aiConfig("https://example.test/v1", "chat_completions")))
		calls := 0
		tr := transportFunc(func(*http.Request) (*http.Response, error) {
			calls++
			// A complete-looking JSON prefix must still be discarded on a read error.
			body := io.MultiReader(strings.NewReader(completedResponse(t, "chat_completions", "PARTIAL")), aiBrokenReader{test.err})
			return &http.Response{StatusCode: 200, Body: io.NopCloser(body), Header: http.Header{}}, nil
		})
		r := a.askWithTransport(context.Background(), "", false, tr)
		log, err := os.ReadFile(a.state("events.log"))
		must(t, err)
		if r["ai_error"] != test.code || calls != 1 || f.writes != 0 || strings.Contains(pretty(r)+string(log), "test-secret") || r["answer"] != nil {
			t.Fatal("incorrect read failure handling", r["ai_error"], calls)
		}
	}
}
