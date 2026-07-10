package main

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/corruptmemory/ringer/internal/artifact"
	"github.com/corruptmemory/ringer/internal/hud"
)

// buildRingerBinaryForE2E compiles the real ringer binary once so the mock
// engine has a genuine executable to exec. This mirrors
// internal/runner/runner_test.go's buildRingerBinary and matters for a
// reason specific to running the E2E through runManifestFile: under `go
// test`, os.Executable() returns the TEST binary, and runManifestFile's
// self-exec fallback ("mock" -> Bin: self) would make the runner spawn
// `<test binary> mock-worker {spec}` — that subprocess is a go-test-compiled
// binary whose generated main() falls through to TestMain, and
// main_helper_test.go's TestMain only guards the "hud" subcommand (a no-op
// exit), not "mock-worker", so it would recursively re-run the whole test
// suite (the fork bomb TestMain's own doc comment warns about). Declaring
// "mock" explicitly in the test's config.toml against this freshly-built,
// real ringer binary sidesteps the self-exec fallback entirely: the
// mock-worker subprocess is a genuine `ringer mock-worker` invocation.
func buildRingerBinaryForE2E(t *testing.T) string {
	t.Helper()
	bin := filepath.Join(t.TempDir(), "ringer")
	cmd := exec.Command("go", "build", "-o", bin, "github.com/corruptmemory/ringer/cmd/ringer")
	cmd.Env = append(os.Environ(), "CGO_ENABLED=0")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("build ringer: %v\n%s", err, out)
	}
	return bin
}

// TestArtifactE2E is Task 14's black-box proof of the whole artifact path.
// It runs buildDemoManifest's 3-task mock manifest (a plain pass, a
// multi-file pass with expect_files, and a fail-then-retry-pass) through the
// REAL `run` execution path — runManifestFile, the exact function `ringer
// run`/`ringer demo` call — with state_dir pointed at t.TempDir() and
// --no-dashboard (noDashboard=true) so no HUD spawns and no browser opens.
// It then asserts the on-disk artifact tree + library.json match what a real
// run produces, and that the HUD's real /hud/library panel (hud.New, not a
// stub) renders the entry the writer just wrote — closing the loop: writer
// -> library.json -> HUD library panel.
func TestArtifactE2E(t *testing.T) {
	stateDir := t.TempDir()
	workdir := t.TempDir()
	ringerBin := buildRingerBinaryForE2E(t)

	manifestData, err := buildDemoManifest(workdir)
	if err != nil {
		t.Fatalf("buildDemoManifest: %v", err)
	}
	manifestPath := filepath.Join(t.TempDir(), "ringer.json")
	if err := os.WriteFile(manifestPath, manifestData, 0o644); err != nil {
		t.Fatalf("write manifest: %v", err)
	}

	// config.toml: state_dir hermetically scoped to t.TempDir(), and the
	// "mock" engine declared explicitly against the freshly-built real
	// ringer binary (see buildRingerBinaryForE2E's doc comment for why).
	cfgPath := filepath.Join(t.TempDir(), "config.toml")
	cfgToml := fmt.Sprintf(`state_dir = %q

[engines.mock]
bin = %q
args_template = ["mock-worker", "{spec}"]
isolation = "none"
`, stateDir, ringerBin)
	if err := os.WriteFile(cfgPath, []byte(cfgToml), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	// runManifestFile reads the config path off the package-level `opts`
	// var (the same one the CLI's --config flag populates) — point it at
	// our hermetic config for the duration of this test.
	prevConfig := opts.Config
	opts.Config = cfgPath
	t.Cleanup(func() { opts.Config = prevConfig })

	const runName = "ringer-demo" // buildDemoManifest's fixed RunName

	if err := runManifestFile(context.Background(), manifestPath, 0, "e2e-test", false, true); err != nil {
		t.Fatalf("runManifestFile: %v", err)
	}

	art := filepath.Join(stateDir, "artifacts")
	for _, rel := range []string{"library.json", "index.html"} {
		if _, err := os.Stat(filepath.Join(art, rel)); err != nil {
			t.Errorf("missing %s: %v", rel, err)
		}
	}

	lib := artifact.ReadLibrary(stateDir)
	e, ok := lib.Artifacts[runName]
	if !ok || e.State != "pass" || len(e.Versions) != 1 {
		t.Fatalf("library entry wrong: %+v", e)
	}
	// The version's page exists on disk.
	if _, err := os.Stat(e.Versions[0].Path); err != nil {
		t.Errorf("version page missing: %v", err)
	}
	// The pass tasks' deliverables (expect_files) were harvested and
	// recorded on the version.
	if len(e.Versions[0].Deliverables) == 0 {
		t.Error("no deliverables recorded")
	}

	// HUD panel smoke: the real hud.New server, constructed over the same
	// state dir the writer just wrote to, must render the run name and its
	// pass state on GET /hud/library.
	srv := hud.New(stateDir, nil).Handler()
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/hud/library", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /hud/library status = %d", rec.Code)
	}
	body := rec.Body.String()
	if !strings.Contains(body, runName) {
		t.Errorf("hud library panel missing run name %q:\n%s", runName, body)
	}
	if !strings.Contains(body, "pass") {
		t.Errorf("hud library panel missing pass state:\n%s", body)
	}
}
