package verify

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestVerifyPass(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "a.txt"), []byte("data"), 0o644)
	r := Verify(context.Background(), dir, `test -f a.txt`, []string{"a.txt"}, 10*time.Second)
	if !r.Pass || r.ExitCode != 0 {
		t.Fatalf("expected pass, got %+v", r)
	}
}

func TestVerifyFailExit(t *testing.T) {
	r := Verify(context.Background(), t.TempDir(), `echo nope; exit 3`, nil, 10*time.Second)
	if r.Pass || r.ExitCode != 3 {
		t.Fatalf("expected fail exit 3, got %+v", r)
	}
	if !contains(r.Output, "nope") {
		t.Errorf("output must capture check stdout, got %q", r.Output)
	}
}

func TestVerifyMissingExpectFile(t *testing.T) {
	r := Verify(context.Background(), t.TempDir(), `true`, []string{"ghost.txt"}, 10*time.Second)
	if r.Pass || len(r.Missing) != 1 || r.Missing[0] != "ghost.txt" {
		t.Fatalf("expected missing ghost.txt, got %+v", r)
	}
}

func TestVerifyEmptyFileIsMissing(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "empty.txt"), nil, 0o644)
	r := Verify(context.Background(), dir, `true`, []string{"empty.txt"}, 10*time.Second)
	if r.Pass || len(r.Missing) != 1 {
		t.Fatalf("empty file must count as missing, got %+v", r)
	}
}

func TestVerifyTimeout(t *testing.T) {
	r := Verify(context.Background(), t.TempDir(), `sleep 5`, nil, 200*time.Millisecond)
	if r.Pass || !r.TimedOut {
		t.Fatalf("expected timeout, got %+v", r)
	}
}

// TestVerifyTimeoutWithOrphanedChild guards against a hard-to-notice hang: a
// check that backgrounds a grandchild process before the shell itself blocks
// (here via `wait`) leaves that grandchild holding the inherited stdout/stderr
// pipes open. Canceling the context only kills the direct child (sh); Wait()
// inside CombinedOutput then blocks on pipe EOF until the orphaned grandchild
// exits on its own — 30s later — unless Cmd.WaitDelay forces the pipes closed.
// This test asserts the "hard timeout" promised by the Verify doc comment is
// actually hard.
func TestVerifyTimeoutWithOrphanedChild(t *testing.T) {
	start := time.Now()
	r := Verify(context.Background(), t.TempDir(), `sleep 30 & wait`, nil, 200*time.Millisecond)
	elapsed := time.Since(start)
	if r.Pass || !r.TimedOut {
		t.Fatalf("expected timeout, got %+v", r)
	}
	if elapsed >= 10*time.Second {
		t.Fatalf("Verify took %s; orphaned grandchild kept Wait() blocked past WaitDelay", elapsed)
	}
}

func contains(s, sub string) bool { return len(s) >= len(sub) && (s == sub || indexOf(s, sub) >= 0) }
func indexOf(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}
