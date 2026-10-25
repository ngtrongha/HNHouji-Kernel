package experience

import (
	"context"
	"errors"
	"os"
	"time"
)

type Request struct {
	Action     string          `json:"action"`
	Package    string          `json:"package,omitempty"`
	Profile    string          `json:"profile,omitempty"`
	Guide      string          `json:"guide,omitempty"`
	Protected  bool            `json:"protected,omitempty"`
	Enabled    bool            `json:"enabled,omitempty"`
	AI         *AIConfig       `json:"ai,omitempty"`
	Prompt     string          `json:"prompt,omitempty"`
	Learning   *LearningConfig `json:"learning,omitempty"`
	Mode       string          `json:"mode,omitempty"`
	JobID      string          `json:"job_id,omitempty"`
	Goal       string          `json:"goal,omitempty"`
	Messages   []ChatMessage   `json:"messages,omitempty"`
	ProposalID string          `json:"proposal_id,omitempty"`
}
type Response struct {
	OK    bool   `json:"ok"`
	Data  any    `json:"data,omitempty"`
	Error string `json:"error,omitempty"`
}

func (a *App) Dispatch(ctx context.Context, r Request) Response {
	v, e := a.dispatch(ctx, r)
	if e != nil {
		return Response{OK: false, Error: e.Error()}
	}
	return Response{OK: true, Data: v}
}

func (a *App) dispatch(ctx context.Context, r Request) (any, error) {
	switch r.Action {
	case "ai_proposal_apply":
		return a.applyProposal(ctx, r.ProposalID, false)
	case "ai_application_status":
		return a.ApplicationStatus(), nil
	case "ai_application_undo":
		return a.undoAI(ctx)
	case "ai_application_acknowledge":
		return a.acknowledgeAIApplication()
	case "ai_job_start":
		return a.startAIJob(r, a.launchAIWorker)
	case "ai_job_status":
		return a.AIJobStatus(r.JobID, false)
	case "ai_job_cancel":
		return a.AIJobStatus(r.JobID, true)
	case "ai_job_dismiss":
		u, e := a.lock("ai-job")
		if e != nil {
			return nil, e
		}
		defer u()
		var job AIJob
		if readAIJSON(a.state("ai-job.json"), &job) == nil && activeJob(job) {
			return nil, errors.New("ai-job-busy")
		}
		e = os.Remove(a.state("ai-job.json"))
		if os.IsNotExist(e) {
			e = nil
		}
		return nil, e
	case "learning_status":
		return a.LearningStatus(ctx), nil
	case "learning_preview":
		return a.dailyPayload(time.Now())
	case "learning_analyze":
		return a.dailyAnalyze(ctx, time.Now(), false, nil)
	case "learning_save":
		if r.Learning == nil {
			return nil, errors.New("missing learning configuration")
		}
		return nil, a.saveLearning(*r.Learning)
	case "learning_reset":
		return nil, a.resetLearning()
	case "status":
		return a.Status(ctx), nil
	case "doctor":
		return a.Doctor(ctx), nil
	case "ai_preview":
		return a.advisorContext(ctx, time.Now()), nil
	case "report":
		return a.Report(ctx), nil
	case "packages":
		return a.Packages(ctx)
	case "guide_open":
		guides := map[string]string{"winlator": "https://github.com/brunodev85/winlator/releases/tag/v11.2.0", "kde": "https://kdeconnect.kde.org/", "localsend": "https://localsend.org/", "tailscale": "https://tailscale.com/docs/install/android", "droidspaces": "https://github.com/ravindu644/Droidspaces-OSS"}
		link, ok := guides[r.Guide]
		if !ok {
			return nil, errors.New("unknown guide")
		}
		_, e := a.run(ctx, "am", "start", "-a", "android.intent.action.VIEW", "-d", link)
		return map[string]bool{"opened": e == nil}, e
	case "ai_status":
		return a.AIStatus(), nil
	case "ai_test":
		return a.Ask(ctx, "", true), nil
	case "ai_ask":
		return a.Ask(ctx, r.Prompt, false), nil
	case "export":
		path, e := a.Export(ctx)
		return map[string]string{"path": path}, e
	}
	unlock, e := a.lock("state")
	if e != nil {
		return nil, e
	}
	defer unlock()
	c, e := a.config()
	if e != nil {
		if r.Action != "disable" && r.Action != "restore" {
			return nil, e
		}
		data, _ := os.ReadFile(a.state("config.json"))
		if e = atomicWrite(a.state("config.invalid.backup"), data, 0600); e != nil {
			return nil, e
		}
		c = disabledConfig()
	}
	switch r.Action {
	case "kernel_tuning_set":
		c.KernelTuning = r.Enabled
		c.Generation++
		if e = atomicJSON(a.state("config.json"), c); e != nil {
			return nil, e
		}
		if !r.Enabled {
			j, err := a.journal()
			if err != nil {
				return nil, err
			}
			e = a.restoreEntries(ctx, &j, func(entry Entry) bool { return entry.Kind == "kernel" })
		}
		return a.snapshotConfig(), e
	case "enable":
		if e = a.compatible(ctx); e != nil {
			return nil, e
		}
		if a.moduleDisabled() {
			return nil, errors.New("module disabled in Manager")
		}
		if exists(a.Paths.Legacy) {
			return nil, errors.New("legacy-conflict: migrate helper, then reboot")
		}
		if c.MigrationBoot != "" && c.MigrationBoot == a.bootID() {
			return nil, errors.New("reboot-required")
		}
		j, e := a.journal()
		if e != nil {
			return nil, e
		}
		if len(j.Entries) > 0 {
			if e = a.restoreEntries(ctx, &j, func(Entry) bool { return true }); e != nil {
				return nil, e
			}
		}
		c.Enabled = true
	case "disable", "restore":
		c.Enabled = false
		c.DefaultsVersion = 1
		c.Generation++
		if e = atomicJSON(a.state("config.json"), c); e != nil {
			return nil, e
		}
		e = a.restoreAll(ctx)
		a.runtime("observe", "", "disabled")
		return a.snapshotConfig(), e
	case "migrate_legacy":
		if e = a.compatible(ctx); e != nil {
			return nil, e
		}
		e = a.migrateLegacy(ctx, &c)
		return a.snapshotConfig(), e
	case "resolve_conflicts":
		c.Enabled = false
		c.DefaultsVersion = 1
		c.Generation++
		if e = atomicJSON(a.state("config.json"), c); e != nil {
			return nil, e
		}
		j, e := a.journal()
		if e != nil {
			return nil, e
		}
		remaining := []Entry{}
		for _, entry := range j.Entries {
			v, err := a.current(ctx, entry)
			if entry.Conflict && err == nil && v != entry.Applied {
				continue
			}
			remaining = append(remaining, entry)
		}
		j.Entries = remaining
		if e = atomicJSON(a.state("journal.json"), j); e != nil {
			return nil, e
		}
		return a.snapshotConfig(), nil
	case "app_set", "app_remove", "protect_set":
		if !validPackage(r.Package) {
			return nil, errors.New("invalid package")
		}
		if r.Action != "protect_set" {
			delete(c.AIProfiles, r.Package)
		}
		if r.Action == "app_remove" {
			delete(c.Apps, r.Package)
		} else {
			if !a.installed(ctx, r.Package) {
				return nil, errors.New("package not installed for owner user")
			}
			if r.Action == "app_set" {
				if !profiles[r.Profile] {
					return nil, errors.New("unknown profile")
				}
				if len(c.Apps) >= 100 && c.Apps[r.Package] == "" {
					return nil, errors.New("maximum 100 app rules")
				}
				c.Apps[r.Package] = r.Profile
			} else {
				if r.Protected {
					if len(c.Protected) >= 100 && !c.Protected[r.Package] {
						return nil, errors.New("maximum 100 protected apps")
					}
					c.Protected[r.Package] = true
				} else {
					delete(c.Protected, r.Package)
				}
			}
		}
	case "ai_save":
		if r.AI == nil {
			return nil, errors.New("missing AI configuration")
		}
		if e = a.saveAI(*r.AI); e != nil {
			return nil, e
		}
		return a.AIStatus(), nil
	case "ai_disable":
		_, _ = a.AIJobStatus("", true)
		var ai AIConfig
		if e = readJSON(a.state("ai.json"), &ai); e != nil {
			if os.IsNotExist(e) {
				return a.AIStatus(), nil
			}
			return nil, e
		}
		ai.Enabled = false
		if e = atomicJSON(a.state("ai.json"), ai); e != nil {
			return nil, e
		}
		return a.AIStatus(), nil
	case "ai_clear":
		_, _ = a.AIJobStatus("", true)
		if e = os.Remove(a.state("ai.json")); e != nil && !os.IsNotExist(e) {
			return nil, e
		}
		return a.AIStatus(), nil
	default:
		return nil, errors.New("unsupported action")
	}
	c.Generation++
	if e = atomicJSON(a.state("config.json"), c); e != nil {
		return nil, e
	}
	return a.snapshotConfig(), nil
}
