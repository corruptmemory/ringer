package views

import (
	"context"
	"flag"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/a-h/templ"
	"github.com/corruptmemory/ringer/internal/state"
)

var update = flag.Bool("update", false, "update golden files")

// renderComponentString renders a templ.Component into a string via a
// strings.Builder, failing the test on a render error.
func renderComponentString(t *testing.T, c templ.Component) string {
	t.Helper()
	var sb strings.Builder
	if err := c.Render(context.Background(), &sb); err != nil {
		t.Fatalf("render: %v", err)
	}
	return sb.String()
}

func fixedRun() state.RunState {
	return state.RunState{
		RunID: "run-123", RunName: "demo", Identity: "godlike-artix",
		StartedAt: "2026-07-10T10:00:00Z", UpdatedAt: "2026-07-10T10:01:04Z", Done: true,
		Tasks: []state.TaskView{
			{Key: "alpha", Status: "passed", Engine: "mock", StartedAt: "2026-07-10T10:00:00Z", EndedAt: "2026-07-10T10:00:05Z",
				Verified: "alpha.txt exists", CheckTail: "ok\n",
				Deliverables: []state.Deliverable{{TaskKey: "alpha", Name: "alpha.txt", Path: "/s/artifacts/deliverables/run-123/alpha/alpha.txt", Bytes: 11}}},
			{Key: "bravo", Status: "failed", Engine: "mock", StartedAt: "2026-07-10T10:00:00Z", EndedAt: "2026-07-10T10:00:07Z"},
		},
	}
}

func assertGolden(t *testing.T, name, got string) {
	t.Helper()
	path := filepath.Join("testdata", name)
	if *update {
		_ = os.MkdirAll("testdata", 0o755)
		if err := os.WriteFile(path, []byte(got), 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read golden %s (run with -update first): %v", name, err)
	}
	if got != string(want) {
		t.Errorf("%s mismatch; run `./build.sh --test` after `-update` to refresh", name)
	}
}

func TestStatusAndFinalGoldens(t *testing.T) {
	rs := fixedRun()
	status := renderComponentString(t, StatusPage(rs, "/s"))
	assertGolden(t, "status_page.golden.html", status)
	// contract sanity independent of golden bytes:
	for _, must := range []string{"refresh", "class=\"page\"", "alpha", "work-group", "glyph pass", "glyph fail"} {
		if !strings.Contains(status, must) {
			t.Errorf("status page missing %q", must)
		}
	}
	final := renderComponentString(t, FinalReportPage(rs, "/s"))
	assertGolden(t, "final_report.golden.html", final)
	if strings.Contains(final, "http-equiv=\"refresh\"") {
		t.Error("final report must not self-refresh")
	}
	if !strings.Contains(final, "is-primary") {
		t.Error("final report work section must be primary")
	}
}
