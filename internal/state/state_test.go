package state

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestWriteRunStateAtomic(t *testing.T) {
	dir := t.TempDir()
	s := RunState{RunID: "r1", RunName: "demo", PID: os.Getpid(), Tasks: []TaskView{{Key: "a", Status: "passed"}}}
	if err := WriteRunState(dir, s); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, "runs", "r1.json")); err != nil {
		t.Fatalf("run state file not written: %v", err)
	}
}

// deadPID starts a short-lived child process, waits for it to exit, and
// returns its PID. The PID is guaranteed dead but (barring PID reuse, which
// t.TempDir()-isolated tests don't need to defend against) not yet recycled.
func deadPID(t *testing.T) int {
	t.Helper()
	cmd := exec.Command("true")
	if err := cmd.Run(); err != nil {
		t.Fatalf("spawning short-lived process: %v", err)
	}
	return cmd.Process.Pid
}

func TestActiveRunsRoundTripAndPrune(t *testing.T) {
	dir := t.TempDir()
	// Live PID (us) survives; a dead PID is pruned.
	if err := RegisterActiveRun(dir, "live", "id", "n", "wd", os.Getpid(), "t"); err != nil {
		t.Fatal(err)
	}
	if err := RegisterActiveRun(dir, "dead", "id", "n", "wd", deadPID(t), "t"); err != nil {
		t.Fatal(err)
	}
	runs, err := ReadActiveRuns(dir)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := runs["live"]; !ok {
		t.Error("live run pruned incorrectly")
	}
	if _, ok := runs["dead"]; ok {
		t.Error("dead run not pruned")
	}
}

func TestUnregister(t *testing.T) {
	dir := t.TempDir()
	RegisterActiveRun(dir, "x", "id", "n", "wd", os.Getpid(), "t")
	if err := UnregisterActiveRun(dir, "x"); err != nil {
		t.Fatal(err)
	}
	runs, _ := ReadActiveRuns(dir)
	if _, ok := runs["x"]; ok {
		t.Error("run not unregistered")
	}
}

// TestReadActiveRunsPersistsPrune asserts that ReadActiveRuns doesn't just
// prune in memory — it re-persists the pruned map to disk, matching Python's
// read_active_runs (ringer.py:1847-1852), which re-writes whenever pruning
// changes the map.
func TestReadActiveRunsPersistsPrune(t *testing.T) {
	dir := t.TempDir()
	if err := RegisterActiveRun(dir, "live", "id", "n", "wd", os.Getpid(), "t"); err != nil {
		t.Fatal(err)
	}
	if err := RegisterActiveRun(dir, "dead", "id", "n", "wd", deadPID(t), "t"); err != nil {
		t.Fatal(err)
	}

	if _, err := ReadActiveRuns(dir); err != nil {
		t.Fatal(err)
	}

	raw := readRawFile(t, dir)
	if _, ok := raw["dead"]; ok {
		t.Error("dead entry still present on disk after ReadActiveRuns")
	}
	if _, ok := raw["live"]; !ok {
		t.Error("live entry missing on disk after ReadActiveRuns")
	}
}

// TestRegisterPrunesOnWrite asserts that RegisterActiveRun self-cleans the
// shared file: registering a live entry after a dead one was registered
// should prune the dead entry as a side effect of that write, matching
// Python's _write_active_runs (ringer.py:1839-1845), which prunes on every
// write.
func TestRegisterPrunesOnWrite(t *testing.T) {
	dir := t.TempDir()
	if err := RegisterActiveRun(dir, "dead", "id", "n", "wd", deadPID(t), "t"); err != nil {
		t.Fatal(err)
	}
	if err := RegisterActiveRun(dir, "live", "id", "n", "wd", os.Getpid(), "t"); err != nil {
		t.Fatal(err)
	}

	raw := readRawFile(t, dir)
	if _, ok := raw["dead"]; ok {
		t.Error("dead entry not pruned by the registering write")
	}
	if _, ok := raw["live"]; !ok {
		t.Error("live entry missing on disk")
	}
}

// TestActiveRunWorkdirRoundTrips asserts the workdir field, added to match
// Python's register_active_run (ringer.py:1856-1873), survives a
// register -> read round trip intact. Losing it on a read-modify-write would
// strip workdir from entries the shared Python process wrote.
func TestActiveRunWorkdirRoundTrips(t *testing.T) {
	dir := t.TempDir()
	const wantWorkdir = "/home/jim/projects/ringer/some/workdir"
	if err := RegisterActiveRun(dir, "r1", "id", "n", wantWorkdir, os.Getpid(), "t"); err != nil {
		t.Fatal(err)
	}
	runs, err := ReadActiveRuns(dir)
	if err != nil {
		t.Fatal(err)
	}
	got, ok := runs["r1"]
	if !ok {
		t.Fatal("registered run missing from ReadActiveRuns result")
	}
	if got.Workdir != wantWorkdir {
		t.Errorf("Workdir = %q, want %q", got.Workdir, wantWorkdir)
	}
}

// readRawFile reads active-runs.json directly off disk, bypassing any
// pruning ReadActiveRuns would otherwise apply, so tests can assert on
// exactly what's persisted.
func readRawFile(t *testing.T, dir string) map[string]ActiveRun {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(dir, "active-runs.json"))
	if err != nil {
		t.Fatalf("reading active-runs.json: %v", err)
	}
	var raw map[string]ActiveRun
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("unmarshaling active-runs.json: %v", err)
	}
	return raw
}

func TestPruneDropsNonPositivePIDs(t *testing.T) {
	stateDir := t.TempDir()
	// Register a run, then corrupt its PID to 0 on disk — the shared file is
	// written by two eras of code, so defensive reading is part of the contract.
	if err := RegisterActiveRun(stateDir, "run-zero", "id", "name", "/wd", os.Getpid(), "2026-07-09T00:00:00Z"); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(stateDir, "active-runs.json")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	corrupted := strings.Replace(string(data), fmt.Sprintf(`"pid": %d`, os.Getpid()), `"pid": 0`, 1)
	if corrupted == string(data) {
		t.Fatal("test setup: pid substitution did not take")
	}
	if err := os.WriteFile(path, []byte(corrupted), 0o644); err != nil {
		t.Fatal(err)
	}
	runs, err := ReadActiveRuns(stateDir)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := runs["run-zero"]; ok {
		t.Fatal("pid=0 entry survived pruning; kill(0,0) probes our own process group, not a process")
	}
}
