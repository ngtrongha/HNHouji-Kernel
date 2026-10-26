package experience

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"math"
	"strings"
	"time"
)

type AIAction struct {
	Kind    string `json:"kind"`
	Control string `json:"control,omitempty"`
	Level   string `json:"level,omitempty"`
	AppID   string `json:"app_id,omitempty"`
	Profile string `json:"profile,omitempty"`
	Enabled *bool  `json:"enabled,omitempty"`
	Reason  string `json:"reason"`
}
type AIChange struct {
	Kind        string `json:"kind"`
	Control     string `json:"control,omitempty"`
	AppID       string `json:"app_id,omitempty"`
	Package     string `json:"package,omitempty"`
	Before      string `json:"before"`
	After       string `json:"after"`
	Reason      string `json:"reason"`
	BeforeOwner string `json:"before_owner,omitempty"`
	AfterOwner  string `json:"after_owner,omitempty"`
}
type AIProposal struct {
	ID                 string     `json:"id"`
	Created            time.Time  `json:"created"`
	Boot               string     `json:"boot"`
	BaseHash           string     `json:"base_hash"`
	LearningGeneration int64      `json:"learning_generation"`
	Changes            []AIChange `json:"changes"`
}
type AIApplication struct {
	ProposalID   string         `json:"proposal_id"`
	At           time.Time      `json:"at"`
	Automatic    bool           `json:"automatic"`
	Status       string         `json:"status"`
	BeforeHash   string         `json:"before_hash"`
	AfterHash    string         `json:"after_hash"`
	Changes      []AIChange     `json:"changes"`
	Error        string         `json:"error,omitempty"`
	UndoFromHash string         `json:"undo_from_hash,omitempty"`
	UndoHash     string         `json:"undo_hash,omitempty"`
	Previous     *AIApplication `json:"previous,omitempty"`
}

func configHash(c Config) string { b, _ := json.Marshal(c); return digest(b) }
func appProfile(c Config, pkg string) string {
	if c.Apps[pkg] == "" {
		return "automatic"
	}
	return c.Apps[pkg]
}
func enabledText(on bool) string {
	if on {
		return "on"
	}
	return "off"
}

const proposalInstruction = `
Trả DUY NHẤT một JSON object, không bọc code fence, theo mẫu:
{"answer":"Phân tích tiếng Việt bằng Markdown, đủ bằng chứng/đánh đổi/cách kiểm chứng","actions":[{"kind":"app_profile","app_id":"app-01","profile":"daily","reason":"Lý do có bằng chứng"},{"kind":"kernel_preset","enabled":false,"reason":"Lý do"}]}
Ưu tiên kernel_level: {"kind":"kernel_level","control":"swap_readahead","level":"1","reason":"Lý do có bằng chứng"}. Chỉ chọn level từ levels của control trong snapshot; stock hoàn tác override. Không gộp kernel_level và kernel_control cho cùng control. Android system_settings_read_only chỉ được đọc. AI theo lịch chỉ đổi control đang bật khi master bật, không tự bật lại mục tắt. actions có thể rỗng khi chưa có thay đổi đủ căn cứ. Thêm kind kernel_control: {"kind":"kernel_control","control":"tcp_congestion","enabled":false,"reason":"Lý do ngắn có bằng chứng"}. control chỉ thuộc swap_readahead, tcp_congestion, data_fsync và chỉ bật nếu kernel_controls có state=available, kernel_compatibility.ready=true. Mỗi control bật dùng mức đã lưu của module; tắt khôi phục giá trị gốc do module sở hữu. Không được tự tạo giá trị ngoài levels cho phép. Bật control cần kernel_preset bật (có thể đề xuất cùng lượt). Mỗi ứng dụng/control/preset chỉ xuất hiện một lần. reason tối đa 300 ký tự. profile thuộc automatic (bỏ quy tắc riêng), daily, native-game, windows-game, linux. App-ID phải có trong snapshot; native-game cần performance_supported=true. Với automatic_run=true, không đổi app có rule_source=manual. Không tạo lệnh, đường dẫn, giá trị sysctl, bảo vệ thông báo, khóa API hoặc tùy chọn tự cấp quyền. Không nói đã áp dụng: module sẽ kiểm tra, người dùng bấm áp dụng hoặc đã bật tự áp dụng theo lịch. Chỉ đề xuất thay đổi có bằng chứng và phép kiểm chứng rõ ràng; không suy đoán số tiết kiệm/FPS. Nếu không có thay đổi phù hợp, trả actions=[] và giải thích; không bịa thay đổi để có nút áp dụng.`

func decodeProposal(answer string) (string, []AIAction, error) {
	text := strings.TrimSpace(answer)
	if strings.HasPrefix(text, "```json\n") && strings.HasSuffix(text, "```") {
		text = strings.TrimSuffix(strings.TrimPrefix(text, "```json\n"), "```")
	}
	var envelope struct {
		Answer  string     `json:"answer"`
		Actions []AIAction `json:"actions"`
	}
	d := json.NewDecoder(strings.NewReader(text))
	d.DisallowUnknownFields()
	if err := d.Decode(&envelope); err != nil {
		return "", nil, errors.New("proposal-format-invalid")
	}
	var extra any
	if d.Decode(&extra) != io.EOF || strings.TrimSpace(envelope.Answer) == "" || envelope.Actions == nil || len(envelope.Actions) > 18 {
		return "", nil, errors.New("proposal-format-invalid")
	}
	return envelope.Answer, envelope.Actions, nil
}
func makeProposal(actions []AIAction, legend []HabitApp, base Config, learningGeneration int64, boot string, at time.Time) (AIProposal, error) {
	p := AIProposal{Created: at.UTC(), Boot: boot, BaseHash: configHash(base), LearningGeneration: learningGeneration, Changes: []AIChange{}}
	apps := map[string]HabitApp{}
	for _, app := range legend {
		apps[app.ID] = app
	}
	seen := map[string]bool{}
	for _, action := range actions {
		if strings.TrimSpace(action.Reason) == "" || len(action.Reason) > 1200 {
			return p, errors.New("proposal-reason-invalid")
		}
		switch action.Kind {
		case "app_profile":
			if action.Level != "" {
				return p, errors.New("proposal-action-not-allowed")
			}
			app, ok := apps[action.AppID]
			if !ok || !validPackage(app.Package) || action.Enabled != nil || action.Control != "" || (!profiles[action.Profile] && action.Profile != "automatic") {
				return p, errors.New("proposal-app-invalid")
			}
			if action.Profile == "native-game" && !app.GameSupported {
				return p, errors.New("proposal-game-unsupported")
			}
			if seen[app.Package] {
				return p, errors.New("proposal-duplicate")
			}
			seen[app.Package] = true
			before := appProfile(base, app.Package)
			if before == action.Profile {
				continue
			}
			owner := ""
			if action.Profile != "automatic" {
				owner = action.Profile
			}
			p.Changes = append(p.Changes, AIChange{Kind: action.Kind, AppID: action.AppID, Package: app.Package, Before: before, After: action.Profile, Reason: action.Reason, BeforeOwner: base.AIProfiles[app.Package], AfterOwner: owner})
		case "kernel_preset":
			if action.Level != "" {
				return p, errors.New("proposal-action-not-allowed")
			}
			if action.AppID != "" || action.Profile != "" || action.Control != "" || action.Enabled == nil || seen["kernel"] {
				return p, errors.New("proposal-kernel-invalid")
			}
			seen["kernel"] = true
			if base.KernelTuning != *action.Enabled {
				p.Changes = append(p.Changes, AIChange{Kind: action.Kind, Before: enabledText(base.KernelTuning), After: enabledText(*action.Enabled), Reason: action.Reason})
			}
		case "kernel_control":
			if action.Level != "" {
				return p, errors.New("proposal-action-not-allowed")
			}
			if !validKernelControl(action.Control) || action.AppID != "" || action.Profile != "" || action.Enabled == nil || seen["control:"+action.Control] {
				return p, errors.New("proposal-kernel-invalid")
			}
			seen["control:"+action.Control] = true
			before := kernelControlEnabled(base, action.Control)
			if before != *action.Enabled {
				p.Changes = append(p.Changes, AIChange{Kind: action.Kind, Control: action.Control, Before: enabledText(before), After: enabledText(*action.Enabled), Reason: action.Reason})
			}
		case "kernel_level":
			if !validKernelLevel(action.Control, action.Level) || action.Enabled != nil || action.AppID != "" || action.Profile != "" || seen["control:"+action.Control] {
				return p, errors.New("proposal-kernel-invalid")
			}
			seen["control:"+action.Control] = true
			before := kernelLevel(base, action.Control)
			if before != action.Level {
				p.Changes = append(p.Changes, AIChange{Kind: action.Kind, Control: action.Control, Before: before, After: action.Level, Reason: action.Reason})
			}
		default:
			return p, errors.New("proposal-action-not-allowed")
		}
	}
	var id [16]byte
	if _, err := rand.Read(id[:]); err != nil {
		return p, err
	}
	p.ID = hex.EncodeToString(id[:])
	return p, nil
}

// Parsing a structured response never applies it. Only a stored, checked plan
// ID may reach the settings transaction below, not caller-supplied actions.
func (a *App) attachProposal(result map[string]any, legend []HabitApp, base Config, learningGeneration int64, at time.Time) map[string]any {
	if result["source"] != "api" {
		return result
	}
	answer, actions, err := decodeProposal(result["answer"].(string))
	if err != nil {
		var readable struct {
			Answer string `json:"answer"`
		}
		if json.Unmarshal([]byte(result["answer"].(string)), &readable) == nil && readable.Answer != "" {
			var ai AIConfig
			_ = readJSON(a.state("ai.json"), &ai)
			result["answer"] = redactAI(readable.Answer, ai.Key)
		}
		result["proposal_error"] = "proposal-format-invalid"
		return result
	}
	result["answer"] = answer
	var ai AIConfig
	if readJSON(a.state("ai.json"), &ai) == nil {
		result["answer"] = redactAI(answer, ai.Key)
		for i := range actions {
			actions[i].Reason = redactAI(actions[i].Reason, ai.Key)
		}
	}
	p, err := makeProposal(actions, legend, base, learningGeneration, a.bootID(), at)
	if err != nil {
		result["proposal_error"] = err.Error()
		return result
	}
	if len(p.Changes) == 0 {
		result["proposal_note"] = "no-changes"
		return result
	}
	stateUnlock, err := a.lock("state")
	if err != nil {
		result["proposal_error"] = "proposal-busy"
		return result
	}
	defer stateUnlock()
	learningUnlock, err := a.lock("learning")
	if err != nil {
		result["proposal_error"] = "proposal-busy"
		return result
	}
	defer learningUnlock()
	current, err := a.config()
	learning, learningErr := a.learningConfig()
	if err != nil || learningErr != nil || configHash(current) != p.BaseHash || learning.Generation != learningGeneration {
		result["proposal_error"] = "proposal-settings-changed"
		return result
	}
	if !current.Enabled || a.moduleDisabled() {
		result["proposal_error"] = "proposal-module-disabled"
		return result
	}
	u, err := a.lock("proposal")
	if err != nil {
		result["proposal_error"] = "proposal-busy"
		return result
	}
	defer u()
	if err = atomicJSON(a.state("ai-proposal.json"), p); err != nil {
		result["proposal_error"] = "proposal-save-failed"
		return result
	}
	result["proposal"] = p
	return result
}

func (a *App) aiApplyReady(ctx context.Context, c Config) error {
	if !c.Enabled || a.moduleDisabled() {
		return errors.New("proposal-module-disabled")
	}
	if err := a.compatible(ctx); err != nil {
		return errors.New("proposal-device-unsupported")
	}
	if !a.ownerUser(ctx) {
		return errors.New("owner-user-only")
	}
	if exists(a.Paths.Legacy) || (c.MigrationBoot != "" && c.MigrationBoot == a.bootID()) {
		return errors.New("proposal-reboot-or-legacy")
	}
	if !a.KernelSync(ctx).Ready {
		return errors.New("proposal-kernel-mismatch")
	}
	j, err := a.journal()
	if err != nil {
		return errors.New("proposal-journal-invalid")
	}
	for _, entry := range j.Entries {
		if entry.Conflict {
			return errors.New("proposal-restore-conflict")
		}
	}
	var runtime Runtime
	_ = readJSON(a.state("runtime.json"), &runtime)
	if runtime.Reason == "kernel-setting-failed" || runtime.Reason == "restore-conflict" || runtime.Reason == "setting-failed" {
		return errors.New("proposal-runtime-needs-review")
	}
	return nil
}
func (a *App) ApplicationStatus() AIApplication {
	var receipt AIApplication
	_ = readJSON(a.state("ai-application.json"), &receipt)
	if receipt.Status == "pending" {
		if c, e := a.config(); e == nil {
			switch configHash(c) {
			case receipt.AfterHash:
				receipt.Status = "applied"
			case receipt.BeforeHash:
				receipt.Status = "not-committed"
			default:
				receipt.Status = "needs-review"
			}
		}
	}
	if receipt.Status == "not-committed" && receipt.Previous != nil {
		return *receipt.Previous
	}
	if receipt.Status == "applied" {
		receipt.Previous = nil
	}
	if receipt.Status == "undo-pending" {
		if c, e := a.config(); e == nil {
			switch configHash(c) {
			case receipt.UndoHash:
				receipt.Status = "undone"
			case receipt.UndoFromHash:
				receipt.Status = "applied"
			default:
				receipt.Status = "needs-review"
			}
		}
	}
	return receipt
}
func (a *App) applyProposal(ctx context.Context, id string, automatic bool) (AIApplication, error) {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	u, err := a.lock("state")
	if err != nil {
		return AIApplication{}, err
	}
	defer u()
	v, err := a.lock("learning")
	if err != nil {
		return AIApplication{}, err
	}
	defer v()
	w, err := a.lock("proposal")
	if err != nil {
		return AIApplication{}, err
	}
	defer w()
	var p AIProposal
	if readJSON(a.state("ai-proposal.json"), &p) != nil || p.ID != id {
		return AIApplication{}, errors.New("proposal-not-current")
	}
	previous := a.ApplicationStatus()
	if previous.ProposalID == id && (previous.Status == "applied" || previous.Status == "undone") {
		return previous, nil
	}
	if previous.Status == "pending" || previous.Status == "needs-review" {
		return previous, errors.New("proposal-application-needs-review")
	}
	if p.Boot != a.bootID() || time.Since(p.Created) > 24*time.Hour || time.Since(p.Created) < 0 || len(p.Changes) == 0 || len(p.Changes) > 18 {
		return AIApplication{}, errors.New("proposal-expired")
	}
	c, err := a.config()
	if err != nil {
		return AIApplication{}, err
	}
	if configHash(c) != p.BaseHash {
		return AIApplication{}, errors.New("proposal-settings-changed")
	}
	l, err := a.learningConfig()
	if err != nil {
		return AIApplication{}, err
	}
	if l.Generation != p.LearningGeneration {
		return AIApplication{}, errors.New("proposal-settings-changed")
	}
	if automatic {
		if !l.Enabled || !l.DailyAI || !l.AutoApply {
			return AIApplication{}, errors.New("proposal-auto-disabled")
		}
		h, e := a.usageHistory()
		if e != nil {
			return AIApplication{}, e
		}
		pruneHistory(&h, l, time.Now())
		var days int
		var samples, observed int64
		for _, d := range h.Days {
			if d.Samples > 0 {
				days++
			}
			samples += d.Samples
			observed += d.Observed
		}
		if days < 2 || samples < 30 || observed < 1800 {
			return AIApplication{}, errors.New("proposal-auto-needs-history")
		}
	}
	if err = a.aiApplyReady(ctx, c); err != nil {
		return AIApplication{}, err
	}
	for _, change := range p.Changes {
		if change.Kind != "kernel_control" && change.Kind != "kernel_level" && change.Control != "" {
			return AIApplication{}, errors.New("proposal-kernel-invalid")
		}
		switch change.Kind {
		case "app_profile":
			if !validPackage(change.Package) || (!profiles[change.After] && change.After != "automatic") || appProfile(c, change.Package) != change.Before || change.BeforeOwner != c.AIProfiles[change.Package] || (change.After != "automatic" && change.AfterOwner != change.After) || (change.After == "automatic" && change.AfterOwner != "") {
				return AIApplication{}, errors.New("proposal-app-invalid")
			}
			if !a.installed(ctx, change.Package) {
				return AIApplication{}, errors.New("proposal-app-uninstalled")
			}
			if automatic && c.Apps[change.Package] != "" && c.AIProfiles[change.Package] != c.Apps[change.Package] {
				return AIApplication{}, errors.New("proposal-manual-rule")
			}
			if change.After == "native-game" && !a.gameSupported(ctx, change.Package) {
				return AIApplication{}, errors.New("proposal-game-unsupported")
			}
		case "kernel_preset":
			if automatic && change.After == "on" && !c.KernelTuning {
				return AIApplication{}, errors.New("proposal-kernel-preset-off")
			}
			if (change.After != "on" && change.After != "off") || change.Before != enabledText(c.KernelTuning) {
				return AIApplication{}, errors.New("proposal-kernel-invalid")
			}
			if change.After == "on" {
				supported := false
				for _, knob := range a.kernelPlan() {
					supported = supported || knob.State == "available"
				}
				if !supported {
					return AIApplication{}, errors.New("proposal-kernel-unsupported")
				}
			}
		case "kernel_control":
			if automatic && (!c.KernelTuning || !kernelControlEnabled(c, change.Control)) {
				return AIApplication{}, errors.New("proposal-control-disabled")
			}
			if !validKernelControl(change.Control) || (change.After != "on" && change.After != "off") || change.Before != enabledText(kernelControlEnabled(c, change.Control)) {
				return AIApplication{}, errors.New("proposal-kernel-invalid")
			}
			if change.After == "on" {
				supported := false
				for _, knob := range a.configuredKernelPlan(c) {
					supported = supported || (knob.Control == change.Control && knob.State == "available")
				}
				if !supported {
					return AIApplication{}, errors.New("proposal-kernel-unsupported")
				}
			}
		case "kernel_level":
			if !validKernelLevel(change.Control, change.After) || !validKernelLevel(change.Control, change.Before) || change.Before != kernelLevel(c, change.Control) {
				return AIApplication{}, errors.New("proposal-kernel-invalid")
			}
			if !c.KernelTuning || !kernelControlEnabled(c, change.Control) {
				return AIApplication{}, errors.New("proposal-control-disabled")
			}
			if change.After != "stock" {
				supported := false
				for _, knob := range a.configuredKernelPlan(c) {
					if knob.Control == change.Control && knob.Current != "" && hasLevel(knob, change.After) {
						supported = true
					}
				}
				if !supported {
					return AIApplication{}, errors.New("proposal-kernel-unsupported")
				}
			}
		default:
			return AIApplication{}, errors.New("proposal-action-not-allowed")
		}
	}
	for _, change := range p.Changes {
		if change.Kind == "kernel_level" && change.After != "stock" {
			if err = a.pressureReady(ctx); err != nil {
				return AIApplication{}, err
			}
			break
		}
	}
	if automatic {
		increase := false
		for _, change := range p.Changes {
			increase = increase || change.After == "native-game" || (change.Kind == "kernel_level" && change.After != "stock") || ((change.Kind == "kernel_preset" || change.Kind == "kernel_control") && change.After == "on")
		}
		if increase {
			sampleCtx, cancel := context.WithTimeout(ctx, 1500*time.Millisecond)
			metrics, _ := a.Report(sampleCtx)["metrics"].(map[string]any)
			cancel()
			full, fullOK := metrics["psi_full_avg10"].(float64)
			thermal, thermalOK := metrics["thermal_status"].(float64)
			if !fullOK || !thermalOK || full < 0 || thermal < 0 || math.IsNaN(full) || math.IsNaN(thermal) || math.IsInf(full, 0) || math.IsInf(thermal, 0) {
				return AIApplication{}, errors.New("proposal-pressure-unknown")
			}
			if full >= 2 || thermal >= 3 {
				return AIApplication{}, errors.New("proposal-pressure")
			}
		}
	}
	if ctx.Err() != nil {
		return AIApplication{}, ctx.Err()
	}
	before := configHash(c)
	setAIChanges(&c, p.Changes, false)
	for _, change := range p.Changes {
		if change.Kind == "kernel_control" && change.After == "on" && !c.KernelTuning {
			return AIApplication{}, errors.New("proposal-kernel-preset-off")
		}
	}
	if len(c.Apps) > 100 {
		return AIApplication{}, errors.New("maximum 100 app rules")
	}
	c.Generation++
	receipt := AIApplication{ProposalID: id, At: time.Now().UTC(), Automatic: automatic, Status: "pending", BeforeHash: before, AfterHash: configHash(c), Changes: p.Changes}
	if previous.Status == "applied" {
		copy := previous
		copy.Previous = nil
		receipt.Previous = &copy
	}
	if err = atomicJSON(a.state("ai-application.json"), receipt); err != nil {
		return receipt, err
	}
	if err = a.restoreAIChanges(ctx, p.Changes, false); err != nil {
		receipt.Status = "failed"
		receipt.Error = "restore-conflict"
		_ = a.saveApplyFailure(receipt)
		return receipt, err
	}
	if err = atomicJSON(a.state("config.json"), c); err != nil {
		receipt.Status = "failed"
		receipt.Error = "settings-save-failed"
		_ = a.saveApplyFailure(receipt)
		return receipt, err
	}
	receipt.Status = "applied"
	receipt.Previous = nil
	if err = atomicJSON(a.state("ai-application.json"), receipt); err != nil {
		return receipt, err
	}
	return receipt, nil
}
func (a *App) saveApplyFailure(receipt AIApplication) error {
	if receipt.Previous != nil {
		return atomicJSON(a.state("ai-application.json"), receipt.Previous)
	}
	return atomicJSON(a.state("ai-application.json"), receipt)
}

func (a *App) acknowledgeAIApplication() (AIApplication, error) {
	u, err := a.lock("state")
	if err != nil {
		return AIApplication{}, err
	}
	defer u()
	v, err := a.lock("proposal")
	if err != nil {
		return AIApplication{}, err
	}
	defer v()
	r := a.ApplicationStatus()
	if r.Status != "needs-review" {
		return r, errors.New("proposal-no-review-needed")
	}
	r.Status = "acknowledged"
	return r, atomicJSON(a.state("ai-application.json"), r)
}
func setAIChanges(c *Config, changes []AIChange, undo bool) {
	if c.AIProfiles == nil {
		c.AIProfiles = map[string]string{}
	}
	for _, change := range changes {
		value, owner := change.After, change.AfterOwner
		if undo {
			value, owner = change.Before, change.BeforeOwner
		}
		switch change.Kind {
		case "app_profile":
			if value == "automatic" {
				delete(c.Apps, change.Package)
			} else {
				c.Apps[change.Package] = value
			}
			if owner == "" {
				delete(c.AIProfiles, change.Package)
			} else {
				c.AIProfiles[change.Package] = owner
			}
		case "kernel_preset":
			c.KernelTuning = value == "on"
		case "kernel_control":
			if c.KernelControls == nil {
				c.KernelControls = map[string]bool{}
			}
			if value == "on" {
				delete(c.KernelControls, change.Control)
			} else {
				c.KernelControls[change.Control] = false
			}
		case "kernel_level":
			if c.KernelLevels == nil {
				c.KernelLevels = map[string]string{}
			}
			c.KernelLevels[change.Control] = value
		}
	}
}
func (a *App) restoreAIChanges(ctx context.Context, changes []AIChange, undo bool) error {
	apps := map[string]bool{}
	kernelOff := false
	controlsOff := map[string]bool{}
	for _, change := range changes {
		if change.Kind == "app_profile" {
			apps[change.Package] = true
		}
		value := change.After
		if undo {
			value = change.Before
		}
		if change.Kind == "kernel_preset" && value == "off" {
			kernelOff = true
		}
		if change.Kind == "kernel_control" && value == "off" {
			controlsOff[change.Control] = true
		}
		if change.Kind == "kernel_level" {
			controlsOff[change.Control] = true
		}
	}
	j, err := a.journal()
	if err != nil {
		return err
	}
	return a.restoreEntries(ctx, &j, func(e Entry) bool {
		return (e.Kind == "game" && apps[e.Package]) || (e.Kind == "kernel" && (kernelOff || controlsOff[kernelControlForKnob(e.Package)]))
	})
}
func (a *App) undoAI(ctx context.Context) (AIApplication, error) {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	if !a.ownerUser(ctx) {
		return AIApplication{}, errors.New("owner-user-only")
	}
	u, err := a.lock("state")
	if err != nil {
		return AIApplication{}, err
	}
	defer u()
	v, err := a.lock("proposal")
	if err != nil {
		return AIApplication{}, err
	}
	defer v()
	r := a.ApplicationStatus()
	if r.Status != "applied" {
		return r, errors.New("proposal-no-undo")
	}
	c, err := a.config()
	if err != nil {
		return r, err
	}
	for _, change := range r.Changes {
		if change.Kind != "app_profile" && change.Kind != "kernel_preset" && change.Kind != "kernel_control" && change.Kind != "kernel_level" {
			return r, errors.New("proposal-action-not-allowed")
		}
		if change.Kind == "app_profile" && (!validPackage(change.Package) || (!profiles[change.Before] && change.Before != "automatic")) {
			return r, errors.New("proposal-app-invalid")
		}
		if change.Kind == "kernel_preset" && change.Before != "on" && change.Before != "off" {
			return r, errors.New("proposal-kernel-invalid")
		}
		if change.Kind == "app_profile" && (appProfile(c, change.Package) != change.After || c.AIProfiles[change.Package] != change.AfterOwner) {
			return r, errors.New("proposal-undo-conflict")
		}
		if change.Kind == "kernel_preset" && enabledText(c.KernelTuning) != change.After {
			return r, errors.New("proposal-undo-conflict")
		}
		if change.Kind == "kernel_control" {
			if !validKernelControl(change.Control) || (change.Before != "on" && change.Before != "off") {
				return r, errors.New("proposal-kernel-invalid")
			}
			if enabledText(kernelControlEnabled(c, change.Control)) != change.After {
				return r, errors.New("proposal-undo-conflict")
			}
			if kernelLevel(c, change.Control) != defaultKernelLevel(change.Control) {
				return r, errors.New("proposal-undo-conflict")
			}
		}
		if change.Kind == "kernel_level" && (!validKernelLevel(change.Control, change.Before) || kernelLevel(c, change.Control) != change.After || !kernelControlEnabled(c, change.Control)) {
			return r, errors.New("proposal-undo-conflict")
		}
	}
	if err = a.restoreAIChanges(ctx, r.Changes, true); err != nil {
		return r, err
	}
	r.UndoFromHash = configHash(c)
	setAIChanges(&c, r.Changes, true)
	c.Generation++
	r.UndoHash = configHash(c)
	r.Status = "undo-pending"
	if err = atomicJSON(a.state("ai-application.json"), r); err != nil {
		return r, err
	}
	if err = atomicJSON(a.state("config.json"), c); err != nil {
		return r, err
	}
	r.Status = "undone"
	if err = atomicJSON(a.state("ai-application.json"), r); err != nil {
		return r, err
	}
	return r, nil
}
