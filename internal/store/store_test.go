// internal/store/store_test.go
package store

import (
	"path/filepath"
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
