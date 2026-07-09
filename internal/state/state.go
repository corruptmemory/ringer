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

func RegisterActiveRun(stateDir, runID string, pid int, runName, identity, startedAt string) error {
	m := readActiveRaw(stateDir)
	m[runID] = ActiveRun{PID: pid, RunName: runName, Identity: identity, StartedAt: startedAt}
	return atomicWriteJSON(activeRunsPath(stateDir), m)
}

func UnregisterActiveRun(stateDir, runID string) error {
	m := readActiveRaw(stateDir)
	delete(m, runID)
	return atomicWriteJSON(activeRunsPath(stateDir), m)
}

func ReadActiveRuns(stateDir string) (map[string]ActiveRun, error) {
	m := readActiveRaw(stateDir)
	for id, r := range m {
		if !pidAlive(r.PID) {
			delete(m, id)
		}
	}
	return m, nil
}
