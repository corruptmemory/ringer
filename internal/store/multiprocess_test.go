// internal/store/multiprocess_test.go
package store

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// TestMultiProcessWrites spawns writer child processes against ONE database
// file — the exact topology of concurrent `ringer run` invocations plus the
// HUD reader. Deliberately over-stressed vs the real envelope (~10 writes/min):
// 5 procs x 200 rows as fast as they can go.
func TestMultiProcessWrites(t *testing.T) {
	if testing.Short() {
		t.Skip("multi-process smoke skipped in -short")
	}
	const nProcs, nRows = 5, 200
	dbPath := filepath.Join(t.TempDir(), "smoke.db")

	// Create schema up front so children race only on writes.
	s, err := Open(dbPath)
	if err != nil {
		t.Fatalf("parent Open: %v", err)
	}
	s.Close()

	procs := make([]*exec.Cmd, nProcs)
	for i := range procs {
		cmd := exec.Command(os.Args[0], "-test.run", "^TestSmokeChildProcess$", "-test.v")
		cmd.Env = append(os.Environ(),
			"STORE_SMOKE_DB="+dbPath,
			fmt.Sprintf("STORE_SMOKE_PROC=%d", i),
			fmt.Sprintf("STORE_SMOKE_ROWS=%d", nRows),
		)
		out, err := os.CreateTemp(t.TempDir(), "child-*.log")
		if err != nil {
			t.Fatal(err)
		}
		cmd.Stdout, cmd.Stderr = out, out
		if err := cmd.Start(); err != nil {
			t.Fatalf("start child %d: %v", i, err)
		}
		procs[i] = cmd
	}
	for i, cmd := range procs {
		if err := cmd.Wait(); err != nil {
			t.Errorf("child %d failed: %v", i, err)
		}
	}

	s, err = Open(dbPath)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	defer s.Close()
	n, err := s.CountAttempts()
	if err != nil {
		t.Fatalf("CountAttempts: %v", err)
	}
	if n != int64(nProcs*nRows) {
		t.Errorf("rows = %d, want %d (lost writes!)", n, nProcs*nRows)
	}
	if err := s.Integrity(); err != nil {
		t.Errorf("integrity after concurrent writes: %v", err)
	}
}

// TestSmokeChildProcess is the child body; it is a no-op unless the smoke
// env vars are present, so a plain `go test ./...` never runs it standalone.
func TestSmokeChildProcess(t *testing.T) {
	dbPath := os.Getenv("STORE_SMOKE_DB")
	if dbPath == "" {
		t.Skip("not a smoke child")
	}
	var proc, rows int
	fmt.Sscanf(os.Getenv("STORE_SMOKE_PROC"), "%d", &proc)
	fmt.Sscanf(os.Getenv("STORE_SMOKE_ROWS"), "%d", &rows)

	s, err := Open(dbPath)
	if err != nil {
		t.Fatalf("child open: %v", err)
	}
	defer s.Close()
	for i := 0; i < rows; i++ {
		a := Attempt{
			RunID:   fmt.Sprintf("run-%d", proc),
			TaskKey: fmt.Sprintf("task-%d-%d", proc, i),
			Verdict: "PASS", CreatedAt: "2026-07-08T00:00:00Z",
		}
		if err := s.InsertAttempt(a); err != nil {
			t.Fatalf("child %d insert %d: %v", proc, i, err)
		}
	}
}
