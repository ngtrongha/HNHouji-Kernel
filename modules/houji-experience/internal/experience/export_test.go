package experience

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestExportCreatesReadablePublicDownloadWithoutChangingPrivateState(t *testing.T) {
	a, _ := fixture(t)
	must(t, a.saveAI(aiConfig("https://private.example/v1", "responses")))
	before, e := os.ReadFile(a.state("ai.json"))
	must(t, e)
	path, e := a.Export(context.Background())
	must(t, e)
	if filepath.Dir(path) != a.system("/storage/emulated/0/Download/HNHouji") {
		t.Fatal("not in public Downloads", path)
	}
	if !strings.HasPrefix(filepath.Base(path), "HNHouji-report-") || filepath.Ext(path) != ".json" {
		t.Fatal(path)
	}
	data, e := os.ReadFile(path)
	must(t, e)
	if !json.Valid(data) {
		t.Fatal("incomplete report")
	}
	if strings.Contains(string(data), "test-secret") || strings.Contains(string(data), "private.example") {
		t.Fatal("private configuration exported")
	}
	info, e := os.Stat(path)
	must(t, e)
	if info.Mode().Perm()&0044 != 0044 {
		t.Fatal("report not created with shared-readable mode", info.Mode())
	}
	info, e = os.Stat(filepath.Dir(path))
	must(t, e)
	if info.Mode().Perm()&0005 != 0005 {
		t.Fatal("Downloads folder not traversable", info.Mode())
	}
	after, e := os.ReadFile(a.state("ai.json"))
	must(t, e)
	if string(after) != string(before) {
		t.Fatal("private state changed")
	}
	if exists(a.state("report.json")) {
		t.Fatal("export still written to root-only state")
	}
}

func TestRepeatedExportsDoNotOverwrite(t *testing.T) {
	a, _ := fixture(t)
	first, e := a.Export(context.Background())
	must(t, e)
	original, e := os.ReadFile(first)
	must(t, e)
	second, e := a.Export(context.Background())
	must(t, e)
	if first == second {
		t.Fatal("report overwritten")
	}
	current, e := os.ReadFile(first)
	must(t, e)
	if string(current) != string(original) || !exists(second) {
		t.Fatal("previous report was changed")
	}
}

func TestUnavailableDownloadsFailsWithoutPrivateFallback(t *testing.T) {
	a, _ := fixture(t)
	must(t, atomicWrite(a.system("/storage/emulated/0/Download"), []byte("not a directory"), 0600))
	path, e := a.Export(context.Background())
	if e == nil || path != "" || !strings.Contains(e.Error(), "Downloads") {
		t.Fatal(path, e)
	}
	if exists(a.state("report.json")) {
		t.Fatal("silent private fallback")
	}
}

func TestExportRejectsSecondaryUserAndCancelledRequest(t *testing.T) {
	a, f := fixture(t)
	f.user = "10"
	if path, e := a.Export(context.Background()); e == nil || path != "" {
		t.Fatal("exported another user's report")
	}
	if exists(a.system("/storage")) {
		t.Fatal("wrote storage before checking user")
	}
	f.user = "0"
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, e := a.Export(ctx); e == nil {
		t.Fatal("cancel ignored")
	}
}
