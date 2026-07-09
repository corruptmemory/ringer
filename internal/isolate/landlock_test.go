package isolate

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestLandlockWrapShapesTheSpawn(t *testing.T) {
	scratch := t.TempDir()
	taskDir := t.TempDir()
	stateDir := t.TempDir()
	iso := &LandlockIsolator{Self: "/opt/ringer/ringer", ScratchDir: scratch}
	w, err := iso.Wrap(WrapSpec{
		Key: "t1", Bin: "/usr/bin/tool", Argv: []string{"--flag", "v"},
		TaskDir: taskDir, StateDirs: []string{stateDir},
	})
	if err != nil {
		t.Fatalf("Wrap: %v", err)
	}
	if w.Bin != "/opt/ringer/ringer" {
		t.Fatalf("Bin = %q, want the ringer binary (trampoline)", w.Bin)
	}
	if w.Argv[0] != "landlock-exec" {
		t.Fatalf("Argv[0] = %q, want landlock-exec", w.Argv[0])
	}
	joined := strings.Join(w.Argv, " ")
	sep := " -- "
	pre, post, found := strings.Cut(joined, sep)
	if !found {
		t.Fatalf("argv lacks the -- separator: %v", w.Argv)
	}
	for _, want := range []string{"--rw " + taskDir, "--rw " + stateDir, "--ro /usr", "--ro /etc"} {
		if !strings.Contains(pre, want) {
			t.Fatalf("rules missing %q in %q", want, pre)
		}
	}
	if !strings.HasPrefix(post, "/usr/bin/tool --flag v") {
		t.Fatalf("post-separator command = %q", post)
	}
	// TMPDIR points into the per-task scratch under ScratchDir.
	wantScratch := filepath.Join(scratch, "t1")
	foundTmp := false
	for _, e := range w.Env {
		if e == "TMPDIR="+wantScratch {
			foundTmp = true
		}
	}
	if !foundTmp {
		t.Fatalf("Env lacks TMPDIR=%s: %v", wantScratch, w.Env)
	}
	if _, err := os.Stat(wantScratch); err != nil {
		t.Fatalf("scratch not created: %v", err)
	}
	if err := w.Cleanup(); err != nil {
		t.Fatalf("Cleanup: %v", err)
	}
	if _, err := os.Stat(wantScratch); !os.IsNotExist(err) {
		t.Fatal("scratch survived Cleanup")
	}
}

// TestLandlockTrampolineEnforces is the fallback-tier equivalent of the
// jail live test: it builds the ringer binary, runs a probe through the
// landlock-exec trampoline, and asserts write-inside-taskdir works while
// write-outside is denied. Runs on any Linux ≥ 5.13 — including GitHub
// runners, where the jail tests skip.
func TestLandlockTrampolineEnforces(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("landlock is Linux-only")
	}
	if _, ok := LandlockABI(); !ok {
		t.Skip("kernel lacks Landlock")
	}
	shBin, err := exec.LookPath("sh")
	if err != nil {
		t.Skipf("sh not on PATH: %v", err)
	}
	bin := buildRingerForIsolate(t)
	taskDir := t.TempDir()
	home, err := os.UserHomeDir()
	if err != nil {
		t.Skipf("no home dir: %v", err)
	}
	// The denied probe targets $HOME: normally user-writable, NOT covered
	// by any rule — only Landlock denies it. (It is only ever written if
	// enforcement is broken, i.e. on test failure.)
	denied := filepath.Join(home, ".ringer-landlock-probe-denied.txt")
	defer os.Remove(denied) // belt-and-braces: clean up if enforcement failed
	probe := "echo ok > allowed.txt && (echo x > " + denied + " 2>/dev/null && echo WROTE || echo DENIED) && cat /etc/hostname >/dev/null && echo READ-OK"
	iso := &LandlockIsolator{Self: bin, ScratchDir: t.TempDir()}
	w, err := iso.Wrap(WrapSpec{Key: "ll", Bin: shBin, Argv: []string{"-c", probe}, TaskDir: taskDir})
	if err != nil {
		t.Fatalf("Wrap: %v", err)
	}
	defer w.Cleanup()
	cmd := exec.Command(w.Bin, w.Argv...)
	cmd.Dir = taskDir
	cmd.Env = append(os.Environ(), w.Env...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("trampoline: %v\n%s", err, out)
	}
	text := string(out)
	if !strings.Contains(text, "DENIED") {
		t.Fatalf("write outside the rules was NOT denied:\n%s", text)
	}
	if !strings.Contains(text, "READ-OK") {
		t.Fatalf("toolchain read failed under landlock:\n%s", text)
	}
	if _, err := os.Stat(filepath.Join(taskDir, "allowed.txt")); err != nil {
		t.Fatalf("write inside taskdir failed: %v", err)
	}
}

// buildRingerForIsolate compiles the ringer binary for trampoline tests
// (mirrors internal/runner's buildRingerBinary, which lives in another
// package).
func buildRingerForIsolate(t *testing.T) string {
	t.Helper()
	bin := filepath.Join(t.TempDir(), "ringer")
	cmd := exec.Command("go", "build", "-o", bin, "github.com/corruptmemory/ringer/cmd/ringer")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("go build: %v\n%s", err, out)
	}
	return bin
}
