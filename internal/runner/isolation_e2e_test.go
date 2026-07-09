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
func TestWorktreesJailEndToEnd(t *testing.T) {
	if r := jail.CheckUnsharePreflight(); !r.OK() {
		t.Skipf("userns preflight failed: %s", r.Error())
	}
	gitBin, err := exec.LookPath("git")
	if err != nil {
		t.Skipf("git not on PATH: %v", err)
	}
	repo := gitFixtureRepo(t)
	workdir := filepath.Join(t.TempDir(), "wtjail-work")

	m := &manifest.Manifest{
		RunName: "wtjail-e2e", Workdir: workdir, Worktrees: true, Repo: repo,
		Tasks: []manifest.Task{
			// The ENGINE is git itself: `git -C <taskdir> status` exercises
			// the worktree gitdir pointer from inside the namespace.
			{Key: "gt", Engine: "gitstatus", TimeoutS: 60,
				Spec:  "unused",
				Check: "true"},
		},
	}
	engines := map[string]config.EngineConfig{
		"gitstatus": {
			Bin: gitBin, ArgsTemplate: []string{"-C", "{taskdir}", "status"},
			Isolation: "jail",
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
		t.Fatalf("results = %+v, want PASS (git status inside jail+worktree needs the RepoRO mount)", res.Results)
	}
}
