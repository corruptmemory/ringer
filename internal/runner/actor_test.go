package runner

import (
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
