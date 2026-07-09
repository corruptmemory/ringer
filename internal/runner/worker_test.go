package runner

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestRunWorkerCapturesOutputAndExit(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	logPath := filepath.Join(dir, "worker.log")
	var mirror bytes.Buffer

	out := runWorker(context.Background(), "sh", []string{"-c", "echo hello; exit 7"}, dir, logPath, &mirror, 5*time.Second)

	if out.Err != nil {
		t.Fatalf("unexpected error: %v", out.Err)
	}
	if out.TimedOut {
		t.Fatalf("expected TimedOut=false")
	}
	if out.ExitCode != 7 {
		t.Fatalf("expected ExitCode=7, got %d", out.ExitCode)
	}

	logBytes, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("reading log file: %v", err)
	}
	if !strings.Contains(string(logBytes), "hello") {
		t.Fatalf("log file missing %q: %q", "hello", string(logBytes))
	}
	if !strings.Contains(mirror.String(), "hello") {
		t.Fatalf("mirror missing %q: %q", "hello", mirror.String())
	}
}

func TestRunWorkerTimeoutKills(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	logPath := filepath.Join(dir, "worker.log")
	var mirror bytes.Buffer

	start := time.Now()
	out := runWorker(context.Background(), "sleep", []string{"30"}, dir, logPath, &mirror, 200*time.Millisecond)
	elapsed := time.Since(start)

	if out.Err != nil {
		t.Fatalf("unexpected error: %v", out.Err)
	}
	if !out.TimedOut {
		t.Fatalf("expected TimedOut=true, got outcome %+v", out)
	}
	// sleep responds to SIGTERM immediately, so we shouldn't need the 5s grace
	// period before SIGKILL. Comfortably under that bound proves SIGTERM did it.
	if elapsed >= 5*time.Second {
		t.Fatalf("expected SIGTERM to kill well under the grace period, took %s", elapsed)
	}
}

func TestRunWorkerTimeoutKillsProcessGroup(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	logPath := filepath.Join(dir, "worker.log")
	var mirror bytes.Buffer

	// The shell backgrounds a sleep (a grandchild-ish process that inherits
	// the stdout pipe fd at fork time, before the shell itself execs into
	// the second sleep). It shares the leader's process group but is not
	// cmd.Process itself. A leader-only kill leaves it holding the pipe's
	// write end open, so cmd.Wait()'s copy goroutine never sees EOF until
	// it independently exits up to 30s later. Only a process-group signal
	// (or the grandchild happening to die on its own) lets Wait() return
	// promptly.
	start := time.Now()
	out := runWorker(context.Background(), "sh", []string{"-c", "sleep 30 & exec sleep 30"}, dir, logPath, &mirror, 300*time.Millisecond)
	elapsed := time.Since(start)

	if out.Err != nil {
		t.Fatalf("unexpected error: %v", out.Err)
	}
	if !out.TimedOut {
		t.Fatalf("expected TimedOut=true, got outcome %+v", out)
	}
	// Generous bound for CI: well under the grandchild's own 30s sleep,
	// which is only achievable if the process group (not just the leader)
	// was signaled.
	if elapsed >= 10*time.Second {
		t.Fatalf("expected process-group kill to finish well under the grandchild's lifetime, took %s", elapsed)
	}
}

func TestRunWorkerTimeoutSIGKILLFallback(t *testing.T) {
	// Deliberately not t.Parallel(): this test shortens the package-level
	// termGrace var to keep the SIGKILL fallback path fast to exercise.
	// Every other test in this file calls t.Parallel(), and per the
	// testing package's documented behavior, a parallel test's body only
	// proceeds past its Parallel() call once every non-parallel top-level
	// test (this one included) has finished running -- so this test's
	// set/restore of termGrace happens entirely in a window where no
	// parallel test body can be reading it concurrently, regardless of
	// where in the file it's declared.
	orig := termGrace
	termGrace = 300 * time.Millisecond
	t.Cleanup(func() { termGrace = orig })

	dir := t.TempDir()
	logPath := filepath.Join(dir, "worker.log")
	var mirror bytes.Buffer

	// Ignoring SIGTERM forces the SIGKILL fallback: the process can only
	// die once the (shortened) grace period elapses and SIGKILL lands.
	start := time.Now()
	out := runWorker(context.Background(), "sh", []string{"-c", `trap "" TERM; sleep 30`}, dir, logPath, &mirror, 200*time.Millisecond)
	elapsed := time.Since(start)

	if out.Err != nil {
		t.Fatalf("unexpected error: %v", out.Err)
	}
	if !out.TimedOut {
		t.Fatalf("expected TimedOut=true, got outcome %+v", out)
	}
	// Lower bound: must have actually waited out the timeout plus the
	// shortened grace period (~500ms) rather than dying immediately, which
	// would indicate SIGTERM wasn't really ignored. Upper bound: generous
	// for CI, but well under the process's own 30s sleep -- only reachable
	// via the SIGKILL fallback.
	if elapsed < 400*time.Millisecond {
		t.Fatalf("finished suspiciously fast (%s); expected to wait out the timeout and grace period", elapsed)
	}
	if elapsed >= 10*time.Second {
		t.Fatalf("expected SIGKILL fallback to finish well under the process's lifetime, took %s", elapsed)
	}
}

func TestRunWorkerClosesStdin(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	logPath := filepath.Join(dir, "worker.log")
	var mirror bytes.Buffer

	// If stdin were not closed (backed by /dev/null), `cat` would block forever
	// waiting for input and the run would time out.
	out := runWorker(context.Background(), "sh", []string{"-c", "cat; echo done"}, dir, logPath, &mirror, 5*time.Second)

	if out.Err != nil {
		t.Fatalf("unexpected error: %v", out.Err)
	}
	if out.TimedOut {
		t.Fatalf("expected TimedOut=false; stdin was not closed promptly")
	}
	if !strings.Contains(mirror.String(), "done") {
		t.Fatalf("mirror missing %q: %q", "done", mirror.String())
	}
}
