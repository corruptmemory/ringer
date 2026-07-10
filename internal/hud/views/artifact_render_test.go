package views

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/corruptmemory/ringer/internal/state"
)

func TestBriefingLiveAndFinal(t *testing.T) {
	live := state.RunState{RunName: "run", StartedAt: "2026-07-10T10:00:00Z", UpdatedAt: "2026-07-10T10:00:20Z",
		Tasks: []state.TaskView{{Status: "running"}, {Status: "passed"}}}
	if b := BriefingLive(live); !strings.Contains(b, "2") { // "working on N tasks"
		t.Errorf("live briefing missing task count: %q", b)
	}
	final := state.RunState{RunName: "run", Done: true, StartedAt: "2026-07-10T10:00:00Z", UpdatedAt: "2026-07-10T10:01:04Z",
		Tasks: []state.TaskView{{Status: "passed"}, {Status: "passed"}, {Status: "passed"}}}
	if b := BriefingFinal(final); !strings.Contains(b, "3") {
		t.Errorf("final briefing missing task count: %q", b)
	}
}

func TestDeliverableKind(t *testing.T) {
	cases := map[string]string{
		"chart.png": "image", "photo.SVG": "image",
		"notes.md": "document", "log.txt": "document",
		"page.html": "web page", "p.HTM": "web page",
		"data.json": "download", "archive.zip": "download",
	}
	for name, want := range cases {
		if got := DeliverableKind(name); got != want {
			t.Errorf("DeliverableKind(%q) = %q, want %q", name, got, want)
		}
	}
}

func TestWrapperRelPathSanitized(t *testing.T) {
	got := WrapperRelPath("run 1", "task/a", "my report.md")
	want := "view/run-1/task-a--my-report.md.html"
	if got != want {
		t.Errorf("WrapperRelPath = %q, want %q", got, want)
	}
}

func TestDeliverableHrefTextWrapsImagesRaw(t *testing.T) {
	sd := "/x"
	txt := state.Deliverable{TaskKey: "a", Name: "notes.md", Path: "/x/artifacts/deliverables/r/a/notes.md"}
	if h := DeliverableHref(txt, "r", sd); h != "view/r/a--notes.md.html" { // relative to artifacts dir; run/task sanitized upstream
		t.Errorf("text deliverable should link to wrapper, got %q", h)
	}
	img := state.Deliverable{TaskKey: "a", Name: "c.png", Path: "/x/artifacts/deliverables/r/a/c.png"}
	if h := DeliverableHref(img, "r", sd); h != "deliverables/r/a/c.png" {
		t.Errorf("image should link raw, got %q", h)
	}
}

func TestDeliverableTitle(t *testing.T) {
	cases := map[string]string{
		"worker.log":  "Work log",
		"report.md":   "What this worker produced",
		"REPORT.HTML": "What this worker produced",
		"my_notes.md": "My notes",
		"chart-1.png": "Chart 1",
		"":            "Worker output",
	}
	for name, want := range cases {
		if got := DeliverableTitle(name); got != want {
			t.Errorf("DeliverableTitle(%q) = %q, want %q", name, got, want)
		}
	}
}

func TestTruncationBanner(t *testing.T) {
	got := TruncationBanner(1234567)
	want := " Showing the last <b>262,144</b> bytes of <b>1,234,567</b>."
	if got != want {
		t.Errorf("TruncationBanner(1234567) = %q, want %q", got, want)
	}
}

func TestImageDataURI(t *testing.T) {
	dir := t.TempDir()
	png := filepath.Join(dir, "chart.png")
	if err := os.WriteFile(png, []byte{0x89, 0x50, 0x4e, 0x47}, 0o644); err != nil {
		t.Fatal(err)
	}
	got := ImageDataURI(png)
	want := "data:image/png;base64," // base64("\x89PNG") == iVBORw==
	if !strings.HasPrefix(got, want) {
		t.Errorf("ImageDataURI = %q, want prefix %q", got, want)
	}

	if got := ImageDataURI(filepath.Join(dir, "missing.png")); got != "" {
		t.Errorf("missing file should return \"\", got %q", got)
	}

	big := filepath.Join(dir, "huge.png")
	f, err := os.Create(big)
	if err != nil {
		t.Fatal(err)
	}
	if err := f.Truncate(int64(21 * 1024 * 1024)); err != nil { // sparse file, > artifact.DeliverableMaxBytes (20 MiB)
		t.Fatal(err)
	}
	f.Close()
	if got := ImageDataURI(big); got != "" {
		t.Errorf("oversized file should return \"\", got %d bytes", len(got))
	}
}
