package manifest

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

type Task struct {
	Key         string   `json:"key"`
	Spec        string   `json:"spec"`
	Check       string   `json:"check"`
	Engine      string   `json:"engine"` // "" -> "codex" (default) resolved by caller
	Model       string   `json:"model"`
	ExpectFiles []string `json:"expect_files"`
	TimeoutS    int      `json:"timeout_s"` // 0 -> default 900 applied by caller
	FullAccess  bool     `json:"full_access"`
	EngineArgs  []string `json:"engine_args"`
	Verified    string   `json:"verified"`
	TaskType    string   `json:"task_type"`
}

type Manifest struct {
	RunName     string `json:"run_name"`
	Workdir     string `json:"workdir"`
	MaxParallel int    `json:"max_parallel"`
	Worktrees   bool   `json:"worktrees"`
	Repo        string `json:"repo"`
	Tasks       []Task `json:"tasks"`
}

// FromPath reads and validates a manifest JSON file.
// Validation errors are returned joined (all problems at once), not one-at-a-time.
func FromPath(path string) (*Manifest, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("manifest %s: %w", path, err)
	}
	m, err := FromBytes(data)
	if err != nil {
		return nil, fmt.Errorf("manifest %s: %w", path, err)
	}
	return m, nil
}

// FromBytes is the testable core FromPath wraps.
func FromBytes(data []byte) (*Manifest, error) {
	var m Manifest
	if err := json.Unmarshal(data, &m); err != nil {
		return nil, fmt.Errorf("invalid manifest JSON: %w", err)
	}
	var errs []error
	if m.RunName == "" {
		errs = append(errs, errors.New("run_name is required"))
	}
	if m.Workdir == "" {
		errs = append(errs, errors.New("workdir is required"))
	}
	if len(m.Tasks) == 0 {
		errs = append(errs, errors.New("manifest must have at least one task"))
	}
	if m.Worktrees && m.Repo == "" {
		// ringer.py silently falls back to plain directories when repo is
		// missing (worktree ops guard on `repo is not None`) — a silent
		// semantic downgrade. Fail loud instead: deliberate divergence.
		errs = append(errs, errors.New("worktrees mode requires repo (the parent repository each task worktree is checked out from)"))
	}
	if m.MaxParallel < 0 {
		errs = append(errs, errors.New("max_parallel must be >= 0"))
	}
	seen := map[string]bool{}
	for i, tk := range m.Tasks {
		where := fmt.Sprintf("task[%d]", i)
		if tk.Key == "" {
			errs = append(errs, fmt.Errorf("%s: key is required", where))
		} else {
			if seen[tk.Key] {
				errs = append(errs, fmt.Errorf("duplicate task key %q", tk.Key))
			}
			seen[tk.Key] = true
			where = "task " + tk.Key

			// The key becomes the taskdir path component: it must stay
			// inside workdir (ringer.py:7231-7236, moved to load time) and
			// must not shadow the reserved <workdir>/logs directory (Go
			// always writes worker logs there; stricter than Python, which
			// reserves it only in worktrees mode).
			rel, relErr := filepath.Rel(m.Workdir, filepath.Join(m.Workdir, tk.Key))
			if relErr != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
				errs = append(errs, fmt.Errorf("task key escapes workdir: %s", tk.Key))
			} else if rel == "logs" || strings.HasPrefix(rel, "logs"+string(filepath.Separator)) {
				errs = append(errs, fmt.Errorf("task key %q collides with the reserved logs directory", tk.Key))
			}
		}
		if tk.Spec == "" {
			errs = append(errs, fmt.Errorf("%s: spec is required", where))
		}
		if tk.Check == "" {
			errs = append(errs, fmt.Errorf("%s: check is required", where))
		}
	}
	if len(errs) > 0 {
		return nil, errors.Join(errs...)
	}
	return &m, nil
}
