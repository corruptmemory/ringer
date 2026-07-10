package artifact

import (
	"os"
	"testing"

	"github.com/corruptmemory/ringer/internal/state"
)

func TestReconcileFlipsDeadLiveEntries(t *testing.T) {
	dir := t.TempDir()
	if err := WriteLibrary(dir, Library{Artifacts: map[string]Entry{
		"alive": {State: "live", CurrentRunID: "alive-1"},
		"gone":  {State: "live", CurrentRunID: "gone-1"},
		"done":  {State: "pass", CurrentRunID: "done-1"},
	}}); err != nil {
		t.Fatal(err)
	}
	if err := state.RegisterActiveRun(dir, "alive-1", "j", "alive", "/wd", os.Getpid(), "2026-07-09T00:00:00Z"); err != nil {
		t.Fatal(err)
	}
	changed, err := ReconcileDeadRuns(dir, "2026-07-09T12:00:00Z")
	if err != nil || !changed {
		t.Fatalf("changed=%v err=%v, want changed=true", changed, err)
	}
	lib := ReadLibrary(dir)
	if lib.Artifacts["alive"].State != "live" || lib.Artifacts["gone"].State != "died" || lib.Artifacts["done"].State != "pass" {
		t.Fatalf("reconcile wrong: %+v", lib.Artifacts)
	}
	if lib.Artifacts["gone"].UpdatedAt != "2026-07-09T12:00:00Z" {
		t.Fatalf("died flip must stamp the passed-in time: %+v", lib.Artifacts["gone"])
	}
}
