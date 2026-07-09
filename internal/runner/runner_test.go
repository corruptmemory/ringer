package runner

import (
	"context"
	"database/sql"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	_ "modernc.org/sqlite" // driver registration only: opens a second, read-only connection to the store's db file for row assertions (see TestRunEndToEndMockEngineWithStore)

	"github.com/corruptmemory/ringer/internal/config"
	"github.com/corruptmemory/ringer/internal/logging"
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

// TestRunTaskDefaultEngineFallsBackToBuiltinCodex is a regression test for
// runTask routing engine resolution through engine.Resolve instead of a raw
// map lookup (opts.Engines[engineName]). A task with Engine:"" — the
// documented default, and the one Preflight already accepts via Resolve — is
// run against an Engines map that deliberately has NO "codex" entry. That
// forces the code down engine.Resolve's engine.BuiltinCodex() fallback path,
// the exact case a raw map lookup cannot handle: the old code would log
// `unknown engine "codex"` and return immediately, without ever building an
// argv or spawning anything.
//
// This machine happens to have a real `codex` binary on PATH (BuiltinCodex
// hardcodes Bin: "codex"), and a unit test must not risk actually invoking
// it. So PATH is shadowed with a fake "codex" script that only touches a
// marker file and exits — the test asserts that marker exists (proof
// BuiltinCodex's bin was actually resolved and spawned) and that the
// captured log never contains "unknown engine" (proof the old, buggy
// short-circuit path was not taken).
func TestRunTaskDefaultEngineFallsBackToBuiltinCodex(t *testing.T) {
	workdir := t.TempDir()
	stateDir := t.TempDir()
	fakeBinDir := t.TempDir()

	marker := filepath.Join(fakeBinDir, "codex.invoked")
	script := "#!/bin/sh\ntouch '" + marker + "'\nexit 0\n"
	if err := os.WriteFile(filepath.Join(fakeBinDir, "codex"), []byte(script), 0o755); err != nil {
		t.Fatalf("write fake codex: %v", err)
	}

	origPath := os.Getenv("PATH")
	t.Cleanup(func() { os.Setenv("PATH", origPath) })
	os.Setenv("PATH", fakeBinDir+string(os.PathListSeparator)+origPath)

	m := &manifest.Manifest{
		RunName: "engine-resolve-default", Workdir: workdir, MaxParallel: 1,
		Tasks: []manifest.Task{
			// Engine:"" exercises the "" -> "codex" default; empty
			// Check/ExpectFiles means Verify trivially passes once the
			// (fake) codex binary runs, so the task completes in one
			// attempt regardless of what the fake script does.
			{Key: "default-engine", Engine: "", Spec: "irrelevant", TimeoutS: 5},
		},
	}
	// Deliberately empty: no "codex" entry, so resolution can only succeed
	// via engine.Resolve's BuiltinCodex() fallback.
	engines := map[string]config.EngineConfig{}

	lg, capt := logging.NewCapture()
	res, err := Run(context.Background(), Options{
		Manifest: m, Engines: engines, StateDir: stateDir, Identity: "test",
		Stdout: os.Stderr, Logger: lg,
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(res.Results) != 1 {
		t.Fatalf("expected 1 result, got %d: %+v", len(res.Results), res.Results)
	}

	if _, statErr := os.Stat(marker); statErr != nil {
		t.Fatalf("fake codex binary was never invoked (BuiltinCodex fallback did not fire): %v\nlog:\n%s", statErr, capt.String())
	}
	if strings.Contains(capt.String(), "unknown engine") {
		t.Fatalf("runTask still hit the raw map-lookup's \"unknown engine\" path instead of engine.Resolve; log:\n%s", capt.String())
	}
	if got := res.Results[0]; got.Verdict != "PASS" || got.Attempts != 1 {
		t.Errorf("task default-engine: got %+v, want Verdict=PASS Attempts=1 (fake codex ran, empty check trivially passes)", got)
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

	// store.Store only exposes CountAttempts today; there's no row-reading
	// API to assert per-row verdict/retry/duration_s through. Rather than
	// grow internal/store's production surface speculatively for this one
	// test (Plan-5 analytics will need real query shapes this test can't
	// predict), open a second connection straight at the same sqlite file
	// using the driver dependency internal/store already requires
	// (modernc.org/sqlite is a direct go.mod dependency, so this adds no new
	// dependency) and read the rows back directly. Run has already returned
	// by this point, so every InsertAttempt has completed and there's no WAL
	// writer contention to race against.
	raw, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("open db for row assertions: %v", err)
	}
	defer raw.Close()

	rows, err := raw.Query(`SELECT verdict, retry, duration_s FROM attempts ORDER BY id`)
	if err != nil {
		t.Fatalf("query attempts: %v", err)
	}
	defer rows.Close()

	type attemptRow struct {
		Verdict   string
		Retry     int
		DurationS float64
	}
	var got []attemptRow
	for rows.Next() {
		var r attemptRow
		if err := rows.Scan(&r.Verdict, &r.Retry, &r.DurationS); err != nil {
			t.Fatalf("scan attempt row: %v", err)
		}
		got = append(got, r)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("rows: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("expected 2 rows, got %d: %+v", len(got), got)
	}

	// Row 0: attempt 1, MOCK_FAIL_ONCE fires with zero filesystem side
	// effects -> ExpectFiles r.txt missing -> FAIL, retry=0 (attempt-1).
	if got[0].Verdict != "FAIL" || got[0].Retry != 0 {
		t.Errorf("row 0: got %+v, want Verdict=FAIL Retry=0", got[0])
	}
	// Row 1: attempt 2, marker spent -> writes r.txt -> PASS, retry=1.
	if got[1].Verdict != "PASS" || got[1].Retry != 1 {
		t.Errorf("row 1: got %+v, want Verdict=PASS Retry=1", got[1])
	}
	for i, r := range got {
		if r.DurationS <= 0 {
			t.Errorf("row %d: DurationS = %v, want > 0", i, r.DurationS)
		}
	}
}
