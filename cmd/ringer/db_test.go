package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/corruptmemory/ringer/internal/store"
)

func TestAttemptFromLegacyJSONL(t *testing.T) {
	// legacy row: Python names, no model/task_type -> backfilled.
	row := map[string]any{
		"run_id": "r1", "task_key": "t1", "worker_engine": "codex",
		"verdict": "PASS", "duration_ms": float64(8000), "worker_tokens": float64(120),
		"logged_at": "2026-07-10T00:00:00Z", "retry": false,
	}
	runModel := func(runID, taskKey string) string { return "gpt-5.5" }
	mapping := map[string]string{"r1:t1": "code"}
	a := attemptFromJSONL(row, runModel, mapping)
	if a.Engine != "codex" || a.Model != "gpt-5.5" || a.TaskType != "code" {
		t.Fatalf("mapping wrong: %+v", a)
	}
	if a.DurationS != 8.0 || a.Tokens != 120 || a.CreatedAt != "2026-07-10T00:00:00Z" {
		t.Fatalf("field conversion wrong: %+v", a)
	}
}

func TestTaskTypePrecedence(t *testing.T) {
	m := map[string]string{"r1:t1": "exact", "r1": "run", "name:r": "prefix"}
	if got := taskTypeFromMapping(m, "r1", "t1"); got != "exact" {
		t.Fatalf("want exact, got %q", got)
	}
	if got := taskTypeFromMapping(m, "r1", "zz"); got != "run" {
		t.Fatalf("want run, got %q", got)
	}
	if got := taskTypeFromMapping(m, "rX", "zz"); got != "prefix" {
		t.Fatalf("want prefix, got %q", got)
	}
}

// TestTaskTypePrefixTierValueAgnostic locks the FROZEN tier-3 contract
// (scripts/backfill_model_log.py:109-123): the longest matching "name:<prefix>"
// wins the max search by LENGTH ALONE (value-agnostic), and the winner's value
// is truthiness-gated only AFTER the loop. So a longer prefix with an EMPTY
// value wins the contest and voids the whole tier — it does NOT fall back to a
// shorter non-empty prefix. Case 1 is genuine RED against the old code, which
// folded `v != ""` into the max-update condition and wrongly returned the
// shorter prefix's value.
func TestTaskTypePrefixTierValueAgnostic(t *testing.T) {
	// Case 1: longer prefix with an empty value voids the tier.
	m1 := map[string]string{"name:ab": "shortval", "name:abcdef": ""}
	if got := taskTypeFromMapping(m1, "abcdefgh", "zz"); got != "" {
		t.Fatalf("empty longer prefix must void the tier: want %q, got %q", "", got)
	}
	// Case 2: longer non-empty prefix wins over a shorter non-empty one.
	m2 := map[string]string{"name:ab": "x", "name:abcd": "y"}
	if got := taskTypeFromMapping(m2, "abcdef", "zz"); got != "y" {
		t.Fatalf("longest non-empty prefix must win: want %q, got %q", "y", got)
	}
}

func TestRetryFrom(t *testing.T) {
	cases := []struct {
		name string
		row  map[string]any
		want int
	}{
		{"bool true", map[string]any{"retry": true}, 1},
		{"bool false", map[string]any{"retry": false}, 0},
		{"string true", map[string]any{"retry": "true"}, 1},
		{"string TRUE mixed case", map[string]any{"retry": "TRUE"}, 1},
		{"string false", map[string]any{"retry": "false"}, 0},
		{"numeric retry count > 1", map[string]any{"retry": float64(2)}, 2},
		{"numeric zero", map[string]any{"retry": float64(0)}, 0},
		{"notes fallback", map[string]any{"notes": "attempt failed; retry=true"}, 1},
		{"no retry info", map[string]any{}, 0},
		{"unrelated notes", map[string]any{"notes": "all good"}, 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := retryFrom(tc.row); got != tc.want {
				t.Errorf("retryFrom(%+v) = %d, want %d", tc.row, got, tc.want)
			}
		})
	}
}

func TestDurationSeconds(t *testing.T) {
	cases := []struct {
		name string
		row  map[string]any
		want float64
	}{
		{"duration_s wins over duration_ms", map[string]any{"duration_s": 3.5, "duration_ms": float64(9000)}, 3.5},
		{"duration_ms fallback converts ms->s", map[string]any{"duration_ms": float64(2500)}, 2.5},
		{"neither present", map[string]any{}, 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := durationSeconds(tc.row); got != tc.want {
				t.Errorf("durationSeconds(%+v) = %v, want %v", tc.row, got, tc.want)
			}
		})
	}
}

func TestTokensFrom(t *testing.T) {
	cases := []struct {
		name string
		row  map[string]any
		want int64
	}{
		{"tokens wins over worker_tokens", map[string]any{"tokens": float64(50), "worker_tokens": float64(999)}, 50},
		{"worker_tokens fallback", map[string]any{"worker_tokens": float64(120)}, 120},
		{"neither present -> unknown sentinel", map[string]any{}, -1},
		{"worker_tokens null -> unknown sentinel", map[string]any{"worker_tokens": nil}, -1},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := tokensFrom(tc.row); got != tc.want {
				t.Errorf("tokensFrom(%+v) = %d, want %d", tc.row, got, tc.want)
			}
		})
	}
}

func TestFirstNonEmpty(t *testing.T) {
	if got := firstNonEmpty("", "", "c"); got != "c" {
		t.Errorf("firstNonEmpty(\"\",\"\",\"c\") = %q, want %q", got, "c")
	}
	if got := firstNonEmpty("a", "b"); got != "a" {
		t.Errorf("firstNonEmpty(\"a\",\"b\") = %q, want %q", got, "a")
	}
	if got := firstNonEmpty(); got != "" {
		t.Errorf("firstNonEmpty() = %q, want empty", got)
	}
}

// TestRunStateModelLookup ports model_from_run_state's contract: match
// tasks[].key==task_key -> task.model; a matched task with a blank model,
// an unmatched task_key, or a missing run-state file all resolve to "".
func TestRunStateModelLookup(t *testing.T) {
	dir := t.TempDir()
	runState := `{"tasks":[{"key":"t1","model":"gpt-5.5"},{"key":"t2","model":""}]}`
	if err := os.WriteFile(filepath.Join(dir, "r1.json"), []byte(runState), 0o644); err != nil {
		t.Fatal(err)
	}
	lookup := runStateModelLookup(dir)

	if got := lookup("r1", "t1"); got != "gpt-5.5" {
		t.Errorf("lookup(r1,t1) = %q, want gpt-5.5", got)
	}
	if got := lookup("r1", "t2"); got != "" {
		t.Errorf("lookup(r1,t2) = %q, want empty (model present but blank)", got)
	}
	if got := lookup("r1", "missing-task"); got != "" {
		t.Errorf("lookup(r1,missing-task) = %q, want empty", got)
	}
	if got := lookup("nope", "t1"); got != "" {
		t.Errorf("lookup(nope,t1) = %q, want empty (no run-state file)", got)
	}
	if got := lookup("r1", ""); got != "" {
		t.Errorf("lookup with empty task key = %q, want empty (short-circuit)", got)
	}
}

// TestExportImportRoundTrip is brief Step 5: seed a store, export via
// AllAttempts + the same JSON encoding dbExportCmd uses, then re-import each
// line through attemptFromJSONL and confirm the row count and every field
// survive using native (Go-tagged) field names, with no mapping/run-state
// backfill needed (every field is already populated).
func TestExportImportRoundTrip(t *testing.T) {
	s, err := store.Open(filepath.Join(t.TempDir(), "ringer.db"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer s.Close()

	want := []store.Attempt{
		{RunID: "r1", RunName: "demo", TaskKey: "t1", Engine: "codex", Model: "gpt-5.5",
			TaskType: "code", Verdict: "PASS", Retry: 0, DurationS: 2.5, Tokens: 300,
			CheckOutput: "ok", Identity: "orchestrator", CreatedAt: "2026-07-10T00:00:00Z"},
		{RunID: "r1", RunName: "demo", TaskKey: "t2", Engine: "claude", Model: "opus",
			TaskType: "probe", Verdict: "FAIL", Retry: 1, DurationS: 4.0, Tokens: -1,
			CheckOutput: "fail: timeout", Identity: "orchestrator", CreatedAt: "2026-07-10T00:01:00Z"},
	}
	for _, a := range want {
		if err := s.InsertAttempt(a); err != nil {
			t.Fatalf("InsertAttempt: %v", err)
		}
	}

	rows, err := s.AllAttempts()
	if err != nil {
		t.Fatalf("AllAttempts: %v", err)
	}
	if len(rows) != len(want) {
		t.Fatalf("AllAttempts returned %d rows, want %d", len(rows), len(want))
	}

	// Mimic dbExportCmd.Execute: one JSON object per line, Go-native Attempt JSON.
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	for _, a := range rows {
		if err := enc.Encode(a); err != nil {
			t.Fatalf("Encode: %v", err)
		}
	}

	noModel := func(string, string) string { return "" } // must not be needed: every row is already fully populated
	var got []store.Attempt
	sc := bufio.NewScanner(&buf)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" {
			continue
		}
		var row map[string]any
		if err := json.Unmarshal([]byte(line), &row); err != nil {
			t.Fatalf("Unmarshal: %v", err)
		}
		got = append(got, attemptFromJSONL(row, noModel, nil))
	}
	if err := sc.Err(); err != nil {
		t.Fatalf("scan: %v", err)
	}
	if len(got) != len(want) {
		t.Fatalf("round-trip produced %d rows, want %d", len(got), len(want))
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("round-trip row[%d] = %+v, want %+v", i, got[i], want[i])
		}
	}
}

// writeStateDirConfig points opts.Config at a config.toml under dir with
// state_dir=dir, so loadConfig()-based commands resolve their DB to
// dir/ringer.db. Restores the previous opts.Config on test cleanup, matching
// the pattern established in catalog_test.go / main_test.go.
func writeStateDirConfig(t *testing.T, dir string) {
	t.Helper()
	cfgPath := filepath.Join(dir, "config.toml")
	if err := os.WriteFile(cfgPath, []byte("state_dir = "+strconv.Quote(dir)+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	prevConfig := opts.Config
	opts.Config = cfgPath
	t.Cleanup(func() { opts.Config = prevConfig })
}

// captureStdout redirects os.Stdout for the duration of fn and returns what
// was written, so tests can assert on dbImportCmd's printed summary line
// without dbImportCmd.Execute needing to change its verbatim brief signature.
func captureStdout(t *testing.T, fn func() error) (string, error) {
	t.Helper()
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("os.Pipe: %v", err)
	}
	prev := os.Stdout
	os.Stdout = w
	fnErr := fn()
	w.Close()
	os.Stdout = prev
	var buf bytes.Buffer
	if _, err := buf.ReadFrom(r); err != nil {
		t.Fatalf("read captured stdout: %v", err)
	}
	return buf.String(), fnErr
}

// TestDBExportThenImportCLI drives the actual CLI commands end to end
// (dbExportCmd.Execute -> dbImportCmd.Execute across two separate stores),
// not just the attemptFromJSONL helper, so the full export/import path
// (including loadConfig, store.Open, and InsertAttempt) is proven.
func TestDBExportThenImportCLI(t *testing.T) {
	srcDir := t.TempDir()
	writeStateDirConfig(t, srcDir)

	seed, err := store.Open(filepath.Join(srcDir, "ringer.db"))
	if err != nil {
		t.Fatalf("Open source store: %v", err)
	}
	a := store.Attempt{
		RunID: "r1", RunName: "demo", TaskKey: "t1", Engine: "codex", Model: "gpt-5.5",
		TaskType: "code", Verdict: "PASS", Retry: 0, DurationS: 1.5, Tokens: 42,
		CheckOutput: "ok", Identity: "orchestrator", CreatedAt: "2026-07-10T00:00:00Z",
	}
	if err := seed.InsertAttempt(a); err != nil {
		t.Fatalf("InsertAttempt: %v", err)
	}
	seed.Close()

	exportPath := filepath.Join(srcDir, "export.jsonl")
	if err := (&dbExportCmd{Out: exportPath}).Execute(nil); err != nil {
		t.Fatalf("export Execute: %v", err)
	}

	destDir := t.TempDir()
	writeStateDirConfig(t, destDir)
	out, err := captureStdout(t, func() error {
		return (&dbImportCmd{JSONL: exportPath}).Execute(nil)
	})
	if err != nil {
		t.Fatalf("import Execute: %v", err)
	}
	if !strings.Contains(out, "1 imported, 0 skipped") {
		t.Fatalf("import summary = %q, want \"1 imported, 0 skipped\"", out)
	}

	dest, err := store.Open(filepath.Join(destDir, "ringer.db"))
	if err != nil {
		t.Fatalf("Open dest store: %v", err)
	}
	defer dest.Close()
	rows, err := dest.AllAttempts()
	if err != nil {
		t.Fatalf("AllAttempts: %v", err)
	}
	if len(rows) != 1 || rows[0] != a {
		t.Fatalf("round-trip row = %+v, want [%+v]", rows, a)
	}
}

// TestDBImportDryRunDoesNotWriteStore locks the global no-silent-failures /
// dry-run constraint: --dry-run must not open the store for writes at all
// (not merely "open but roll back"), so the DB file must never be created.
func TestDBImportDryRunDoesNotWriteStore(t *testing.T) {
	dir := t.TempDir()
	writeStateDirConfig(t, dir)

	row := map[string]any{
		"run_id": "r1", "task_key": "t1", "worker_engine": "codex", "verdict": "PASS",
		"duration_ms": 1000, "worker_tokens": 10, "logged_at": "2026-07-10T00:00:00Z",
	}
	b, err := json.Marshal(row)
	if err != nil {
		t.Fatal(err)
	}
	jsonlPath := filepath.Join(dir, "legacy.jsonl")
	if err := os.WriteFile(jsonlPath, append(b, '\n'), 0o644); err != nil {
		t.Fatal(err)
	}

	out, err := captureStdout(t, func() error {
		return (&dbImportCmd{JSONL: jsonlPath, DryRun: true}).Execute(nil)
	})
	if err != nil {
		t.Fatalf("dry-run Execute: %v", err)
	}
	if !strings.Contains(out, "1 imported, 0 skipped (dry-run, nothing written)") {
		t.Fatalf("dry-run summary = %q, want the dry-run marker", out)
	}
	if _, err := os.Stat(filepath.Join(dir, "ringer.db")); !os.IsNotExist(err) {
		t.Fatalf("dry-run must not create the store file; stat err = %v", err)
	}
}

// TestDBImportSkipsMalformedAndIncompleteRows locks the no-silent-failures
// constraint on the write path: unparseable JSON, and rows missing
// run_id/task_key/verdict, must be classified as skipped (counted +
// reported), not silently dropped, while the one valid row still imports.
func TestDBImportSkipsMalformedAndIncompleteRows(t *testing.T) {
	dir := t.TempDir()
	writeStateDirConfig(t, dir)

	valid := `{"run_id":"r1","task_key":"t1","worker_engine":"codex","verdict":"PASS","duration_ms":1000,"worker_tokens":10,"logged_at":"2026-07-10T00:00:00Z"}`
	missingRunID := `{"task_key":"t1","verdict":"PASS"}`
	missingTaskKey := `{"run_id":"r3","verdict":"PASS"}`
	missingVerdict := `{"run_id":"r2","task_key":"t2"}`
	malformed := `{not json`
	content := strings.Join([]string{valid, missingRunID, missingTaskKey, missingVerdict, malformed, ""}, "\n")
	jsonlPath := filepath.Join(dir, "legacy.jsonl")
	if err := os.WriteFile(jsonlPath, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	out, err := captureStdout(t, func() error {
		return (&dbImportCmd{JSONL: jsonlPath}).Execute(nil)
	})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !strings.Contains(out, "1 imported, 4 skipped") {
		t.Fatalf("summary = %q, want \"1 imported, 4 skipped\"", out)
	}

	s, err := store.Open(filepath.Join(dir, "ringer.db"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer s.Close()
	n, err := s.CountAttempts()
	if err != nil || n != 1 {
		t.Fatalf("CountAttempts = %d, %v; want 1, nil", n, err)
	}
}

// TestDBIntegrityAndCheckpointCommands is a thin CLI-layer smoke test:
// store.Integrity/Checkpoint already have direct unit tests in
// internal/store; this just proves the dbIntegrityCmd/dbCheckpointCmd
// Execute methods wire loadConfig -> store.Open -> the right store method.
func TestDBIntegrityAndCheckpointCommands(t *testing.T) {
	dir := t.TempDir()
	writeStateDirConfig(t, dir)
	s, err := store.Open(filepath.Join(dir, "ringer.db"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	s.Close()

	if err := (&dbIntegrityCmd{}).Execute(nil); err != nil {
		t.Errorf("dbIntegrityCmd.Execute: %v", err)
	}
	if err := (&dbCheckpointCmd{}).Execute(nil); err != nil {
		t.Errorf("dbCheckpointCmd.Execute: %v", err)
	}
}
