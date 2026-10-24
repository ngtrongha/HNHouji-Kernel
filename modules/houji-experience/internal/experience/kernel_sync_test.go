package experience

import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/binary"
	"encoding/hex"
	"os"
	"strings"
	"testing"
)

const fixtureConfig = "CONFIG_HZ=250\nCONFIG_ARM64_4K_PAGES=y\nCONFIG_ZRAM=m\nCONFIG_ZSMALLOC=m\nCONFIG_NTSYNC=y\n"

func writeRuntimeConfig(t *testing.T, a *App, data string) {
	t.Helper()
	var b bytes.Buffer
	z := gzip.NewWriter(&b)
	_, e := z.Write([]byte(data))
	must(t, e)
	must(t, z.Close())
	must(t, atomicWrite(a.system("/proc/config.gz"), b.Bytes(), 0600))
}
func seedKernel(t *testing.T, a *App) {
	t.Helper()
	writeRuntimeConfig(t, a, fixtureConfig)
	id := bytes.Repeat([]byte{0xab}, 20)
	notes := make([]byte, 12)
	binary.LittleEndian.PutUint32(notes, 4)
	binary.LittleEndian.PutUint32(notes[4:], 20)
	binary.LittleEndian.PutUint32(notes[8:], 3)
	notes = append(notes, []byte("GNU\x00")...)
	notes = append(notes, id...)
	must(t, atomicWrite(a.system("/sys/kernel/notes"), notes, 0600))
	m := KernelManifest{Schema: 1, Device: "houji", ModuleContract: 1, KernelRelease: "6.1.177-android14-r11", ConfigSHA256: digest([]byte(fixtureConfig)), BuildID: hex.EncodeToString(id), ImageSHA256: strings.Repeat("a", 64), SourceRevision: strings.Repeat("b", 40)}
	must(t, atomicJSON(a.system("/data/adb/houji-kernel/manifest.json"), m))
}

func TestKernelMatchesConfigAndBuildMetadata(t *testing.T) {
	a, _ := fixture(t)
	s := a.KernelSync(context.Background())
	if !s.Ready || s.State != "matched" || s.Features["CONFIG_NTSYNC"] != "y" {
		t.Fatal(s)
	}
}
func TestUnknownKernelMetadataBlocksRulesButKeepsDiagnostics(t *testing.T) {
	a, f := fixture(t)
	configured(t, a, true)
	must(t, os.Remove(a.system("/data/adb/houji-kernel/manifest.json")))
	s := a.KernelSync(context.Background())
	if s.Ready || s.State != "manifest-missing" {
		t.Fatal(s)
	}
	must(t, a.Step(context.Background(), "com.example.game", false))
	if f.writes != 0 {
		t.Fatal("unverified kernel received settings")
	}
	var r Runtime
	must(t, readJSON(a.state("runtime.json"), &r))
	if r.Reason != "kernel-manifest-missing" {
		t.Fatal(r)
	}
	if a.Doctor(context.Background())["status"] == nil {
		t.Fatal("diagnostics blocked")
	}
}
func TestKernelMismatchRestoresOwnedSettings(t *testing.T) {
	a, f := fixture(t)
	configured(t, a, true)
	ctx := context.Background()
	must(t, a.Step(ctx, "com.example.game", false))
	if f.game != "performance" {
		t.Fatal(f.game)
	}
	writeRuntimeConfig(t, a, fixtureConfig+"CONFIG_CIFS=y\n")
	s := a.KernelSync(ctx)
	if s.Ready || s.State != "build-mismatch" {
		t.Fatal(s)
	}
	must(t, a.Step(ctx, "com.example.game", false))
	if f.game != "standard" || f.doze["com.example.chat"] {
		t.Fatal("mismatch did not restore owned settings")
	}
}
func TestUnsafeKernelConfigIsNeverOverridden(t *testing.T) {
	for _, data := range []string{strings.ReplaceAll(fixtureConfig, "CONFIG_HZ=250", "CONFIG_HZ=1000"), strings.ReplaceAll(fixtureConfig, "CONFIG_ZRAM=m", "CONFIG_ZRAM=y"), strings.ReplaceAll(fixtureConfig, "CONFIG_ARM64_4K_PAGES=y", "CONFIG_ARM64_4K_PAGES=n")} {
		a, f := fixture(t)
		writeRuntimeConfig(t, a, data)
		s := a.KernelSync(context.Background())
		if s.Ready || s.State != "config-policy-mismatch" {
			t.Fatal(s)
		}
		must(t, a.Step(context.Background(), "com.example.game", false))
		if f.writes != 0 {
			t.Fatal("changed unsafe config")
		}
	}
}
func TestBuildIDMustMatchRunningKernel(t *testing.T) {
	a, _ := fixture(t)
	notes, e := os.ReadFile(a.system("/sys/kernel/notes"))
	must(t, e)
	notes[len(notes)-1] ^= 1
	must(t, os.WriteFile(a.system("/sys/kernel/notes"), notes, 0600))
	s := a.KernelSync(context.Background())
	if s.Ready || s.State != "build-mismatch" {
		t.Fatal(s)
	}
}
func TestMissingOrInvalidIKCONFIGAndNotesFailClosed(t *testing.T) {
	a, _ := fixture(t)
	must(t, os.Remove(a.system("/proc/config.gz")))
	if s := a.KernelSync(context.Background()); s.Ready || s.State != "config-unavailable" {
		t.Fatal(s)
	}
	must(t, atomicWrite(a.system("/proc/config.gz"), []byte("broken gzip"), 0600))
	if s := a.KernelSync(context.Background()); s.Ready || s.State != "config-invalid" {
		t.Fatal(s)
	}
	seedKernel(t, a)
	must(t, os.WriteFile(a.system("/sys/kernel/notes"), []byte("broken note"), 0600))
	if s := a.KernelSync(context.Background()); s.Ready || s.State != "build-id-unavailable" {
		t.Fatal(s)
	}
}

func TestPairedModuleMustMatchExactKernelArtifact(t *testing.T) {
	a, _ := fixture(t)
	ctx := context.Background()
	var metadata KernelManifest
	must(t, readJSON(a.system("/data/adb/houji-kernel/manifest.json"), &metadata))
	must(t, atomicJSON(a.Paths.Module+"/kernel.json", metadata))
	if s := a.KernelSync(ctx); !s.Ready || !s.Paired {
		t.Fatal(s)
	}
	// Release/config/build ID still match runtime and the installed sidecar,
	// but this module belongs to another Image artifact.
	metadata.ImageSHA256 = strings.Repeat("c", 64)
	must(t, atomicJSON(a.Paths.Module+"/kernel.json", metadata))
	if s := a.KernelSync(ctx); s.Ready || s.State != "package-build-mismatch" {
		t.Fatal(s)
	}
	seedKnobs(t, a)
	must(t, a.Step(ctx, "", false))
	knobValue(t, a, "vm.page-cluster", "3")
}

func TestBrokenPairedModuleMetadataNeverFallsBackToGeneric(t *testing.T) {
	a, _ := fixture(t)
	must(t, atomicWrite(a.Paths.Module+"/kernel.json", []byte("broken"), 0600))
	if s := a.KernelSync(context.Background()); s.Ready || s.State != "package-manifest-invalid" {
		t.Fatal(s)
	}
}

func pairedFixture(t *testing.T, a *App, spoof bool) KernelManifest {
	t.Helper()
	var metadata KernelManifest
	must(t, readJSON(a.system("/data/adb/houji-kernel/manifest.json"), &metadata))
	if spoof {
		data := fixtureConfig + "CONFIG_KSU_SUSFS_SPOOF_UNAME=y\n"
		writeRuntimeConfig(t, a, data)
		metadata.ConfigSHA256 = digest([]byte(data))
	}
	must(t, atomicJSON(a.Paths.Module+"/kernel.json", metadata))
	must(t, os.Remove(a.system("/data/adb/houji-kernel/manifest.json")))
	return metadata
}

func TestPairedModuleUsesEmbeddedMetadataWithoutRecoverySidecar(t *testing.T) {
	a, _ := fixture(t)
	pairedFixture(t, a, false)
	seedKnobs(t, a)
	ctx := context.Background()
	s := a.KernelSync(ctx)
	if !s.Ready || !s.Paired || s.MetadataSource != "module" {
		t.Fatal(s)
	}
	must(t, a.Step(ctx, "", false))
	knobValue(t, a, "vm.page-cluster", "0")
	knobValue(t, a, "f2fs:dm-42", "20")
	if exists(a.system("/data/adb/houji-kernel/manifest.json")) {
		t.Fatal("synthesized a kernel sidecar")
	}
}

func TestPairedModuleAllowsSpoofedUnameOnlyWithMatchingLiveBuildAndConfig(t *testing.T) {
	a, f := fixture(t)
	metadata := pairedFixture(t, a, true)
	seedKnobs(t, a)
	f.release = "6.1.177-android14-8-g33591097-ab78788955"
	ctx := context.Background()
	s := a.KernelSync(ctx)
	if !s.Ready || !s.Paired || !s.UnameDiffers || s.ExpectedRelease != metadata.KernelRelease || s.KernelRelease != f.release {
		t.Fatal(s)
	}
	must(t, a.Step(ctx, "", false))
	knobValue(t, a, "vm.page-cluster", "0")
	metadata.BuildID = strings.Repeat("c", 40)
	must(t, atomicJSON(a.Paths.Module+"/kernel.json", metadata))
	if s = a.KernelSync(ctx); s.Ready || s.State != "build-mismatch" {
		t.Fatal(s)
	}
	must(t, a.Step(ctx, "", false))
	knobValue(t, a, "vm.page-cluster", "3")
}

func TestUnexplainedUnameDifferenceRemainsDiagnosticOnly(t *testing.T) {
	a, f := fixture(t)
	pairedFixture(t, a, false)
	seedKnobs(t, a)
	f.release = "6.1.177-android14-unexpected"
	if s := a.KernelSync(context.Background()); s.Ready || s.State != "release-mismatch" {
		t.Fatal(s)
	}
	must(t, a.Step(context.Background(), "", false))
	knobValue(t, a, "vm.page-cluster", "3")
}

func TestPairedModuleIgnoresSidecarForAnOlderBuild(t *testing.T) {
	a, _ := fixture(t)
	metadata := pairedFixture(t, a, false)
	metadata.BuildID = strings.Repeat("c", 40)
	metadata.ConfigSHA256 = strings.Repeat("d", 64)
	must(t, atomicJSON(a.system("/data/adb/houji-kernel/manifest.json"), metadata))
	if s := a.KernelSync(context.Background()); !s.Ready || s.MetadataSource != "module" {
		t.Fatal(s)
	}
}

func TestMissingSidecarNeverAllowsWrongPairedConfig(t *testing.T) {
	a, _ := fixture(t)
	metadata := pairedFixture(t, a, true)
	seedKnobs(t, a)
	metadata.ConfigSHA256 = strings.Repeat("d", 64)
	must(t, atomicJSON(a.Paths.Module+"/kernel.json", metadata))
	if s := a.KernelSync(context.Background()); s.Ready || s.State != "build-mismatch" {
		t.Fatal(s)
	}
	must(t, a.Step(context.Background(), "", false))
	knobValue(t, a, "vm.page-cluster", "3")
}
