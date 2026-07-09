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
