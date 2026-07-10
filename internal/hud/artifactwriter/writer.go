// Package artifactwriter is the artifact WRITER: the orchestrator that turns
// a run-state snapshot into the on-disk artifact tree (pages + wrappers) and
// keeps library.json current. It composes the zero-LLM templ pages
// (internal/hud/views) with the persistence layer (internal/artifact) —
// Task 13 wires it into the runner via a nil-safe interface.
package artifactwriter

import (
	"context"
	"html"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/a-h/templ"
	"github.com/corruptmemory/ringer/internal/artifact"
	"github.com/corruptmemory/ringer/internal/hud/views"
	"github.com/corruptmemory/ringer/internal/logging"
	"github.com/corruptmemory/ringer/internal/state"
)

// libraryLiveThrottle caps how often Live re-writes library.json for an
// unchanged outcome (ringer.py's live-update throttle): once every 5s.
const libraryLiveThrottle = 5 * time.Second

// Config carries the resolved out/report_out/index_out templates + paths a
// Writer renders to. OutTemplate/ReportTemplate may contain "{run_id}" and
// "{run_name}" placeholders (see Writer.outPath/reportPath); IndexPath is a
// fixed path.
type Config struct {
	OutTemplate    string // <artifacts>/{run_id}.html
	ReportTemplate string // <artifacts>/{run_id}-report.html
	IndexPath      string // <artifacts>/index.html
}

// DefaultConfig returns the standard artifact-tree layout rooted at
// artifact.ArtifactsDir(stateDir).
func DefaultConfig(stateDir string) Config {
	art := artifact.ArtifactsDir(stateDir)
	return Config{
		OutTemplate:    filepath.Join(art, "{run_id}.html"),
		ReportTemplate: filepath.Join(art, "{run_id}-report.html"),
		IndexPath:      filepath.Join(art, "index.html"),
	}
}

// Writer renders run-state snapshots into the on-disk artifact tree and
// keeps library.json current. All I/O failures are Warn-logged and never
// fatal — a rendering hiccup must never take down the run it's reporting on.
type Writer struct {
	stateDir string
	cfg      Config
	lg       logging.Logger

	// lastOutcome/lastLibraryAt throttle Live's UpdateLibraryLive calls to
	// once per libraryLiveThrottle for an unchanged outcome.
	lastOutcome   string
	lastLibraryAt time.Time

	// versionRecorded guards AppendLibraryVersion so a run's version is
	// only ever appended once, even if Finish is somehow called twice.
	versionRecorded bool
}

// New builds a Writer that renders under stateDir per cfg, logging non-fatal
// failures through lg.
func New(stateDir string, cfg Config, lg logging.Logger) *Writer {
	return &Writer{stateDir: stateDir, cfg: cfg, lg: lg}
}

func nowISO() string { return time.Now().UTC().Format(time.RFC3339) }

func (w *Writer) outPath(rs state.RunState) string {
	return strings.NewReplacer("{run_id}", rs.RunID, "{run_name}", rs.RunName).Replace(w.cfg.OutTemplate)
}

func (w *Writer) reportPath(rs state.RunState) string {
	return strings.NewReplacer("{run_id}", rs.RunID, "{run_name}", rs.RunName).Replace(w.cfg.ReportTemplate)
}

func (w *Writer) livePath(rs state.RunState) string {
	return filepath.Join(artifact.ArtifactsDir(w.stateDir), "live", artifact.SanitizeName(rs.RunName)+".html")
}

func (w *Writer) versionPath(rs state.RunState) string {
	return filepath.Join(artifact.ArtifactsDir(w.stateDir), "versions", artifact.SanitizeName(rs.RunName), artifact.SanitizeName(rs.RunID)+".html")
}

// baseFor returns the relative prefix ("", "../", "../../", …) from a page's
// on-disk location back to the artifacts root. Deliverable/wrapper hrefs are
// artifacts-root-relative, so a page one dir deep (live/<run_name>.html) must
// prepend "../" and one two dirs deep (versions/<run_name>/<run_id>.html)
// "../../" to reach them — matching Python's page_path resolution, and correct
// for both the HUD's /artifacts/ HTTP serving and file:// opening. Falls back
// to "" for a page outside the artifacts root (a custom out-of-tree config).
func (w *Writer) baseFor(pagePath string) string {
	rel, err := filepath.Rel(artifact.ArtifactsDir(w.stateDir), pagePath)
	if err != nil {
		return ""
	}
	rel = filepath.ToSlash(rel)
	if strings.HasPrefix(rel, "../") {
		return ""
	}
	return strings.Repeat("../", strings.Count(rel, "/"))
}

// renderFile writes c to path, creating parent directories as needed. Any
// failure (mkdir/create/render) is Warn-logged and swallowed — rendering one
// page must never abort the rest of the tree.
func (w *Writer) renderFile(path string, c templ.Component) {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		w.lg.Warnf("artifact: mkdir %s: %v", path, err)
		return
	}
	f, err := os.Create(path)
	if err != nil {
		w.lg.Warnf("artifact: create %s: %v", path, err)
		return
	}
	defer f.Close()
	if err := c.Render(context.Background(), f); err != nil {
		w.lg.Warnf("artifact: render %s: %v", path, err)
	}
}

// Live renders the in-progress artifact pages for rs: the run's own page,
// its stable live/ alias, and the all-runs index, then throttles a
// library.json live-state update to once per 5s for an unchanged outcome.
// Never fatal — every failure is Warn-logged.
func (w *Writer) Live(rs state.RunState) {
	out, live := w.outPath(rs), w.livePath(rs)
	w.renderFile(out, views.StatusPage(rs, w.stateDir, w.baseFor(out)))
	w.renderFile(live, views.StatusPage(rs, w.stateDir, w.baseFor(live)))
	// Wrappers for already-passed tasks too, not just at Finish: the live page
	// shows deliverable/log links as each task completes, so their target
	// wrapper pages must exist mid-run or the links 404 until the whole run
	// finishes (matches Python, which writes wrappers during status render).
	w.writeWrappers(rs)
	w.writeIndex()
	w.updateLibraryLive(rs)
}

// Finish renders the frozen final-report pages for rs (run page, live/
// alias, dedicated -report.html, and the versions/ archive copy),
// regenerates every text-deliverable + worker-log wrapper page, appends the
// run's library version (once), and refreshes the all-runs index. Never
// fatal — every failure is Warn-logged.
func (w *Writer) Finish(rs state.RunState) {
	out, live, report, version := w.outPath(rs), w.livePath(rs), w.reportPath(rs), w.versionPath(rs)
	w.renderFile(out, views.FinalReportPage(rs, w.stateDir, w.baseFor(out)))
	w.renderFile(live, views.FinalReportPage(rs, w.stateDir, w.baseFor(live)))
	w.renderFile(report, views.FinalReportPage(rs, w.stateDir, w.baseFor(report)))
	w.renderFile(version, views.FinalReportPage(rs, w.stateDir, w.baseFor(version)))
	w.writeWrappers(rs)
	w.appendVersion(rs)
	w.writeIndex()
}

func (w *Writer) writeIndex() {
	runs, err := state.ReadAllRunStates(w.stateDir)
	if err != nil {
		w.lg.Warnf("artifact: scan runs for index: %v", err)
		return
	}
	w.renderFile(w.cfg.IndexPath, views.IndexPage(views.BuildIndexRows(runs, w.stateDir)))
}

// updateLibraryLive is throttled to once per libraryLiveThrottle for an
// unchanged outcome — Live is called far more often (every runner flush
// tick) than library.json needs to be rewritten.
func (w *Writer) updateLibraryLive(rs state.RunState) {
	outcome := artifact.OutcomeFromState(rs)
	if outcome == w.lastOutcome && time.Since(w.lastLibraryAt) < libraryLiveThrottle {
		return
	}
	w.lastOutcome, w.lastLibraryAt = outcome, time.Now()
	if err := artifact.UpdateLibraryLive(w.stateDir, rs.RunName, rs.RunID, rs.Identity, w.livePath(rs), outcome, nowISO()); err != nil {
		w.lg.Warnf("artifact: library live update: %v", err)
	}
}

// appendVersion records rs's final version into library.json exactly once
// (versionRecorded guards against a duplicate call), flattening every
// task's harvested deliverables and tallying pass/fail counts.
func (w *Writer) appendVersion(rs state.RunState) {
	if w.versionRecorded {
		return
	}
	w.versionRecorded = true
	var dels []state.Deliverable
	pass, fail := 0, 0
	for _, t := range rs.Tasks {
		dels = append(dels, t.Deliverables...)
		switch t.Status {
		case "passed":
			pass++
		case "failed", "timeout":
			fail++
		}
	}
	rp := w.reportPath(rs)
	vp := w.versionPath(rs)
	var reportPtr *string
	if rp != vp {
		reportPtr = &rp
	}
	rec := artifact.VersionRecord{
		RunName: rs.RunName, RunID: rs.RunID, Identity: rs.Identity, LivePath: w.livePath(rs),
		VersionPath: vp, ReportPath: reportPtr, Outcome: artifact.OutcomeFromState(rs),
		TasksPass: pass, TasksFail: fail, Deliverables: dels,
	}
	if err := artifact.AppendLibraryVersion(w.stateDir, rec, nowISO()); err != nil {
		w.lg.Warnf("artifact: library version append: %v", err)
	}
}

// writeWrappers generates a text wrapper page for each text deliverable and
// for each task's worker log (port of ringer.py's write_wrapper). Images and
// other binary deliverables are linked raw (views.DeliverableHref) and get
// no wrapper. Only files that exist on disk are wrapped: harvested
// deliverables already exist by construction, and a task's log is only
// wrapped when LogPath is set — views.ReadTail can't distinguish a missing
// file from an empty one, so skipping a phantom path here is what keeps that
// ambiguity harmless.
func (w *Writer) writeWrappers(rs state.RunState) {
	art := artifact.ArtifactsDir(w.stateDir)
	for _, t := range rs.Tasks {
		for _, d := range t.Deliverables {
			if !views.IsTextDeliverable(d.Name) {
				continue
			}
			content, size, trunc := views.ReadTail(d.Path, views.ArtifactWrapperTailBytes)
			meta := html.EscapeString(d.Name)
			if trunc {
				meta = views.TruncationBanner(size)
			}
			wp := filepath.Join(art, views.WrapperRelPath(rs.RunID, t.Key, d.Name))
			w.renderFile(wp, views.FileWrapperPage(views.WrapperData{
				RunName: rs.RunName, TaskKey: t.Key, Title: views.DeliverableTitle(d.Name), MetaLine: meta, Content: content, Base: w.baseFor(wp)}))
		}
		if t.LogPath != "" {
			content, size, trunc := views.ReadTail(t.LogPath, views.ArtifactWrapperTailBytes)
			meta := "worker log"
			if trunc {
				meta = views.TruncationBanner(size)
			}
			wp := filepath.Join(art, views.WrapperRelPath(rs.RunID, t.Key, "worker.log"))
			w.renderFile(wp, views.FileWrapperPage(views.WrapperData{
				RunName: rs.RunName, TaskKey: t.Key, Title: "Work log", MetaLine: meta, Content: content, Base: w.baseFor(wp)}))
		}
	}
}
