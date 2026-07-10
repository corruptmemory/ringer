package artifact

import (
	"encoding/json"
	"os"
	"path/filepath"

	"github.com/corruptmemory/ringer/internal/state"
)

type Library struct {
	Artifacts map[string]Entry `json:"artifacts"`
}

type Entry struct {
	LivePath     string    `json:"live_path"`
	State        string    `json:"state"` // live | died | pass | fail
	Identity     string    `json:"identity"`
	CurrentRunID string    `json:"current_run_id"`
	UpdatedAt    string    `json:"updated_at"`
	Versions     []Version `json:"versions"`
}

type Version struct {
	RunID        string        `json:"run_id"`
	Path         string        `json:"path"`
	ReportPath   *string       `json:"report_path"`
	FinishedAt   string        `json:"finished_at"`
	Outcome      string        `json:"outcome"`
	TasksPass    int           `json:"tasks_pass"`
	TasksFail    int           `json:"tasks_fail"`
	Deliverables []Deliverable `json:"deliverables"`
}

// Deliverable is defined in state (the leaf both artifact and the run-state
// snapshot share); aliased here so the frozen library.json schema and the
// existing artifact.Version.Deliverables field are unchanged.
type Deliverable = state.Deliverable

// ReadLibrary loads library.json, degrading a missing or malformed file to
// an empty (non-nil) map (mirrors upstream's {"artifacts": {}} fallback).
func ReadLibrary(stateDir string) Library {
	lib := Library{Artifacts: map[string]Entry{}}
	data, err := os.ReadFile(LibraryPath(stateDir))
	if err != nil {
		return lib
	}
	var parsed Library
	if err := json.Unmarshal(data, &parsed); err != nil || parsed.Artifacts == nil {
		return lib
	}
	return parsed
}

// WriteLibrary atomically writes library.json (tmp + rename).
func WriteLibrary(stateDir string, lib Library) error {
	if lib.Artifacts == nil {
		lib.Artifacts = map[string]Entry{}
	}
	path := LibraryPath(stateDir)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(lib, "", "  ")
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".library-*")
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
