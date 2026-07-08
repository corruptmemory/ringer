//go:build spike

// internal/jail/spike_worktree_test.go
//
// SPIKE (design spec §13.2): a git worktree's .git is a FILE containing
// "gitdir: <parent-repo>/.git/worktrees/<name>" — an ABSOLUTE HOST path
// pointing back into the parent repo. Inside a chroot jail, that absolute
// path only resolves if the parent repo is mounted at the SAME
// (host-identical) path inside the jail. This spike measures exactly what
// is needed for `git status` (and `git diff`) to work inside a jailed
// worktree, isolating the variable ONE AT A TIME:
//
//   - Probe A (both mismatched): worktree AND parent at jail-internal
//     paths. Establishes the baseline failure.
//   - Probe B (both host-identical): worktree AND parent at their
//     host-identical absolute paths, parent RO. Establishes the baseline
//     success.
//   - Probe C (worktree jail-internal, parent host-identical): the
//     ISOLATING probe. A worktree's .git file only encodes the PARENT's
//     absolute path (gitdir: <parent>/.git/worktrees/<name>); git
//     status/diff resolve via that path and never consult a reverse
//     pointer keyed on the worktree's own location. So C tests whether
//     ONLY the parent needs a host-identical path while the worktree can
//     live at a convenience path (/workspace). Whatever C shows is the
//     finding — do not assume the hypothesis.
//
// Run manually: go test -tags=spike -run TestSpikeWorktreeInJail -v ./internal/jail/
package jail

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// run executes a host-side (non-jailed) command, failing the test on error.
// Used only to build the fixture repo/worktree, not to probe the jail.
func run(t *testing.T, dir string, name string, args ...string) string {
	t.Helper()
	cmd := exec.Command(name, args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("%s %v in %s: %v\n%s", name, args, dir, err, out)
	}
	return string(out)
}

func TestSpikeWorktreeInJail(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skipf("git not on PATH: %v", err)
	}
	if r := CheckUnsharePreflight(); !r.OK() {
		t.Skipf("userns preflight failed: %s", r.Error())
	}

	scratch := t.TempDir()
	repo := filepath.Join(scratch, "repo")
	wt := filepath.Join(scratch, "wt-alpha")

	// Parent repo with one commit that includes a real (non-empty) tracked
	// file, so we can also probe `git diff` against an uncommitted edit in
	// the worktree, not just `git status`.
	if err := os.MkdirAll(repo, 0o755); err != nil {
		t.Fatalf("mkdir repo: %v", err)
	}
	run(t, repo, "git", "init", "-q")
	if err := os.WriteFile(filepath.Join(repo, "hello.txt"), []byte("hello\n"), 0o644); err != nil {
		t.Fatalf("write hello.txt: %v", err)
	}
	run(t, repo, "git", "add", "hello.txt")
	run(t, repo, "git", "-c", "user.email=s@s", "-c", "user.name=s", "commit", "-m", "init", "-q")
	run(t, repo, "git", "worktree", "add", "-q", wt)

	// Make an uncommitted edit in the worktree BEFORE jailing anything, so
	// probe B's `git diff` has something to show. The edit happens on the
	// host filesystem; the jail's rw bind mount of wt exposes the same
	// inode, so this is visible inside the jail without any extra step.
	if err := os.WriteFile(filepath.Join(wt, "hello.txt"), []byte("hello\nworld\n"), 0o644); err != nil {
		t.Fatalf("edit hello.txt in worktree: %v", err)
	}

	// --- Probe A (baseline failure): BOTH worktree and parent mounted at
	// jail-internal paths that do NOT match their host paths. The
	// worktree's .git file points (via an absolute host path) at
	// <repo>/.git/worktrees/wt-alpha; inside the jail that path must
	// resolve to something, and here it deliberately does not. (Note this
	// varies two things at once; Probe C isolates which one matters.)
	rootA := filepath.Join(scratch, "jailroot-a")
	jA := NewUnshareJail(rootA)
	mountsA := append(HostMounts(rootA),
		BindMount(wt, filepath.Join(rootA, "workspace"), false),    // worktree rw, jail-internal path
		BindMount(repo, filepath.Join(rootA, "parent-repo"), true), // parent RO, MISMATCHED path — the experiment
		TmpfsMount(filepath.Join(rootA, "tmp")),
	)
	if err := jA.Setup(mountsA); err != nil {
		t.Fatalf("jail A setup: %v", err)
	}
	defer jA.Teardown()

	outA, errA := jA.Command("git", "-C", "/workspace", "-c", "safe.directory=*", "status").CombinedOutput()
	t.Logf("PROBE A (both at mismatched jail-internal paths): err=%v\n%s", errA, outA)

	// --- Probe B: both worktree and parent repo mounted at their
	// host-identical absolute paths. Parent mounted READ-ONLY — testing
	// whether RO is sufficient for status/diff (commit needs
	// .git/worktrees/<name> writable, which we do not test here).
	//
	// Mount ORDER matters: t.TempDir() lives under the host's /tmp, so the
	// host-identical bind targets for wt/repo land *underneath* the jail's
	// own /tmp (e.g. <rootB>/tmp/TestSpikeWorktreeInJail.../001/repo). The
	// TmpfsMount for /tmp must therefore be set up BEFORE those nested
	// binds, or the later tmpfs mount would shadow them.
	rootB := filepath.Join(scratch, "jailroot-b")
	jB := NewUnshareJail(rootB)
	mountsB := append(HostMounts(rootB), TmpfsMount(filepath.Join(rootB, "tmp")))
	mountsB = append(mountsB,
		BindMount(wt, filepath.Join(rootB, wt), false),    // worktree rw, host-identical path
		BindMount(repo, filepath.Join(rootB, repo), true), // parent RO, host-identical path
	)
	if err := jB.Setup(mountsB); err != nil {
		t.Fatalf("jail B setup: %v", err)
	}
	defer jB.Teardown()

	// Diagnostic only (not gating): what does ns-root's uid mapping look
	// like, and does git complain about dubious ownership WITHOUT
	// safe.directory set? This isolates the incidental noise from the real
	// path-mismatch finding.
	idOut, idErr := jB.Command("id").CombinedOutput()
	t.Logf("id inside jail B: err=%v\n%s", idErr, idOut)

	rawOut, rawErr := jB.Command("git", "-C", wt, "status").CombinedOutput()
	t.Logf("PROBE B diagnostic (host-identical paths, NO safe.directory): err=%v\n%s", rawErr, rawOut)

	outB, errB := jB.Command("git", "-C", wt, "-c", "safe.directory=*", "status").CombinedOutput()
	t.Logf("PROBE B (host-identical paths, parent RO, safe.directory=*): err=%v\n%s", errB, outB)

	diffOut, diffErr := jB.Command("git", "-C", wt, "-c", "safe.directory=*", "diff").CombinedOutput()
	t.Logf("PROBE B git diff (host-identical paths, parent RO): err=%v\n%s", diffErr, diffOut)

	if errB != nil {
		t.Fatalf("VERDICT: even host-identical RO parent insufficient for git status — record details: %v\n%s", errB, outB)
	}
	if diffErr != nil {
		t.Logf("VERDICT (diff): host-identical RO parent insufficient for git diff — record details: %v\n%s", diffErr, diffOut)
	} else {
		t.Log("VERDICT (diff): git diff also succeeds with parent RO")
	}

	// --- Probe C (the ISOLATING probe): worktree at a JAIL-INTERNAL path
	// (/workspace, RW), parent repo at its HOST-IDENTICAL absolute path
	// (RO). If this succeeds, only the parent needs a host-identical path;
	// the worktree can live at a convenience path. No safe.directory,
	// consistent with Probe B's finding that it isn't required here.
	//
	// Same tmpfs-ordering gotcha: the parent's host-identical bind target
	// lands under the jail's /tmp, so TmpfsMount(/tmp) must precede it.
	// The worktree's /workspace target is NOT under /tmp, so its order is
	// unconstrained.
	rootC := filepath.Join(scratch, "jailroot-c")
	jC := NewUnshareJail(rootC)
	mountsC := append(HostMounts(rootC), TmpfsMount(filepath.Join(rootC, "tmp")))
	mountsC = append(mountsC,
		BindMount(wt, filepath.Join(rootC, "workspace"), false), // worktree rw, JAIL-INTERNAL path
		BindMount(repo, filepath.Join(rootC, repo), true),       // parent RO, host-identical path
	)
	if err := jC.Setup(mountsC); err != nil {
		t.Fatalf("jail C setup: %v", err)
	}
	defer jC.Teardown()

	outC, errC := jC.Command("git", "-C", "/workspace", "status").CombinedOutput()
	t.Logf("PROBE C (worktree at jail-internal /workspace, parent host-identical RO): status err=%v\n%s", errC, outC)

	diffOutC, diffErrC := jC.Command("git", "-C", "/workspace", "diff").CombinedOutput()
	t.Logf("PROBE C git diff (worktree at /workspace, parent host-identical RO): err=%v\n%s", diffErrC, diffOutC)

	if errA == nil {
		t.Log("VERDICT (probe A): both-mismatched did NOT break git status — hypothesis wrong, record reality")
	} else {
		t.Log("VERDICT (probe A): both-mismatched breaks git status (baseline failure)")
	}

	if errC == nil {
		t.Log("VERDICT (probe C): ISOLATED — worktree at jail-internal /workspace works when parent is host-identical." +
			" Only the PARENT needs a host-identical mount path; the worktree can live at a convenience path.")
	} else {
		t.Logf("VERDICT (probe C): worktree at jail-internal path FAILS even with parent host-identical: %v\n%s"+
			" — both must be host-identical (now actually isolated).", errC, outC)
	}
	if diffErrC == nil {
		t.Log("VERDICT (probe C diff): git diff also succeeds with worktree at /workspace + parent host-identical RO")
	} else {
		t.Logf("VERDICT (probe C diff): git diff fails: %v\n%s", diffErrC, diffOutC)
	}
}
