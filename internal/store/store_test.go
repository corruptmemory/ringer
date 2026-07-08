// internal/store/store_test.go
package store

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestOpenInsertCount(t *testing.T) {
	s, err := Open(filepath.Join(t.TempDir(), "ringer.db"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer s.Close()

	a := Attempt{
		RunID: "r1", RunName: "demo", TaskKey: "alpha", Engine: "mock",
		Model: "m", TaskType: "probe", Verdict: "PASS", Retry: 0,
		DurationS: 1.5, Tokens: 42, CheckOutput: "ok", Identity: "test",
		CreatedAt: "2026-07-08T00:00:00Z",
	}
	if err := s.InsertAttempt(a); err != nil {
		t.Fatalf("InsertAttempt: %v", err)
	}
	n, err := s.CountAttempts()
	if err != nil || n != 1 {
		t.Fatalf("CountAttempts = %d, %v; want 1, nil", n, err)
	}
	if err := s.Checkpoint(); err != nil {
		t.Errorf("Checkpoint: %v", err)
	}
	if err := s.Integrity(); err != nil {
		t.Errorf("Integrity: %v", err)
	}
}

// TestCheckpointTruncatesWAL proves Checkpoint() actually performs a
// TRUNCATE-mode WAL checkpoint, not just that it returns nil. It grows the
// WAL with enough page-touching writes to leave a non-trivial -wal file,
// then asserts the file is truncated to zero bytes after Checkpoint().
func TestCheckpointTruncatesWAL(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "ringer.db")
	s, err := Open(dbPath)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer s.Close()

	// Large payload per row so a modest row count touches many WAL pages
	// without tripping SQLite's default ~1000-page auto-checkpoint
	// threshold (which would confound the "WAL is non-empty before our
	// manual checkpoint" assertion below).
	payload := strings.Repeat("x", 4096)
	for i := 0; i < 200; i++ {
		a := Attempt{
			RunID: "r1", RunName: "demo", TaskKey: fmt.Sprintf("task-%d", i),
			Engine: "mock", Model: "m", TaskType: "probe", Verdict: "PASS",
			Retry: 0, DurationS: 1.5, Tokens: 42, CheckOutput: payload,
			Identity: "test", CreatedAt: "2026-07-08T00:00:00Z",
		}
		if err := s.InsertAttempt(a); err != nil {
			t.Fatalf("InsertAttempt #%d: %v", i, err)
		}
	}

	walPath := dbPath + "-wal"
	before, err := os.Stat(walPath)
	if err != nil {
		t.Fatalf("stat wal before checkpoint: %v", err)
	}
	if before.Size() == 0 {
		t.Fatalf("expected non-empty WAL before checkpoint, got 0 bytes (test setup didn't grow the WAL)")
	}

	if err := s.Checkpoint(); err != nil {
		t.Fatalf("Checkpoint: %v", err)
	}

	after, err := os.Stat(walPath)
	if err != nil {
		t.Fatalf("stat wal after checkpoint: %v", err)
	}
	if after.Size() != 0 {
		t.Fatalf("Checkpoint() returned nil but WAL was not truncated: %d bytes (before: %d)", after.Size(), before.Size())
	}
}

// TestIsBusyDetectsCheckpointBusy is a fast, deterministic unit test of the
// isBusy() extension that lets a wal_checkpoint(TRUNCATE) busy=1 result row
// participate in withBusyRetry's bounded backoff (see errCheckpointBusy in
// store.go). A full SQLite-level integration test that forces busy=1 is not
// exercised here — see the report for why that's infeasible without
// flakiness/slowness given the hardcoded 5s busy_timeout.
func TestIsBusyDetectsCheckpointBusy(t *testing.T) {
	if !isBusy(errCheckpointBusy) {
		t.Errorf("isBusy(errCheckpointBusy) = false, want true")
	}
	wrapped := fmt.Errorf("wal_checkpoint(TRUNCATE) busy: could not obtain checkpoint lock (log=10 checkpointed=3): %w", errCheckpointBusy)
	if !isBusy(wrapped) {
		t.Errorf("isBusy(wrapped errCheckpointBusy) = false, want true")
	}
	if isBusy(errors.New("unrelated failure")) {
		t.Errorf("isBusy(unrelated error) = true, want false")
	}
	if isBusy(nil) {
		t.Errorf("isBusy(nil) = true, want false")
	}
}

func TestOpenIsIdempotent(t *testing.T) {
	p := filepath.Join(t.TempDir(), "ringer.db")
	for i := 0; i < 2; i++ {
		s, err := Open(p)
		if err != nil {
			t.Fatalf("Open #%d: %v", i+1, err)
		}
		s.Close()
	}
}
