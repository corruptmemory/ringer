package runner

import (
	"strings"
	"sync"
	"testing"

	"github.com/corruptmemory/ringer/internal/logging"
)

func TestActorConcurrentUpdatesThenSnapshot(t *testing.T) {
	keys := []string{"a", "b", "c"}
	a := newActor("r1", "demo", "id", keys, logging.Default())
	a.start()
	defer a.stopAndWait()

	var wg sync.WaitGroup
	for _, k := range keys {
		wg.Add(1)
		go func(k string) {
			defer wg.Done()
			a.setStatus(k, "running", 1)
			a.setResult(k, "passed", 100, "did the thing", "/logs/"+k)
		}(k)
	}
	wg.Wait()

	snap := a.snapshot()
	if len(snap.Tasks) != 3 {
		t.Fatalf("snapshot has %d tasks, want 3", len(snap.Tasks))
	}
	for _, tv := range snap.Tasks {
		if tv.Status != "passed" || tv.Tokens != 100 {
			t.Errorf("task %s not settled: %+v", tv.Key, tv)
		}
	}
}

// TestActorDoubleStopLogsWarning proves stop()'s recovered double-stop is
// never silent: a second stop() must be a safe no-op AND must log a Warn
// line keyed by runID, mirroring the collector's identical lifecycle
// contract. Capture is synchronous, so no draining is needed before
// asserting on its buffer.
func TestActorDoubleStopLogsWarning(t *testing.T) {
	const runID = "r1"
	lg, capture := logging.NewCapture()
	a := newActor(runID, "demo", "id", []string{"a"}, lg)
	a.start()

	a.stopAndWait()

	// A second stop() must be a safe no-op (recover-guarded close), not a
	// panic — but it must also be LOGGED, never silently swallowed.
	a.stop()

	if logged := capture.String(); !strings.Contains(logged, runID) || !strings.Contains(logged, "double") {
		t.Fatalf("second stop() did not log a double-stop warning keyed by runID %q, got: %q", runID, logged)
	}
}
