package experience

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"
)

func proposalTransport(answer string, actions []AIAction) transportFunc {
	return func(*http.Request) (*http.Response, error) {
		content, _ := json.Marshal(map[string]any{"answer": answer, "actions": actions})
		body, _ := json.Marshal(map[string]any{"choices": []any{map[string]any{"finish_reason": "stop", "message": map[string]any{"content": string(content)}}}})
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(string(body))), Header: http.Header{}}, nil
	}
}
func seedProposal(t *testing.T, a *App, profile string) AIProposal {
	t.Helper()
	c, err := a.config()
	must(t, err)
	l, err := a.learningConfig()
	must(t, err)
	p, err := makeProposal([]AIAction{{Kind: "app_profile", AppID: "app-01", Profile: profile, Reason: "Đối chiếu thời gian và mức hỗ trợ"}}, []HabitApp{{ID: "app-01", Package: "com.example.game", GameSupported: true}}, c, l.Generation, a.bootID(), time.Now())
	must(t, err)
	must(t, atomicJSON(a.state("ai-proposal.json"), p))
	return p
}
func TestProposalManualApplyRuntimeAndUndoPreserveOtherSettings(t *testing.T) {
	a, f := fixture(t)
	ctx := context.Background()
	p := seedProposal(t, a, "native-game")
	c, _ := a.config()
	if len(c.Apps) != 0 {
		t.Fatal("proposal already applied")
	}
	r, err := a.applyProposal(ctx, p.ID, false)
	must(t, err)
	if r.Status != "applied" || r.Automatic {
		t.Fatal(r)
	}
	c, _ = a.config()
	if c.Apps["com.example.game"] != "native-game" || c.AIProfiles["com.example.game"] != "native-game" {
		t.Fatal(c)
	}
	gen := c.Generation
	_, err = a.applyProposal(ctx, p.ID, false)
	must(t, err)
	c, _ = a.config()
	if c.Generation != gen {
		t.Fatal("duplicate application")
	}
	must(t, a.Step(ctx, "com.example.game", false))
	if f.game != "performance" {
		t.Fatal(f.game)
	}
	c, _ = a.config()
	c.Protected["com.example.chat"] = true
	c.Generation++
	must(t, atomicJSON(a.state("config.json"), c))
	r, err = a.undoAI(ctx)
	must(t, err)
	c, _ = a.config()
	if r.Status != "undone" || c.Apps["com.example.game"] != "" || !c.Protected["com.example.chat"] || f.game != "standard" {
		t.Fatal(r, c, f.game)
	}
}
func TestProposalRejectsStaleUnknownKernelAndExternalUndo(t *testing.T) {
	a, _ := fixture(t)
	ctx := context.Background()
	p := seedProposal(t, a, "daily")
	c, _ := a.config()
	c.Generation++
	must(t, atomicJSON(a.state("config.json"), c))
	if _, err := a.applyProposal(ctx, p.ID, false); err == nil || err.Error() != "proposal-settings-changed" {
		t.Fatal(err)
	}
	p = seedProposal(t, a, "daily")
	must(t, os.Remove(a.system("/data/adb/houji-kernel/manifest.json")))
	if _, err := a.applyProposal(ctx, p.ID, false); err == nil || err.Error() != "proposal-kernel-mismatch" {
		t.Fatal(err)
	}
	seedKernel(t, a)
	_, err := a.applyProposal(ctx, p.ID, false)
	must(t, err)
	response := a.Dispatch(ctx, Request{Action: "app_set", Package: "com.example.game", Profile: "native-game"})
	if !response.OK {
		t.Fatal(response)
	}
	if _, err = a.undoAI(ctx); err == nil || err.Error() != "proposal-undo-conflict" {
		t.Fatal(err)
	}
	c, _ = a.config()
	if c.Apps["com.example.game"] != "native-game" {
		t.Fatal("overwrote user change")
	}
}
func TestProposalStructuredParsingDoesNotAcceptCommandsOrUnknownApps(t *testing.T) {
	for _, body := range []string{`{"answer":"ok","actions":[{"kind":"shell","reason":"x"}]}`, `{"answer":"ok","actions":[{"kind":"app_profile","app_id":"app-01","profile":"daily","command":"reboot","reason":"x"}]}`, `{"answer":"ok","actions":[]} {}`, `{"answer":"ok","actions":[{"kind":"kernel_preset","enabled":1,"reason":"x"}]}`} {
		a, f := fixture(t)
		c, _ := a.config()
		result := a.attachProposal(map[string]any{"source": "api", "answer": body}, nil, c, 0, time.Now())
		if result["proposal"] != nil || f.writes != 0 {
			t.Fatal(result)
		}
	}
	a, _ := fixture(t)
	c, _ := a.config()
	_, err := makeProposal([]AIAction{{Kind: "app_profile", AppID: "app-99", Profile: "native-game", Reason: "x"}}, nil, c, 0, a.bootID(), time.Now())
	if err == nil {
		t.Fatal("unknown ID accepted")
	}
}
func TestProposalAutoRequiresOptInHistoryAndRespectsManualRules(t *testing.T) {
	a, _ := fixture(t)
	ctx := context.Background()
	p := seedProposal(t, a, "native-game")
	if _, err := a.applyProposal(ctx, p.ID, true); err == nil || err.Error() != "proposal-auto-disabled" {
		t.Fatal(err)
	}
	l := defaultLearningConfig()
	l.DailyAI = true
	l.AutoApply = true
	l.Hour = 0
	must(t, a.saveLearning(l))
	p = seedProposal(t, a, "native-game")
	if _, err := a.applyProposal(ctx, p.ID, true); err == nil || err.Error() != "proposal-auto-needs-history" {
		t.Fatal(err)
	}
	seedUsage(t, a, time.Now().UTC())
	r, err := a.applyProposal(ctx, p.ID, true)
	must(t, err)
	if !r.Automatic || r.Status != "applied" {
		t.Fatal(r)
	}
	p = seedProposal(t, a, "daily")
	_, err = a.applyProposal(ctx, p.ID, true)
	must(t, err)
	response := a.Dispatch(ctx, Request{Action: "app_set", Package: "com.example.game", Profile: "native-game"})
	if !response.OK {
		t.Fatal(response)
	}
	p = seedProposal(t, a, "daily")
	if _, err = a.applyProposal(ctx, p.ID, true); err == nil || err.Error() != "proposal-manual-rule" {
		t.Fatal(err)
	}
	_, err = a.applyProposal(ctx, p.ID, false)
	must(t, err)
}
func TestDailyAutoApplyOnlyRunsForScheduledAnalysis(t *testing.T) {
	for _, automatic := range []bool{false, true} {
		t.Run(enabledText(automatic), func(t *testing.T) {
			a, _ := fixture(t)
			now := time.Now().UTC()
			seedUsage(t, a, now)
			l := defaultLearningConfig()
			l.DailyAI = true
			l.AutoApply = true
			l.Hour = 0
			must(t, a.saveLearning(l))
			must(t, a.saveAI(aiConfig("https://example.test/v1", "chat_completions")))
			tr := proposalTransport("Đề xuất dựa trên thống kê", []AIAction{{Kind: "app_profile", AppID: "app-01", Profile: "native-game", Reason: "Có hỗ trợ performance"}})
			r, err := a.dailyAnalyze(context.Background(), now, automatic, tr)
			must(t, err)
			if r.Result["proposal"] == nil {
				t.Fatal(r)
			}
			c, _ := a.config()
			if automatic != (c.Apps["com.example.game"] == "native-game") {
				t.Fatal(c, automatic)
			}
		})
	}
}
func TestProposalResponsePrivacyAndFailureNeverApply(t *testing.T) {
	a, _ := fixture(t)
	seedUsage(t, a, time.Now().UTC())
	c := aiConfig("https://example.test/v1", "chat_completions")
	c.Key = `secret\"quoted`
	must(t, a.saveAI(c))
	tr := proposalTransport("Không hiện "+c.Key, []AIAction{{Kind: "app_profile", AppID: "app-01", Profile: "daily", Reason: "Không hiện " + c.Key}})
	r := a.advisorWithTransport(context.Background(), "đề xuất", "daily", nil, tr)
	if strings.Contains(pretty(r), c.Key) || strings.Contains(r["answer"].(string), c.Key) {
		t.Fatal("credential echo")
	}
	if r["proposal"] == nil {
		t.Fatal(r)
	}
	core, _ := a.config()
	if len(core.Apps) != 0 {
		t.Fatal("manual response auto-applied")
	}
	p := r["proposal"].(AIProposal)
	p.Created = time.Now().Add(-25 * time.Hour)
	must(t, atomicJSON(a.state("ai-proposal.json"), p))
	if _, err := a.applyProposal(context.Background(), p.ID, false); err == nil || err.Error() != "proposal-expired" {
		t.Fatal(err)
	}
}
func TestKernelPresetProposalRestoresOwnedRuntimeValues(t *testing.T) {
	a, _ := fixture(t)
	ctx := context.Background()
	must(t, atomicWrite(a.system("/proc/sys/vm/page-cluster"), []byte("0"), 0600))
	must(t, atomicJSON(a.state("journal.json"), Journal{Boot: a.bootID(), Entries: []Entry{{Kind: "kernel", Package: "vm.page-cluster", Before: "3", Applied: "0"}}}))
	c, _ := a.config()
	off := false
	p, err := makeProposal([]AIAction{{Kind: "kernel_preset", Enabled: &off, Reason: "So sánh trước/sau"}}, nil, c, 0, a.bootID(), time.Now())
	must(t, err)
	must(t, atomicJSON(a.state("ai-proposal.json"), p))
	_, err = a.applyProposal(ctx, p.ID, false)
	must(t, err)
	c, _ = a.config()
	if c.KernelTuning {
		t.Fatal("preset not disabled")
	}
	knobValue(t, a, "vm.page-cluster", "3")
	_, err = a.undoAI(ctx)
	must(t, err)
	c, _ = a.config()
	if !c.KernelTuning {
		t.Fatal("preset not restored")
	}
}

func TestProposalPressureCrashRecoveryAndFailedApplyKeepUndo(t *testing.T) {
	a, f := fixture(t)
	ctx := context.Background()
	l := defaultLearningConfig()
	l.DailyAI = true
	l.AutoApply = true
	must(t, a.saveLearning(l))
	seedUsage(t, a, time.Now().UTC())
	p := seedProposal(t, a, "native-game")
	must(t, atomicWrite(a.system("/proc/pressure/memory"), []byte("full avg10=2.5 avg60=0.0"), 0600))
	if _, err := a.applyProposal(ctx, p.ID, true); err == nil || err.Error() != "proposal-pressure" {
		t.Fatal(err)
	}
	must(t, atomicWrite(a.system("/proc/pressure/memory"), []byte("full avg10=0.0 avg60=0.0"), 0600))
	first, err := a.applyProposal(ctx, p.ID, true)
	must(t, err)
	must(t, a.Step(ctx, "com.example.game", false))
	second := seedProposal(t, a, "daily")
	f.failRead = "cmd game list-modes"
	if _, err = a.applyProposal(ctx, second.ID, false); err == nil {
		t.Fatal("expected restore failure")
	}
	if a.ApplicationStatus().ProposalID != first.ProposalID {
		t.Fatal("failed attempt lost the successful undo receipt")
	}
	f.failRead = ""
	// Simulate a crash after the settings rename and before the final receipt.
	first.Status = "pending"
	must(t, atomicJSON(a.state("ai-application.json"), first))
	if a.ApplicationStatus().Status != "applied" {
		t.Fatal("did not reconcile committed settings")
	}
	_, err = a.undoAI(ctx)
	must(t, err)
	var undone AIApplication
	must(t, readJSON(a.state("ai-application.json"), &undone))
	undone.Status = "undo-pending"
	must(t, atomicJSON(a.state("ai-application.json"), undone))
	if a.ApplicationStatus().Status != "undone" {
		t.Fatal("did not reconcile committed undo")
	}
}

func TestAutoApplyOptOutDuringRequestAndStaleSnapshotAreIgnored(t *testing.T) {
	a, _ := fixture(t)
	ctx := context.Background()
	now := time.Now().UTC()
	seedUsage(t, a, now)
	l := defaultLearningConfig()
	l.Hour = 0
	l.DailyAI = true
	l.AutoApply = true
	must(t, a.saveLearning(l))
	must(t, a.saveAI(aiConfig("https://example.test/v1", "chat_completions")))
	tr := transportFunc(func(req *http.Request) (*http.Response, error) {
		l.AutoApply = false
		must(t, a.saveLearning(l))
		return proposalTransport("Không tự áp dụng", []AIAction{{Kind: "app_profile", AppID: "app-01", Profile: "daily", Reason: "x"}})(req)
	})
	if _, err := a.dailyAnalyze(ctx, now, true, tr); err == nil {
		t.Fatal("ignored settings change during request")
	}
	c, _ := a.config()
	if len(c.Apps) != 0 || exists(a.state("ai-application.json")) {
		t.Fatal("auto opt-out mutated settings")
	}
	base := c
	learning, _ := a.learningConfig()
	must(t, a.resetLearning())
	body, _ := json.Marshal(map[string]any{"answer": "Lời khuyên", "actions": []AIAction{{Kind: "app_profile", AppID: "app-01", Profile: "daily", Reason: "x"}}})
	r := a.attachProposal(map[string]any{"source": "api", "answer": string(body)}, []HabitApp{{ID: "app-01", Package: "com.example.game"}}, base, learning.Generation, time.Now())
	if r["proposal"] != nil || exists(a.state("ai-proposal.json")) {
		t.Fatal("reset history was resurrected")
	}
}
