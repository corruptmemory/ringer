package artifactwriter

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/corruptmemory/ringer/internal/artifact"
	"github.com/corruptmemory/ringer/internal/logging"
	"github.com/corruptmemory/ringer/internal/state"
)

func TestWriterLiveThenFinishProducesTree(t *testing.T) {
	sd := t.TempDir()
	lg, _ := logging.New(logging.Config{Level: 0})
	w := New(sd, DefaultConfig(sd), lg)

	live := state.RunState{RunID: "run-1", RunName: "demo", Identity: "id", Done: false,
		StartedAt: "2026-07-10T10:00:00Z", UpdatedAt: "2026-07-10T10:00:05Z",
		Tasks: []state.TaskView{{Key: "alpha", Status: "running", StartedAt: "2026-07-10T10:00:00Z"}}}
	w.Live(live)

	art := artifact.ArtifactsDir(sd)
	for _, rel := range []string{"run-1.html", "live/demo.html", "index.html"} {
		if _, err := os.Stat(filepath.Join(art, rel)); err != nil {
			t.Errorf("Live did not write %s: %v", rel, err)
		}
	}
	if e := artifact.ReadLibrary(sd).Artifacts["demo"]; e.State != "live" {
		t.Errorf("library entry not live: %+v", e)
	}

	done := live
	done.Done = true
	done.UpdatedAt = "2026-07-10T10:00:09Z"
	done.Tasks = []state.TaskView{{Key: "alpha", Status: "passed", StartedAt: "2026-07-10T10:00:00Z", EndedAt: "2026-07-10T10:00:09Z",
		Verified: "ok", CheckTail: "ok\n",
		Deliverables: []state.Deliverable{{TaskKey: "alpha", Name: "notes.md", Path: filepath.Join(art, "deliverables/run-1/alpha/notes.md"), Bytes: 5}}}}
	// stage the deliverable on disk so the wrapper can read it
	_ = os.MkdirAll(filepath.Dir(done.Tasks[0].Deliverables[0].Path), 0o755)
	_ = os.WriteFile(done.Tasks[0].Deliverables[0].Path, []byte("hello"), 0o644)
	w.Finish(done)

	for _, rel := range []string{"run-1-report.html", "versions/demo/run-1.html", "view/run-1/alpha--notes.md.html"} {
		if _, err := os.Stat(filepath.Join(art, rel)); err != nil {
			t.Errorf("Finish did not write %s: %v", rel, err)
		}
	}
	e := artifact.ReadLibrary(sd).Artifacts["demo"]
	if e.State != "pass" || len(e.Versions) != 1 || len(e.Versions[0].Deliverables) != 1 {
		t.Errorf("library version not recorded: %+v", e)
	}
}
