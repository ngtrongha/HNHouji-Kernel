package experience

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"time"
)

// One private job, overwritten only after completion. Nothing sensitive is put
// in process arguments; the detached child receives only a random job ID.
type AIJob struct {
	ID       string    `json:"id"`
	Mode     string    `json:"mode"`
	State    string    `json:"state"`
	Started  time.Time `json:"started"`
	Deadline time.Time `json:"deadline"`
	Boot     string    `json:"boot"`
	PID      int       `json:"pid"`
	Cancel   bool      `json:"cancel"`
	Input    *Request  `json:"input,omitempty"`
	Result   any       `json:"result,omitempty"`
	Error    string    `json:"error,omitempty"`
}

func readAIJSON(path string, value any) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	b, err := io.ReadAll(io.LimitReader(f, maxAIStateBytes+1))
	if err != nil {
		return err
	}
	if len(b) > maxAIStateBytes {
		return errors.New("ai-state-too-large")
	}
	return json.Unmarshal(b, value)
}
func activeJob(j AIJob) bool {
	return j.State == "queued" || j.State == "running" || j.State == "cancelling"
}
func (a *App) jobView(j AIJob) map[string]any {
	return map[string]any{"id": j.ID, "mode": j.Mode, "state": j.State, "started": j.Started, "elapsed_ms": time.Since(j.Started).Milliseconds(), "result": j.Result, "error": j.Error}
}
func (a *App) reconcileJob(j *AIJob) {
	if activeJob(*j) && (j.Boot != a.bootID() || time.Now().After(j.Deadline) || (j.PID > 0 && !processAlive(j.PID)) || (j.State == "queued" && time.Since(j.Started) > 15*time.Second)) {
		j.State = "failed"
		j.Error = "ai-job-interrupted"
		j.Input = nil
	}
}
func (a *App) startAIJob(r Request, launch func(string) error) (map[string]any, error) {
	if r.Mode != "ask" && r.Mode != "test" && r.Mode != "daily" {
		return nil, errors.New("invalid AI mode")
	}
	if len(r.Prompt) > 8192 || len(r.Messages) > 6 {
		return nil, errors.New("prompt-too-long")
	}
	u, err := a.lock("ai-job")
	if err != nil {
		return nil, err
	}
	defer u()
	var old AIJob
	if e := readAIJSON(a.state("ai-job.json"), &old); e != nil && !os.IsNotExist(e) {
		return nil, e
	}
	a.reconcileJob(&old)
	if activeJob(old) {
		return nil, errors.New("ai-job-busy")
	}
	var c AIConfig
	_ = readJSON(a.state("ai.json"), &c)
	if c.validate() != nil {
		c.Timeout = 180
	}
	var bytes [16]byte
	if _, err = rand.Read(bytes[:]); err != nil {
		return nil, err
	}
	input := Request{Prompt: r.Prompt, Goal: r.Goal, Messages: r.Messages}
	j := AIJob{ID: hex.EncodeToString(bytes[:]), Mode: r.Mode, State: "queued", Started: time.Now().UTC(), Boot: a.bootID(), Input: &input}
	j.Deadline = j.Started.Add(time.Duration(c.Timeout+30) * time.Second)
	if err = atomicJSON(a.state("ai-job.json"), j); err != nil {
		return nil, err
	}
	if err = launch(j.ID); err != nil {
		j.State = "failed"
		j.Error = "ai-worker-unavailable"
		j.Input = nil
		_ = atomicJSON(a.state("ai-job.json"), j)
		return a.jobView(j), nil
	}
	return a.jobView(j), nil
}
func (a *App) AIJobStatus(id string, cancel bool) (map[string]any, error) {
	u, err := a.lock("ai-job")
	if err != nil {
		return nil, err
	}
	defer u()
	var j AIJob
	if err = readAIJSON(a.state("ai-job.json"), &j); os.IsNotExist(err) {
		return map[string]any{"state": "idle"}, nil
	} else if err != nil {
		return nil, err
	}
	if id != "" && id != j.ID {
		return nil, errors.New("ai-job-not-found")
	}
	a.reconcileJob(&j)
	if cancel && activeJob(j) {
		j.Cancel = true
		if j.State == "queued" {
			j.State = "cancelled"
			j.Input = nil
		} else {
			j.State = "cancelling"
		}
	}
	if cancel || !activeJob(j) {
		if err = atomicJSON(a.state("ai-job.json"), j); err != nil {
			return nil, err
		}
	}
	return a.jobView(j), nil
}
func (a *App) RunAIJob(ctx context.Context, id string, tr http.RoundTripper) error {
	worker, err := a.lock("ai-worker")
	if err != nil {
		return err
	}
	defer worker()
	u, err := a.lock("ai-job")
	if err != nil {
		return err
	}
	var j AIJob
	err = readAIJSON(a.state("ai-job.json"), &j)
	if err != nil || j.ID != id || j.State != "queued" || j.Boot != a.bootID() || time.Now().After(j.Deadline) {
		u()
		return errors.New("ai-job-not-queued")
	}
	j.State = "running"
	j.PID = os.Getpid()
	input := j.Input
	j.Input = nil
	if err = atomicJSON(a.state("ai-job.json"), j); err != nil {
		u()
		return err
	}
	u()
	if input == nil {
		return errors.New("ai-job-input-missing")
	}
	ctx, cancel := context.WithDeadline(ctx, j.Deadline)
	defer cancel()
	done := make(chan struct{})
	defer close(done)
	go func() {
		ticker := time.NewTicker(250 * time.Millisecond)
		defer ticker.Stop()
		for {
			select {
			case <-done:
				return
			case <-ctx.Done():
				return
			case <-ticker.C:
				var current AIJob
				if readAIJSON(a.state("ai-job.json"), &current) != nil || current.ID != id || current.Cancel || a.moduleDisabled() {
					cancel()
					return
				}
			}
		}
	}()
	var result any
	var workErr error
	switch j.Mode {
	case "test":
		result = a.askWithTransport(ctx, "", true, tr)
	case "ask":
		goal := input.Goal
		if goal == "" {
			goal = "daily"
		}
		result = a.advisorWithTransport(ctx, input.Prompt, goal, input.Messages, tr)
	case "daily":
		result, workErr = a.dailyAnalyze(ctx, time.Now(), false, tr)
	}
	u, err = a.lock("ai-job")
	if err != nil {
		return err
	}
	defer u()
	var current AIJob
	if readAIJSON(a.state("ai-job.json"), &current) != nil || current.ID != id {
		return nil
	}
	if current.Cancel || a.moduleDisabled() {
		current.State = "cancelled"
		current.Result = nil
		current.Error = ""
	} else if workErr != nil {
		current.State = "failed"
		current.Error = workErr.Error()
	} else {
		current.State = "completed"
		current.Result = result
	}
	current.Input = nil
	current.PID = 0
	return atomicJSON(a.state("ai-job.json"), current)
}
func (a *App) launchAIWorker(id string) error {
	return startDetached(filepath.Join(a.Paths.Module, "bin", "houjictl"), id)
}
