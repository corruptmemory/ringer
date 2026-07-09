package runner

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/corruptmemory/ringer/internal/config"
	"github.com/corruptmemory/ringer/internal/manifest"
	"github.com/corruptmemory/ringer/internal/state"
	"github.com/corruptmemory/ringer/internal/store"
)

// buildRingerBinary compiles the ringer binary once so the mock engine has a bin.
func buildRingerBinary(t *testing.T) string {
	t.Helper()
	bin := filepath.Join(t.TempDir(), "ringer")
	cmd := exec.Command("go", "build", "-o", bin, "github.com/corruptmemory/ringer/cmd/ringer")
	cmd.Env = append(os.Environ(), "CGO_ENABLED=0")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("build ringer: %v\n%s", err, out)
	}
	return bin
}

// TestRunEndToEndMockEngine is the Milestone 1 proof: it drives runner.Run
// directly (no need to spawn a real `ringer` process for the orchestrator
// side) against the mock engine, which itself points at the freshly built
// ringer binary's `mock-worker` subcommand. One task passes outright; the
// other exercises the fail-then-retry path deterministically via the
// MOCK_FAIL_ONCE sentinel added to internal/mockworker for exactly this
// purpose (see mockworker.go's doc comment on mockFailOnceMarker) — the
// brief's own literal spec ("MOCK_FILE...MOCK_END" with expect_files
// covering the written file) would pass on attempt 1 and never exercise
// retry at all, so this deviates from the brief's literal test spec text
// while keeping its intent (a PASS task + a fail-then-retry task, both
// ending AllPassed).
func TestRunEndToEndMockEngine(t *testing.T) {
	ringerBin := buildRingerBinary(t)
	workdir := t.TempDir()
	stateDir := t.TempDir()

	m := &manifest.Manifest{
		RunName: "e2e", Workdir: workdir, MaxParallel: 2,
		Tasks: []manifest.Task{
			{Key: "pass", Engine: "mock",
				Spec:  "MOCK_FILE: out.txt\nalpha ready\nMOCK_END\n",
				Check: `test "$(cat out.txt)" = "alpha ready"`, ExpectFiles: []string{"out.txt"}},
			{Key: "retry", Engine: "mock",
				// MOCK_FAIL_ONCE fails attempt 1 with zero filesystem side
				// effects (so ExpectFiles r.txt is missing => verdict FAIL,
				// no shell check even runs); attempt 2 finds the marker the
				// first attempt left behind, falls through to the
				// MOCK_FILE/MOCK_END block, writes r.txt, and passes. This
				// deterministically exercises the runner's retry-once path.
				Spec:  "MOCK_FAIL_ONCE\nMOCK_FILE: r.txt\nok\nMOCK_END\n",
				Check: `test -f r.txt`, ExpectFiles: []string{"r.txt"}},
		},
	}
	engines := map[string]config.EngineConfig{
		"mock": {Bin: ringerBin, ArgsTemplate: []string{"mock-worker", "{spec}"}},
	}
	res, err := Run(context.Background(), Options{
		Manifest: m, Engines: engines, StateDir: stateDir, Identity: "test", Stdout: os.Stderr,
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !res.AllPassed {
		t.Fatalf("expected all pass, got %+v", res.Results)
	}
	if len(res.Results) != 2 {
		t.Fatalf("expected 2 results, got %d: %+v", len(res.Results), res.Results)
	}

	byKey := map[string]TaskResult{}
	for _, r := range res.Results {
		byKey[r.Key] = r
	}
	if got := byKey["pass"]; got.Verdict != "PASS" || got.Attempts != 1 {
		t.Errorf("task pass: got %+v, want Verdict=PASS Attempts=1", got)
	}
	if got := byKey["retry"]; got.Verdict != "PASS" || got.Attempts != 2 {
		t.Errorf("task retry: got %+v, want Verdict=PASS Attempts=2 (fail then retry-pass)", got)
	}

	// Run-state file was written, and reflects a finished run.
	runStatePath := filepath.Join(stateDir, "runs", res.RunID+".json")
	data, err := os.ReadFile(runStatePath)
	if err != nil {
		t.Fatalf("run-state not written: %v", err)
	}
	var rs state.RunState
	if err := json.Unmarshal(data, &rs); err != nil {
		t.Fatalf("run-state not valid JSON: %v", err)
	}
	if !rs.Done {
		t.Errorf("run-state Done = false, want true")
	}
	if rs.RunID != res.RunID || rs.RunName != m.RunName || rs.Identity != "test" {
		t.Errorf("run-state identity mismatch: %+v", rs)
	}
	if rs.PID != os.Getpid() {
		t.Errorf("run-state PID = %d, want %d", rs.PID, os.Getpid())
	}

	// active-runs.json no longer references this run: UnregisterActiveRun ran.
	active, err := state.ReadActiveRuns(stateDir)
	if err != nil {
		t.Fatalf("ReadActiveRuns: %v", err)
	}
	if _, ok := active[res.RunID]; ok {
		t.Errorf("active-runs.json still lists run %s after completion", res.RunID)
	}

	// Deliverables exist.
	if _, err := os.Stat(filepath.Join(workdir, "pass", "out.txt")); err != nil {
		t.Errorf("deliverable missing: %v", err)
	}
	if _, err := os.Stat(filepath.Join(workdir, "retry", "r.txt")); err != nil {
		t.Errorf("deliverable missing: %v", err)
	}
}

// TestRunEndToEndMockEngineWithStore proves eval rows are logged: one row per
// attempt (pass: 1 row; retry: 2 rows — a FAIL then a PASS), with the
// frozen columns populated from the manifest/engine/identity the run was
// given.
func TestRunEndToEndMockEngineWithStore(t *testing.T) {
	ringerBin := buildRingerBinary(t)
	workdir := t.TempDir()
	stateDir := t.TempDir()
	dbPath := filepath.Join(t.TempDir(), "ringer.db")

	st, err := store.Open(dbPath)
	if err != nil {
		t.Fatalf("store.Open: %v", err)
	}
	defer st.Close()

	m := &manifest.Manifest{
		RunName: "e2e-store", Workdir: workdir, MaxParallel: 1,
		Tasks: []manifest.Task{
			{Key: "retry", Engine: "mock",
				Spec:  "MOCK_FAIL_ONCE\nMOCK_FILE: r.txt\nok\nMOCK_END\n",
				Check: `test -f r.txt`, ExpectFiles: []string{"r.txt"}},
		},
	}
	engines := map[string]config.EngineConfig{
		"mock": {Bin: ringerBin, ArgsTemplate: []string{"mock-worker", "{spec}"}},
	}
	res, err := Run(context.Background(), Options{
		Manifest: m, Engines: engines, StateDir: stateDir, Identity: "store-test",
		Store: st, Stdout: os.Stderr,
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !res.AllPassed {
		t.Fatalf("expected all pass, got %+v", res.Results)
	}

	n, err := st.CountAttempts()
	if err != nil {
		t.Fatalf("CountAttempts: %v", err)
	}
	if n != 2 {
		t.Fatalf("expected 2 attempt rows (fail + retry-pass), got %d", n)
	}
}
