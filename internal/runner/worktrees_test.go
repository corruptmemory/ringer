package runner

import (
	"context"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/corruptmemory/ringer/internal/config"
	"github.com/corruptmemory/ringer/internal/logging"
	"github.com/corruptmemory/ringer/internal/manifest"
)

// gitFixtureRepo creates a repo with one commit containing seed.txt.
func gitFixtureRepo(t *testing.T) string {
	t.Helper()
	repo := filepath.Join(t.TempDir(), "parent-repo")
	if err := os.MkdirAll(repo, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repo, "seed.txt"), []byte("seed\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{
		{"init", "-q"},
		{"add", "seed.txt"},
		{"-c", "user.email=t@t", "-c", "user.name=t", "commit", "-q", "-m", "init"},
	} {
		cmd := exec.Command("git", args...)
		cmd.Dir = repo
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	return repo
}

func TestWorktreesModeEndToEnd(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skipf("git not on PATH: %v", err)
	}
	ringerBin := buildRingerBinary(t)
	repo := gitFixtureRepo(t)
	workdir := filepath.Join(t.TempDir(), "wt-work")

	m := &manifest.Manifest{
		RunName: "wt-e2e", Workdir: workdir, Worktrees: true, Repo: repo,
		Tasks: []manifest.Task{
			// Passing task: leaves a report for the snapshot, deliverable
			// visible to the check while the worktree still exists. NOTE
			// the frozen grammar (§9.9): "MOCK_FILE: <path>" with colon.
			{Key: "passer", Engine: "mock", TimeoutS: 30,
				Spec:  "MOCK_FILE: report.md\nwt report body\nMOCK_END\nMOCK_FILE: out.txt\ndone\nMOCK_END",
				Check: "test -f out.txt && test -f seed.txt"},
			// Failing task: worktree must survive for debugging. MOCK_FAIL
			// must be an exact line (the grammar matches the whole line).
			// Verdict is gated entirely by the check (runTask's verdict
			// switch never reads the worker's own exit code), so the check
			// must itself fail for this task to land on FAIL — "false"
			// forces that regardless of MOCK_FAIL's zero-side-effects
			// contract (a check like "test -f out.txt" would also work,
			// since MOCK_FAIL never reaches the MOCK_FILE block, but
			// "false" is the most direct way to force FAIL).
			{Key: "failer", Engine: "mock", TimeoutS: 30,
				Spec:  "MOCK_FAIL",
				Check: "false"},
		},
	}
	engines := map[string]config.EngineConfig{
		"mock": {Bin: ringerBin, ArgsTemplate: []string{"mock-worker", "{spec}"}},
	}
	res, err := Run(context.Background(), Options{
		Manifest: m, Engines: engines, StateDir: t.TempDir(),
		Identity: "test", Stdout: io.Discard, Logger: logging.Default(),
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	verdicts := map[string]string{}
	for _, r := range res.Results {
		verdicts[r.Key] = r.Verdict
	}
	// The check ran INSIDE the worktree: seed.txt (from the repo commit)
	// and out.txt (from the worker) were both present.
	if verdicts["passer"] != "PASS" {
		t.Fatalf("passer = %q, want PASS (check saw repo checkout + worker output)", verdicts["passer"])
	}
	// PASS ⇒ worktree removed…
	if _, err := os.Stat(filepath.Join(workdir, "passer")); !os.IsNotExist(err) {
		t.Fatalf("passer worktree not removed on PASS (stat err = %v)", err)
	}
	// …report snapshotted next to the log first…
	snap := filepath.Join(workdir, "logs", "passer.worker.reports", "report.md")
	if _, err := os.Stat(snap); err != nil {
		t.Fatalf("report snapshot missing at %s: %v", snap, err)
	}
	// …and the worker log survives outside the worktree.
	if _, err := os.Stat(filepath.Join(workdir, "logs", "passer.worker.log")); err != nil {
		t.Fatalf("worker log missing: %v", err)
	}
	// FAIL ⇒ worktree kept for debugging.
	if _, err := os.Stat(filepath.Join(workdir, "failer")); err != nil {
		t.Fatalf("failer worktree must survive: %v", err)
	}
}
