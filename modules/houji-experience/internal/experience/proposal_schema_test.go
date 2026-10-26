package experience

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"
)

func TestAIRequestsStructuredOutputForBothProtocols(t *testing.T) {
	for _, style := range []string{"chat_completions", "responses"} {
		t.Run(style, func(t *testing.T) {
			a, _ := fixture(t)
			must(t, a.saveAI(aiConfig("https://openrouter.ai/api/v1", style)))
			calls := 0
			tr := transportFunc(func(req *http.Request) (*http.Response, error) {
				calls++
				var body map[string]any
				must(t, json.NewDecoder(req.Body).Decode(&body))
				var format map[string]any
				if style == "responses" {
					format = body["text"].(map[string]any)["format"].(map[string]any)
					if format["type"] != "json_schema" || body["response_format"] != nil {
						t.Fatal("wrong Responses format")
					}
				} else {
					responseFormat := body["response_format"].(map[string]any)
					if responseFormat["type"] != "json_schema" || body["text"] != nil {
						t.Fatal("wrong Chat format")
					}
					format = responseFormat["json_schema"].(map[string]any)
				}
				if format["strict"] != true || format["name"] != "houji_proposal" || body["provider"].(map[string]any)["require_parameters"] != true {
					t.Fatal("schema or provider enforcement missing")
				}
				schema := format["schema"].(map[string]any)
				if schema["additionalProperties"] != false || len(schema["required"].([]any)) != 2 {
					t.Fatal("open envelope schema")
				}
				content := `{"answer":"Không cần đổi cài đặt trong lượt này.","actions":[]}`
				return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(completedResponse(t, style, content))), Header: http.Header{}}, nil
			})
			r := a.advisorWithTransport(context.Background(), "Phân tích", "daily", nil, tr)
			if calls != 1 || r["source"] != "api" || r["proposal_error"] != nil || r["proposal_note"] != "no-changes" {
				t.Fatal(r["ai_error"], r["proposal_error"], calls)
			}
		})
	}
}

func TestSchemaUsesExactAppsAndLiveCapabilities(t *testing.T) {
	c := defaultConfig()
	c.Apps["com.manual.game"] = "daily"
	legend := []HabitApp{{ID: "app-01", Package: "com.manual.game", GameSupported: true}, {ID: "app-02", Package: "com.other.game"}}
	knobs := []KernelKnob{{Control: "tcp_congestion", State: "unsupported"}}
	schema := proposalSchema(legend, c, knobs, false, true)
	variants := schema["properties"].(map[string]any)["actions"].(map[string]any)["items"].(map[string]any)["anyOf"].([]any)
	if len(variants) != 3 || strings.Contains(pretty(schema), "app-01") || strings.Contains(pretty(schema), "com.") || strings.Contains(pretty(schema), "native-game") {
		t.Fatal("manual/unsupported/private app leaked into schema")
	}
	for _, variant := range variants {
		v := variant.(map[string]any)
		if v["additionalProperties"] != false {
			t.Fatal("open action schema")
		}
		p := v["properties"].(map[string]any)
		if enabled, ok := p["enabled"]; ok {
			values := enabled.(map[string]any)["enum"].([]bool)
			if len(values) != 1 || values[0] {
				t.Fatal("unsupported kernel increase permitted")
			}
		}
	}
}

func TestSchemaRejectionDoesNotRetryOrDowngrade(t *testing.T) {
	a, f := fixture(t)
	must(t, a.saveAI(aiConfig("https://openrouter.ai/api/v1", "chat_completions")))
	calls := 0
	tr := transportFunc(func(*http.Request) (*http.Response, error) {
		calls++
		return &http.Response{StatusCode: 400, Body: io.NopCloser(strings.NewReader("test-secret provider details")), Header: http.Header{}}, nil
	})
	r := a.advisorWithTransport(context.Background(), "Phân tích", "daily", nil, tr)
	if r["structured_output_hint"] != true || r["ai_error"] != "http-400" || calls != 1 || f.writes != 0 || strings.Contains(pretty(r), "test-secret") {
		t.Fatal(r["ai_error"], calls)
	}
}

func TestGranularKernelApplyRestoresOnlySelectedControlAndUndo(t *testing.T) {
	for _, test := range []struct{ control, knob, original, target string }{
		{"tcp_congestion", "tcp.congestion", "cubic", "bbr"},
		{"swap_readahead", "vm.page-cluster", "3", "0"},
		{"data_fsync", "f2fs:dm-42", "8", "20"},
	} {
		t.Run(test.control, func(t *testing.T) {
			a, _ := fixture(t)
			seedKnobs(t, a)
			ctx := context.Background()
			must(t, a.Step(ctx, "", true))
			c, err := a.config()
			must(t, err)
			off := false
			p, err := makeProposal([]AIAction{{Kind: "kernel_control", Control: test.control, Enabled: &off, Reason: "Đo riêng thay đổi này"}}, nil, c, 0, a.bootID(), time.Now())
			must(t, err)
			must(t, atomicJSON(a.state("ai-proposal.json"), p))
			_, err = a.applyProposal(ctx, p.ID, false)
			must(t, err)
			knobValue(t, a, test.knob, test.original)
			must(t, a.Step(ctx, "", true))
			knobValue(t, a, test.knob, test.original)
			for _, knob := range a.kernelPlan() {
				if knob.ID != test.knob {
					knobValue(t, a, knob.ID, knob.Target)
				}
			}
			c, err = a.config()
			must(t, err)
			if !c.KernelTuning || kernelControlEnabled(c, test.control) {
				t.Fatal("master or selected control changed incorrectly")
			}
			_, err = a.undoAI(ctx)
			must(t, err)
			must(t, a.Step(ctx, "", true))
			knobValue(t, a, test.knob, test.target)
		})
	}
}

func TestGranularKernelRejectsUnknownUnsupportedAndUndoConflicts(t *testing.T) {
	a, _ := fixture(t)
	on := true
	off := false
	c, _ := a.config()
	if _, err := makeProposal([]AIAction{{Kind: "kernel_control", Control: "/proc/sys/walt/sched_ravg_window_nr_ticks", Enabled: &off, Reason: "x"}}, nil, c, 0, a.bootID(), time.Now()); err == nil {
		t.Fatal("arbitrary control allowed")
	}
	c.KernelControls = map[string]bool{"tcp_congestion": false}
	must(t, atomicJSON(a.state("config.json"), c))
	p, err := makeProposal([]AIAction{{Kind: "kernel_control", Control: "tcp_congestion", Enabled: &on, Reason: "x"}}, nil, c, 0, a.bootID(), time.Now())
	must(t, err)
	must(t, atomicJSON(a.state("ai-proposal.json"), p))
	if _, err = a.applyProposal(context.Background(), p.ID, false); err == nil || err.Error() != "proposal-kernel-unsupported" {
		t.Fatal("unsupported control enabled", err)
	}
	seedKnobs(t, a)
	_, err = a.applyProposal(context.Background(), p.ID, false)
	must(t, err)
	c, err = a.config()
	must(t, err)
	c.KernelControls = map[string]bool{"tcp_congestion": false}
	must(t, atomicJSON(a.state("config.json"), c))
	if _, err = a.undoAI(context.Background()); err == nil || err.Error() != "proposal-undo-conflict" {
		t.Fatal("external control overwritten", err)
	}
}

func TestGranularKernelScheduledIncreaseHonorsPressureAndMaster(t *testing.T) {
	a, _ := fixture(t)
	seedKnobs(t, a)
	l := defaultLearningConfig()
	l.DailyAI = true
	l.AutoApply = true
	must(t, a.saveLearning(l))
	l, err := a.learningConfig()
	must(t, err)
	seedUsage(t, a, time.Now().UTC())
	c, _ := a.config()
	c.KernelTuning = false
	c.KernelControls = map[string]bool{"tcp_congestion": false}
	must(t, atomicJSON(a.state("config.json"), c))
	on := true
	actions := []AIAction{{Kind: "kernel_control", Control: "tcp_congestion", Enabled: &on, Reason: "Đối chiếu mạng"}}
	p, err := makeProposal(actions, nil, c, l.Generation, a.bootID(), time.Now())
	must(t, err)
	must(t, atomicJSON(a.state("ai-proposal.json"), p))
	if _, err = a.applyProposal(context.Background(), p.ID, false); err == nil || err.Error() != "proposal-kernel-preset-off" {
		t.Fatal("control increased with master off", err)
	}
	actions = append(actions, AIAction{Kind: "kernel_preset", Enabled: &on, Reason: "Bật công tắc chính"})
	p, err = makeProposal(actions, nil, c, l.Generation, a.bootID(), time.Now())
	must(t, err)
	must(t, atomicJSON(a.state("ai-proposal.json"), p))
	must(t, atomicWrite(a.system("/proc/pressure/memory"), []byte("full avg10=2.5 avg60=0.0"), 0600))
	if _, err = a.applyProposal(context.Background(), p.ID, true); err == nil || err.Error() != "proposal-pressure" {
		t.Fatal("scheduled increase ignored pressure", err)
	}
	must(t, atomicWrite(a.system("/proc/pressure/memory"), []byte("full avg10=0.0 avg60=0.0"), 0600))
	_, err = a.applyProposal(context.Background(), p.ID, true)
	must(t, err)
}
