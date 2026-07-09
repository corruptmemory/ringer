package runner

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/corruptmemory/ringer/internal/logging"
	"github.com/corruptmemory/ringer/internal/manifest"
)

// taskReportFilenames mirrors ringer.py TASK_REPORT_FILENAMES: the report
// files snapshotted out of a worktree before it is removed on PASS.
var taskReportFilenames = []string{"report.md", "report.html"}

// prepareTaskDir creates the task's working directory. In worktrees mode
// the taskdir is a fresh `git worktree add` checkout of the manifest repo's
// HEAD (ringer.py:6987-7010); a pre-existing taskdir is an error there — a
// stale worktree would silently reuse another run's checkout. Default mode
// is a plain MkdirAll.
func prepareTaskDir(m *manifest.Manifest, taskDir string) error {
	if !m.Worktrees {
		return os.MkdirAll(taskDir, 0o755)
	}
	if err := os.MkdirAll(filepath.Dir(taskDir), 0o755); err != nil {
		return err
	}
	if _, err := os.Stat(taskDir); err == nil {
		return fmt.Errorf("worktree taskdir already exists: %s", taskDir)
	}
	out, err := exec.Command("git", "-C", m.Repo, "worktree", "add", taskDir, "HEAD").CombinedOutput()
	if err != nil {
		return fmt.Errorf("git worktree add: %v: %s", err, strings.TrimSpace(string(out)))
	}
	return nil
}

// cleanupWorktreeOnPass snapshots the task's report files out of the doomed
// worktree, then removes it (ringer.py:7012-7056). Failures are logged at
// Warn, never fatal: a leftover worktree is an inconvenience, not a broken
// run. No-op outside worktrees mode.
func cleanupWorktreeOnPass(m *manifest.Manifest, lg logging.Logger, taskKey, taskDir, logsDir string) {
	if !m.Worktrees {
		return
	}
	reportsDir := filepath.Join(logsDir, taskKey+".worker.reports")
	for _, name := range taskReportFilenames {
		src := filepath.Join(taskDir, name)
		if _, err := os.Stat(src); err != nil {
			continue
		}
		if err := copyFile(src, filepath.Join(reportsDir, name)); err != nil {
			lg.Warnf("task %s: report snapshot %s: %v", taskKey, name, err)
		}
	}
	out, err := exec.Command("git", "-C", m.Repo, "worktree", "remove", "--force", taskDir).CombinedOutput()
	if err != nil {
		lg.Warnf("task %s: git worktree remove: %v: %s", taskKey, err, strings.TrimSpace(string(out)))
	}
}

// copyFile is a whole-file copy for small report files.
func copyFile(src, dst string) error {
	data, err := os.ReadFile(src)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	return os.WriteFile(dst, data, 0o644)
}
