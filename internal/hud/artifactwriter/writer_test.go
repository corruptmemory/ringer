package artifactwriter

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/corruptmemory/ringer/internal/artifact"
	"github.com/corruptmemory/ringer/internal/hud/views"
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

// TestLiveWritesWrappersForPassedTasks locks that deliverable/log wrapper
// pages are written during Live, not only at Finish — otherwise a live page's
// deliverable link 404s until the whole run completes.
func TestLiveWritesWrappersForPassedTasks(t *testing.T) {
	sd := t.TempDir()
	lg, _ := logging.New(logging.Config{Level: 0})
	w := New(sd, DefaultConfig(sd), lg)

	art := artifact.ArtifactsDir(sd)
	delPath := filepath.Join(art, "deliverables/run-1/alpha/notes.md")
	if err := os.MkdirAll(filepath.Dir(delPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(delPath, []byte("hello"), 0o644); err != nil {
		t.Fatal(err)
	}

	// Still-live run (Done:false): alpha already passed + harvested; beta runs.
	rs := state.RunState{RunID: "run-1", RunName: "demo", Identity: "id", Done: false,
		StartedAt: "2026-07-10T10:00:00Z", UpdatedAt: "2026-07-10T10:00:06Z",
		Tasks: []state.TaskView{
			{Key: "alpha", Status: "passed", StartedAt: "2026-07-10T10:00:00Z", EndedAt: "2026-07-10T10:00:05Z",
				Deliverables: []state.Deliverable{{TaskKey: "alpha", Name: "notes.md", Path: delPath, Bytes: 5}}},
			{Key: "beta", Status: "running", StartedAt: "2026-07-10T10:00:00Z"},
		}}
	w.Live(rs)

	wp := filepath.Join(art, views.WrapperRelPath(rs.RunID, "alpha", "notes.md"))
	if _, err := os.Stat(wp); err != nil {
		t.Errorf("Live must write a passed task's deliverable wrapper so the live link resolves mid-run: %v", err)
	}
}

// TestFinishEscapesDeliverableNameInMetaLine locks writeWrappers' MetaLine
// construction (html.EscapeString(d.Name) in writer.go): a deliverable name
// carrying HTML metacharacters must reach the wrapper page escaped, never
// raw, so a future refactor can't silently reintroduce an HTML injection.
func TestFinishEscapesDeliverableNameInMetaLine(t *testing.T) {
	sd := t.TempDir()
	lg, _ := logging.New(logging.Config{Level: 0})
	w := New(sd, DefaultConfig(sd), lg)

	art := artifact.ArtifactsDir(sd)
	delPath := filepath.Join(art, "deliverables/run-1/alpha/a<b>.md")
	if err := os.MkdirAll(filepath.Dir(delPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(delPath, []byte("hello"), 0o644); err != nil {
		t.Fatal(err)
	}

	rs := state.RunState{RunID: "run-1", RunName: "demo", Identity: "id", Done: true,
		StartedAt: "2026-07-10T10:00:00Z", UpdatedAt: "2026-07-10T10:00:09Z",
		Tasks: []state.TaskView{{Key: "alpha", Status: "passed", StartedAt: "2026-07-10T10:00:00Z", EndedAt: "2026-07-10T10:00:09Z",
			Verified: "ok", CheckTail: "ok\n",
			Deliverables: []state.Deliverable{{TaskKey: "alpha", Name: "a<b>.md", Path: delPath, Bytes: 5}}}}}
	w.Finish(rs)

	wp := filepath.Join(art, views.WrapperRelPath(rs.RunID, "alpha", "a<b>.md"))
	data, err := os.ReadFile(wp)
	if err != nil {
		t.Fatalf("wrapper page not written: %v", err)
	}
	if !bytes.Contains(data, []byte("a&lt;b&gt;.md")) {
		t.Errorf("wrapper page missing escaped meta name, got:\n%s", data)
	}
	if bytes.Contains(data, []byte("a<b>.md")) {
		t.Errorf("wrapper page leaked raw unescaped deliverable name, got:\n%s", data)
	}
}

// TestFinishAppendsVersionOnlyOnce locks the versionRecorded once-guard:
// calling Finish twice for the same run must not double-append the run's
// library version.
func TestFinishAppendsVersionOnlyOnce(t *testing.T) {
	sd := t.TempDir()
	lg, _ := logging.New(logging.Config{Level: 0})
	w := New(sd, DefaultConfig(sd), lg)

	art := artifact.ArtifactsDir(sd)
	delPath := filepath.Join(art, "deliverables/run-1/alpha/notes.md")
	if err := os.MkdirAll(filepath.Dir(delPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(delPath, []byte("hello"), 0o644); err != nil {
		t.Fatal(err)
	}

	rs := state.RunState{RunID: "run-1", RunName: "demo", Identity: "id", Done: true,
		StartedAt: "2026-07-10T10:00:00Z", UpdatedAt: "2026-07-10T10:00:09Z",
		Tasks: []state.TaskView{{Key: "alpha", Status: "passed", StartedAt: "2026-07-10T10:00:00Z", EndedAt: "2026-07-10T10:00:09Z",
			Verified: "ok", CheckTail: "ok\n",
			Deliverables: []state.Deliverable{{TaskKey: "alpha", Name: "notes.md", Path: delPath, Bytes: 5}}}}}

	w.Finish(rs)
	w.Finish(rs)

	e := artifact.ReadLibrary(sd).Artifacts["demo"]
	if len(e.Versions) != 1 {
		t.Errorf("expected exactly one version after two Finish calls, got %d", len(e.Versions))
	}
}
