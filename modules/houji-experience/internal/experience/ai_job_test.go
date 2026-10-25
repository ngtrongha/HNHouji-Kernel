package experience

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"
)

func jobTransport(answer string) transportFunc {
	return func(*http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(`{"choices":[{"finish_reason":"stop","message":{"content":"` + answer + `"}}]}`)), Header: http.Header{}}, nil
	}
}
func TestAIJobQueuePrivacyAndSingleExecution(t *testing.T) {
	a, f := fixture(t)
	must(t, a.saveAI(aiConfig("https://example.test/v1", "chat_completions")))
	var launched string
	start := time.Now()
	job, err := a.startAIJob(Request{Mode: "ask", Goal: "multitask", Prompt: "private question"}, func(id string) error { launched = id; return nil })
	must(t, err)
	if launched != job["id"] || time.Since(start) > time.Second || strings.Contains(pretty(job), "private question") {
		t.Fatal(job)
	}
	if _, err = a.startAIJob(Request{Mode: "test"}, func(string) error { t.Fatal("duplicate launch"); return nil }); err == nil {
		t.Fatal("accepted duplicate job")
	}
	must(t, a.RunAIJob(context.Background(), launched, jobTransport("## Nhận định")))
	status, err := a.AIJobStatus(launched, false)
	must(t, err)
	if status["state"] != "completed" || f.writes != 0 || strings.Contains(pretty(status), "private question") {
		t.Fatal(status)
	}
	if err = a.RunAIJob(context.Background(), launched, jobTransport("never")); err == nil {
		t.Fatal("re-executed completed job")
	}
}
func TestAIJobCancellationStopsHTTPAndQueuedWork(t *testing.T) {
	a, _ := fixture(t)
	must(t, a.saveAI(aiConfig("https://example.test/v1", "chat_completions")))
	job, err := a.startAIJob(Request{Mode: "test"}, func(string) error { return nil })
	must(t, err)
	id := job["id"].(string)
	status, err := a.AIJobStatus(id, true)
	must(t, err)
	if status["state"] != "cancelled" {
		t.Fatal(status)
	}
	job, err = a.startAIJob(Request{Mode: "test"}, func(string) error { return nil })
	must(t, err)
	id = job["id"].(string)
	entered := make(chan struct{})
	done := make(chan error, 1)
	tr := transportFunc(func(req *http.Request) (*http.Response, error) {
		close(entered)
		<-req.Context().Done()
		return nil, req.Context().Err()
	})
	go func() { done <- a.RunAIJob(context.Background(), id, tr) }()
	<-entered
	if _, err = a.AIJobStatus("wrong-id", true); err == nil {
		t.Fatal("wrong ID could cancel")
	}
	_, err = a.AIJobStatus(id, true)
	must(t, err)
	select {
	case err = <-done:
		must(t, err)
	case <-time.After(3 * time.Second):
		t.Fatal("HTTP not cancelled")
	}
	status, err = a.AIJobStatus(id, false)
	must(t, err)
	if status["state"] != "cancelled" || status["result"] != nil {
		t.Fatal(status)
	}
}
func TestAIJobInterruptedOrLaunchFailedNeverRetries(t *testing.T) {
	a, _ := fixture(t)
	job, err := a.startAIJob(Request{Mode: "test"}, func(string) error { return errors.New("no worker") })
	must(t, err)
	if job["state"] != "failed" {
		t.Fatal(job)
	}
	job, err = a.startAIJob(Request{Mode: "test"}, func(string) error { return nil })
	must(t, err)
	var stored AIJob
	must(t, readAIJSON(a.state("ai-job.json"), &stored))
	stored.Boot = "old-boot"
	must(t, atomicJSON(a.state("ai-job.json"), stored))
	status, err := a.AIJobStatus(job["id"].(string), false)
	must(t, err)
	if status["state"] != "failed" || status["error"] != "ai-job-interrupted" {
		t.Fatal(status)
	}
}
func TestAdvisorUsesComparisonsGoalAndConversationWithoutPackageNames(t *testing.T) {
	a, _ := fixture(t)
	seedUsage(t, a, time.Now().UTC())
	must(t, a.saveAI(aiConfig("https://example.test/v1", "chat_completions")))
	tr := transportFunc(func(req *http.Request) (*http.Response, error) {
		b, _ := io.ReadAll(req.Body)
		body := string(b)
		for _, want := range []string{"daily_comparison", "multitask", "Đa nhiệm", "mean", "Câu trước"} {
			if !strings.Contains(body, want) && want != "multitask" {
				t.Fatal("missing evidence", want)
			}
		}
		if strings.Contains(body, "com.example") || strings.Contains(body, "test-secret") || strings.Contains(body, "tối đa 180 từ") {
			t.Fatal("private or constrained body")
		}
		return jobTransport("Đề xuất có bằng chứng")(req)
	})
	result := a.advisorWithTransport(context.Background(), "Phân tích test-secret", "multitask", []ChatMessage{{Role: "user", Content: "Câu trước"}}, tr)
	if result["source"] != "api" {
		t.Fatal(result)
	}
}
