package artifact

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/corruptmemory/ringer/internal/state"
)

func TestUpdateLibraryLivePreservesVersions(t *testing.T) {
	sd := t.TempDir()
	// Seed an entry with one existing version.
	seed := Library{Artifacts: map[string]Entry{"run": {
		State: "pass", Identity: "id", CurrentRunID: "old",
		Versions: []Version{{RunID: "old", Path: "/p/old.html", Outcome: "pass"}},
	}}}
	if err := WriteLibrary(sd, seed); err != nil {
		t.Fatal(err)
	}
	if err := UpdateLibraryLive(sd, "run", "new", "id", "/a/live.html", "live", "2026-07-10T10:00:00Z"); err != nil {
		t.Fatal(err)
	}
	e := ReadLibrary(sd).Artifacts["run"]
	if e.State != "live" || e.CurrentRunID != "new" || e.LivePath != "/a/live.html" {
		t.Errorf("entry not updated: %+v", e)
	}
	if len(e.Versions) != 1 || e.Versions[0].RunID != "old" {
		t.Errorf("existing versions must be preserved: %+v", e.Versions)
	}
}

func TestAppendVersionPrependsDedupsFlipsState(t *testing.T) {
	sd := t.TempDir()
	_ = UpdateLibraryLive(sd, "run", "r2", "id", "/a/live.html", "live", "t0")
	rep := "/a/r2-report.html"
	rec := VersionRecord{
		RunName: "run", RunID: "r2", Identity: "id", LivePath: "/a/live.html",
		VersionPath: "/a/versions/run/r2.html", ReportPath: &rep,
		Outcome: "pass", TasksPass: 3, TasksFail: 0,
		Deliverables: []state.Deliverable{{TaskKey: "a", Name: "o.md", Path: "/d/o.md", Bytes: 4}},
	}
	if err := AppendLibraryVersion(sd, rec, "t1"); err != nil {
		t.Fatal(err)
	}
	e := ReadLibrary(sd).Artifacts["run"]
	if e.State != "pass" {
		t.Errorf("entry state must flip to outcome, got %q", e.State)
	}
	if len(e.Versions) != 1 || e.Versions[0].RunID != "r2" || e.Versions[0].TasksPass != 3 {
		t.Fatalf("version not recorded: %+v", e.Versions)
	}
	if len(e.Versions[0].Deliverables) != 1 {
		t.Errorf("deliverables not carried: %+v", e.Versions[0])
	}
	// Re-appending same run_id replaces (still one, still front).
	_ = AppendLibraryVersion(sd, rec, "t2")
	if e2 := ReadLibrary(sd).Artifacts["run"]; len(e2.Versions) != 1 {
		t.Errorf("same run_id must dedup, got %d versions", len(e2.Versions))
	}
}

func TestAppendVersionCapsAt20AndPrunesFiles(t *testing.T) {
	sd := t.TempDir()
	art := ArtifactsDir(sd)
	mk := func(rel string) string { // create a real file under artifacts/ so prune can delete it
		p := filepath.Join(art, rel)
		_ = os.MkdirAll(filepath.Dir(p), 0o755)
		_ = os.WriteFile(p, []byte("x"), 0o644)
		return p
	}
	for i := 0; i < 21; i++ {
		id := "r" + string(rune('a'+i))
		vpath := mk("versions/run/" + id + ".html")
		rpath := mk(id + "-report.html")
		rp := rpath
		_ = AppendLibraryVersion(sd, VersionRecord{
			RunName: "run", RunID: id, Identity: "id", LivePath: "/a/live.html",
			VersionPath: vpath, ReportPath: &rp, Outcome: "pass",
		}, "t")
	}
	e := ReadLibrary(sd).Artifacts["run"]
	if len(e.Versions) != 20 {
		t.Fatalf("want 20 kept, got %d", len(e.Versions))
	}
	// The oldest (r"a") version's files must be gone from disk.
	if _, err := os.Stat(filepath.Join(art, "versions/run/ra.html")); !os.IsNotExist(err) {
		t.Errorf("pruned version html should be deleted")
	}
	if _, err := os.Stat(filepath.Join(art, "ra-report.html")); !os.IsNotExist(err) {
		t.Errorf("pruned report html should be deleted")
	}
}

func TestOutcomeFromState(t *testing.T) {
	live := state.RunState{Done: false, Tasks: []state.TaskView{{Status: "running"}}}
	if OutcomeFromState(live) != "live" {
		t.Errorf("running run should be live")
	}
	pass := state.RunState{Done: true, Tasks: []state.TaskView{{Status: "passed"}}}
	if OutcomeFromState(pass) != "pass" {
		t.Errorf("all-pass should be pass")
	}
	fail := state.RunState{Done: true, Tasks: []state.TaskView{{Status: "passed"}, {Status: "failed"}}}
	if OutcomeFromState(fail) != "fail" {
		t.Errorf("any-fail should be fail")
	}
}
