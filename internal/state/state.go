package state

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"syscall"
)

type TaskView struct {
	Key      string `json:"key"`
	Engine   string `json:"engine"`
	Model    string `json:"model"`
	Status   string `json:"status"` // pending|running|passed|failed|timeout
	Attempt  int    `json:"attempt"`
	Tokens   int64  `json:"tokens"`
	Verified string `json:"verified"`
	LogPath  string `json:"log_path"`
}

type RunState struct {
	RunID     string     `json:"run_id"`
	RunName   string     `json:"run_name"`
	Identity  string     `json:"identity"`
	PID       int        `json:"pid"`
	StartedAt string     `json:"started_at"`
	UpdatedAt string     `json:"updated_at"`
	Done      bool       `json:"done"`
	Tasks     []TaskView `json:"tasks"`
}

type ActiveRun struct {
	PID       int    `json:"pid"`
	RunName   string `json:"run_name"`
	Identity  string `json:"identity"`
	Workdir   string `json:"workdir"`
	StartedAt string `json:"started_at"`
}

func atomicWriteJSON(path string, v any) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".tmp-*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		os.Remove(tmpName)
		return err
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmpName)
		return err
	}
	return os.Rename(tmpName, path)
}

func WriteRunState(stateDir string, s RunState) error {
	return atomicWriteJSON(filepath.Join(stateDir, "runs", s.RunID+".json"), s)
}

func activeRunsPath(stateDir string) string { return filepath.Join(stateDir, "active-runs.json") }

func pidAlive(pid int) bool {
	if pid <= 0 {
		// kill(0, 0) probes the caller's own process group and kill(-1, 0)
		// probes every process we may signal — both "exist", so a zero or
		// negative pid from a corrupt entry would otherwise never prune.
		return false
	}
	err := syscall.Kill(pid, 0)
	if err == nil {
		return true
	}
	return errors.Is(err, syscall.EPERM) // exists but not ours
}

func readActiveRaw(stateDir string) map[string]ActiveRun {
	m := map[string]ActiveRun{}
	data, err := os.ReadFile(activeRunsPath(stateDir))
	if err == nil {
		_ = json.Unmarshal(data, &m)
	}
	return m
}

// pruneActiveRuns returns a copy of m with entries whose PID is no longer
// alive removed. Mirrors Python's _prune_active_runs (ringer.py:1818-1836).
func pruneActiveRuns(m map[string]ActiveRun) map[string]ActiveRun {
	pruned := make(map[string]ActiveRun, len(m))
	for id, r := range m {
		if pidAlive(r.PID) {
			pruned[id] = r
		}
	}
	return pruned
}

// writeActiveRuns prunes dead entries then atomically writes the result.
// Mirrors Python's _write_active_runs (ringer.py:1839-1845), which prunes on
// every write so every writer self-cleans the shared file.
func writeActiveRuns(stateDir string, m map[string]ActiveRun) error {
	return atomicWriteJSON(activeRunsPath(stateDir), pruneActiveRuns(m))
}

func RegisterActiveRun(stateDir, runID, identity, runName, workdir string, pid int, startedAt string) error {
	m := readActiveRaw(stateDir)
	m[runID] = ActiveRun{PID: pid, RunName: runName, Identity: identity, Workdir: workdir, StartedAt: startedAt}
	return writeActiveRuns(stateDir, m)
}

func UnregisterActiveRun(stateDir, runID string) error {
	m := readActiveRaw(stateDir)
	delete(m, runID)
	return writeActiveRuns(stateDir, m)
}

// ReadActiveRuns mirrors Python's read_active_runs (ringer.py:1847-1852): it
// prunes dead entries and, if pruning changed anything, re-persists the
// pruned map before returning it. A write failure during that re-persist
// propagates (Python's read_active_runs has no try/except around
// _write_active_runs, so the exception there is not swallowed either).
func ReadActiveRuns(stateDir string) (map[string]ActiveRun, error) {
	raw := readActiveRaw(stateDir)
	pruned := pruneActiveRuns(raw)
	if len(pruned) != len(raw) {
		if err := writeActiveRuns(stateDir, pruned); err != nil {
			return nil, err
		}
	}
	return pruned, nil
}
