package runner

import (
	"context"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/corruptmemory/ringer/internal/config"
	"github.com/corruptmemory/ringer/internal/isolate"
	"github.com/corruptmemory/ringer/internal/jail"
	"github.com/corruptmemory/ringer/internal/logging"
	"github.com/corruptmemory/ringer/internal/manifest"
)

// TestJailedMockEndToEnd runs the full pipeline with isolation="jail": the
// mock engine (the ringer binary itself) executes inside a user-namespace
// chroot, writes its deliverable into the taskdir through the
// host-identical bind, and the HOST-side verifier confirms it. The ringer
// binary lives outside the host-toolchain mounts, so jail_ro_binds carries
// its directory — exercising that key for real.
func TestJailedMockEndToEnd(t *testing.T) {
	if r := jail.CheckUnsharePreflight(); !r.OK() {
		t.Skipf("userns preflight failed: %s", r.Error())
	}
	ringerBin := buildRingerBinary(t)
	workdir := filepath.Join(t.TempDir(), "jailed-work")

	m := &manifest.Manifest{
		RunName: "jail-e2e", Workdir: workdir,
		Tasks: []manifest.Task{
			{Key: "jt", Engine: "mock", TimeoutS: 60,
				Spec:  "MOCK_FILE: out.txt\njailed hello\nMOCK_END",
				Check: "grep -q 'jailed hello' out.txt"},
		},
	}
	engines := map[string]config.EngineConfig{
		"mock": {
			Bin: ringerBin, ArgsTemplate: []string{"mock-worker", "{spec}"},
			Isolation:   "jail",
			JailRoBinds: []string{filepath.Dir(ringerBin)},
		},
	}
	res, err := Run(context.Background(), Options{
		Manifest: m, Engines: engines, StateDir: t.TempDir(),
		Identity: "test", Stdout: io.Discard, Logger: logging.Default(),
		Isolator: &isolate.JailIsolator{Base: filepath.Join(workdir, ".jail")},
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !res.AllPassed {
		t.Fatalf("results = %+v, want PASS (worker wrote through the jail into the host taskdir)", res.Results)
	}
	// Per-task jail scaffolding cleaned up.
	if _, err := os.Stat(filepath.Join(workdir, ".jail", "jt")); !os.IsNotExist(err) {
		t.Fatalf("jail root not cleaned (stat err = %v)", err)
	}
}

// TestWorktreesJailEndToEnd is the spike's probe C in the real pipeline:
// the engine runs `git status` INSIDE the jail, inside a worktree taskdir,
// which only works if the parent repo is bind-mounted read-only at its
// host-identical path (WrapSpec.RepoRO).
//
// The engine is a shell that redirects `git status` into a file INSIDE the
// taskdir (git-status.out) — the taskdir is read-write at its host-identical
// path in the jail, so that file is host-visible after the jailed process
// exits. The Check then greps that file for git's success markers on the
// HOST, after the run (verify.Verify's `sh -c <check>` invariant: the
// worker's own exit code never decides the verdict — only the check does).
// A vacuous `Check: "true"` here would PASS even if RepoRO were completely
// broken, defeating the point of this test; grepping the captured output
// means a broken RepoRO mount surfaces as "fatal: not a git repository" (or
// similar) in git-status.out, the grep finds no success marker, verdict
// FAILs, and AllPassed goes false.
func TestWorktreesJailEndToEnd(t *testing.T) {
	if r := jail.CheckUnsharePreflight(); !r.OK() {
		t.Skipf("userns preflight failed: %s", r.Error())
	}
	if _, err := exec.LookPath("git"); err != nil {
		t.Skipf("git not on PATH: %v", err)
	}
	shBin, err := exec.LookPath("sh")
	if err != nil {
		t.Skipf("sh not on PATH: %v", err)
	}
	repo := gitFixtureRepo(t)
	workdir := filepath.Join(t.TempDir(), "wtjail-work")

	m := &manifest.Manifest{
		RunName: "wtjail-e2e", Workdir: workdir, Worktrees: true, Repo: repo,
		Tasks: []manifest.Task{
			// The ENGINE is a shell running INSIDE the jail: `git -C
			// <taskdir> status` exercises the worktree gitdir pointer from
			// inside the namespace, and its output is captured to a file
			// under the taskdir so the HOST-side check can inspect it. The
			// git status output is captured into a shell variable FIRST and
			// only written to git-status.out afterward — writing straight
			// to a `>` redirect target inside the taskdir would create that
			// file before git status runs (shell redirection happens before
			// exec), so git status would see its own not-yet-written output
			// file as an untracked file and never report a clean tree.
			{Key: "gt", Engine: "gitstatus", TimeoutS: 60,
				Spec:  "unused",
				Check: `grep -qE 'On branch|working tree|nothing to commit' git-status.out`},
		},
	}
	engines := map[string]config.EngineConfig{
		"gitstatus": {
			Bin:          shBin,
			ArgsTemplate: []string{"-c", `out="$(git -C {taskdir} status 2>&1)"; printf '%s\n' "$out" > {taskdir}/git-status.out`},
			Isolation:    "jail",
		},
	}
	res, err := Run(context.Background(), Options{
		Manifest: m, Engines: engines, StateDir: t.TempDir(),
		Identity: "test", Stdout: io.Discard, Logger: logging.Default(),
		Isolator: &isolate.JailIsolator{Base: filepath.Join(workdir, ".jail")},
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !res.AllPassed {
		// Diagnostic for a RepoRO regression, from two sources:
		//   - git-status.out, inside the worktree taskdir. cleanupWorktreeOnPass
		//     only removes the worktree on PASS (verified empirically here:
		//     a FAIL leaves it in place, same as TestWorktreesModeEndToEnd
		//     asserts for its own "failer" task), so on this failing branch
		//     the file — and the "fatal: ..." git error inside it — survives.
		//   - the worker log (workdir/logs/gt.worker.log), which lives
		//     outside the taskdir and survives regardless of verdict. For
		//     THIS engine it's normally empty (the whole script's output is
		//     redirected into git-status.out, not left on the wrapped
		//     process's own stdout/stderr) but it's cheap insurance against
		//     an earlier failure (e.g. the jail setup itself erroring out
		//     before the script ever ran).
		statusTail := "(git-status.out unreadable)"
		if data, rerr := os.ReadFile(filepath.Join(workdir, "gt", "git-status.out")); rerr == nil {
			statusTail = capTail(string(data), 4096)
		}
		logTail := "(worker log unreadable)"
		if data, rerr := os.ReadFile(filepath.Join(workdir, "logs", "gt.worker.log")); rerr == nil {
			logTail = capTail(string(data), 4096)
		}
		t.Fatalf("results = %+v, want PASS (git status inside jail+worktree needs the RepoRO mount)\ngit-status.out tail:\n%s\nworker log tail:\n%s",
			res.Results, statusTail, logTail)
	}
}
