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
	// base "" = a page at the artifacts root; hrefs stay root-relative, so
	// these goldens are unchanged by the base-prefix mechanism.
	status := renderComponentString(t, StatusPage(rs, "/s", ""))
	assertGolden(t, "status_page.golden.html", status)
	// contract sanity independent of golden bytes:
	for _, must := range []string{"refresh", "class=\"page\"", "alpha", "work-group", "glyph pass", "glyph fail"} {
		if !strings.Contains(status, must) {
			t.Errorf("status page missing %q", must)
		}
	}
	final := renderComponentString(t, FinalReportPage(rs, "/s", ""))
	assertGolden(t, "final_report.golden.html", final)
	if strings.Contains(final, "http-equiv=\"refresh\"") {
		t.Error("final report must not self-refresh")
	}
	if !strings.Contains(final, "is-primary") {
		t.Error("final report work section must be primary")
	}
}

// TestPageBasePrefixesRelativeHrefs locks the live-page 404 fix: deliverable/
// wrapper hrefs are artifacts-root-relative, so a page served from a
// subdirectory (live/<run_name>.html, versions/<name>/<id>.html) must prepend
// its base prefix or every link resolves one+ dirs too deep and 404s.
func TestPageBasePrefixesRelativeHrefs(t *testing.T) {
	rs := fixedRun()
	root := renderComponentString(t, StatusPage(rs, "/s", ""))
	if !strings.Contains(root, `href="view/`) {
		t.Fatalf("root page should carry root-relative view/ hrefs")
	}
	deep := renderComponentString(t, StatusPage(rs, "/s", "../"))
	if !strings.Contains(deep, `href="../view/`) {
		t.Errorf("a one-dir-deep page must prefix hrefs with ../ (the live/ 404 fix)")
	}
	if strings.Contains(deep, `href="view/`) {
		t.Errorf("a deep page must not emit un-prefixed view/ hrefs — they 404 from live/")
	}
}

func TestReadTailTruncates(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "big.log")
	big := make([]byte, ArtifactWrapperTailBytes+100)
	for i := range big {
		big[i] = 'x'
	}
	copy(big[len(big)-3:], []byte("END"))
	_ = os.WriteFile(p, big, 0o644)
	content, size, truncated := ReadTail(p, ArtifactWrapperTailBytes)
	if !truncated || size != int64(len(big)) {
		t.Fatalf("truncated=%v size=%d", truncated, size)
	}
	if len(content) != ArtifactWrapperTailBytes || !strings.HasSuffix(content, "END") {
		t.Errorf("tail wrong: len=%d", len(content))
	}
}

func TestFileWrapperGolden(t *testing.T) {
	d := WrapperData{RunName: "demo", TaskKey: "alpha", Title: "Work log",
		MetaLine: "alpha produced this.", Content: "line one\nline two\n<script>alert(1)</script>\n"}
	page := renderComponentString(t, FileWrapperPage(d))
	assertGolden(t, "file_wrapper.golden.html", page)
	for _, must := range []string{"<pre>", "line one", "class=\"page\""} {
		if !strings.Contains(page, must) {
			t.Errorf("wrapper missing %q", must)
		}
	}
	// Content is arbitrary file bytes, never trusted HTML: templ's
	// auto-escaping must turn a literal "<script>" in the file into inert
	// text, not a live tag.
	if strings.Contains(page, "<script>alert(1)</script>") {
		t.Error("wrapper must escape file content, not render it as HTML")
	}
	if !strings.Contains(page, "&lt;script&gt;alert(1)&lt;/script&gt;") {
		t.Error("wrapper missing escaped file content")
	}
}

func TestIndexPageGolden(t *testing.T) {
	runs := []state.RunState{
		{RunID: "run-123", RunName: "demo", Identity: "id", Done: true, StartedAt: "2026-07-10T10:00:00Z", UpdatedAt: "2026-07-10T10:01:04Z",
			Tasks: []state.TaskView{{Status: "passed"}, {Status: "passed"}}},
		{RunID: "run-124", RunName: "live-run", Identity: "id", Done: false, StartedAt: "2026-07-10T10:02:00Z", UpdatedAt: "2026-07-10T10:02:20Z",
			Tasks: []state.TaskView{{Status: "running"}}},
	}
	rows := BuildIndexRows(runs, "/s")
	page := renderComponentString(t, IndexPage(rows))
	assertGolden(t, "index_page.golden.html", page)
	for _, must := range []string{"refresh", "demo", "live-run", "file://", "<table"} {
		if !strings.Contains(page, must) {
			t.Errorf("index page missing %q", must)
		}
	}
}
