package artifact

import (
	"encoding/json"
	"os"
	"testing"
)

func TestPathsCanonical(t *testing.T) {
	if ArtifactsDir("/s") != "/s/artifacts" || LibraryPath("/s") != "/s/artifacts/library.json" {
		t.Fatalf("paths wrong: %q %q", ArtifactsDir("/s"), LibraryPath("/s"))
	}
	if got := DeliverablesDir("/s", "run 1", "task/key"); got != "/s/artifacts/deliverables/run-1/task-key" {
		t.Fatalf("DeliverablesDir = %q (sanitization)", got)
	}
}

func TestReadWriteLibraryRoundTrip(t *testing.T) {
	dir := t.TempDir()
	report := "/x/rid-report.html"
	lib := Library{Artifacts: map[string]Entry{
		"demo": {LivePath: "/x/live/demo.html", State: "pass", Identity: "jim", CurrentRunID: "demo-1", UpdatedAt: "2026-07-09T00:00:00Z",
			Versions: []Version{{RunID: "demo-1", Path: "/x/v/demo-1.html", ReportPath: &report, FinishedAt: "2026-07-09T00:00:00Z", Outcome: "pass", TasksPass: 3,
				Deliverables: []Deliverable{{TaskKey: "a", Name: "out.txt", Path: "/x/out.txt", Bytes: 12}}}}},
	}}
	if err := WriteLibrary(dir, lib); err != nil {
		t.Fatal(err)
	}
	got := ReadLibrary(dir)
	if got.Artifacts["demo"].State != "pass" || len(got.Artifacts["demo"].Versions) != 1 || got.Artifacts["demo"].Versions[0].TasksPass != 3 {
		t.Fatalf("round-trip lost fields: %+v", got.Artifacts["demo"])
	}
	raw, _ := os.ReadFile(LibraryPath(dir))
	var probe map[string]any
	_ = json.Unmarshal(raw, &probe)
	if _, ok := probe["artifacts"].(map[string]any)["demo"].(map[string]any)["live_path"]; !ok {
		t.Fatalf("frozen key live_path missing: %s", raw)
	}
}

func TestReadLibraryMissingOrGarbageIsEmpty(t *testing.T) {
	if lib := ReadLibrary(t.TempDir()); lib.Artifacts == nil || len(lib.Artifacts) != 0 {
		t.Fatalf("missing → empty non-nil map, got %+v", lib)
	}
	dir := t.TempDir()
	_ = os.MkdirAll(ArtifactsDir(dir), 0o755)
	_ = os.WriteFile(LibraryPath(dir), []byte("{ nope"), 0o644)
	if lib := ReadLibrary(dir); len(lib.Artifacts) != 0 {
		t.Fatalf("garbage → empty, got %+v", lib)
	}
}
