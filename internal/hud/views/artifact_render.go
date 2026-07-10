package views

import (
	"encoding/base64"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/corruptmemory/ringer/internal/artifact"
	"github.com/corruptmemory/ringer/internal/state"
)

// BriefingLive is the status page's plain-language heading (port of
// live_briefing_html, ringer.py:3013-3034): e.g. "Ringer is working on 2
// tasks — 1 finished and checked and 1 working, started 20 seconds ago."
func BriefingLive(rs state.RunState) string {
	total := len(rs.Tasks)
	ago := formatAgo(RunElapsed(rs))
	if total == 0 {
		return fmt.Sprintf("Ringer has no tasks. Started %s ago.", ago)
	}
	pass, fail := PassCount(rs), FailCount(rs)
	working, retry, waiting := bucketCounts(rs)
	var parts []string
	if pass > 0 {
		parts = append(parts, passedPhrase(pass))
	}
	if working > 0 {
		parts = append(parts, runningPhrase(working))
	}
	if retry > 0 {
		parts = append(parts, retryPhrase(retry))
	}
	if waiting > 0 {
		parts = append(parts, waitingPhrase(waiting))
	}
	if fail > 0 {
		parts = append(parts, failedPhrase(fail))
	}
	statusSentence := joinPlainParts(parts)
	return fmt.Sprintf("Ringer is working on %d %s — %s, started %s ago.", total, taskWord(total), statusSentence, ago)
}

// BriefingFinal is the final report heading (port of final_briefing_html,
// ringer.py:3041-3053): e.g. "Ringer finished 3 tasks in 1m 04s. All 3
// finished and checked." / "...2 finished and checked, 1 failed after
// retry."
func BriefingFinal(rs state.RunState) string {
	total := len(rs.Tasks)
	pass, fail := PassCount(rs), FailCount(rs)
	elapsed := FormatDuration(RunElapsed(rs))
	first := fmt.Sprintf("Ringer finished %d %s in %s.", total, taskWord(total), elapsed)
	if fail == 0 {
		return fmt.Sprintf("%s All %d finished and checked.", first, total)
	}
	return fmt.Sprintf("%s %d finished and checked, %d failed after retry.", first, pass, fail)
}

// progressLegend is the progress bar's summary line (port of
// render_progress_bar's legend, ringer.py:3169-3180): e.g. "1 finished · 1
// working" or "No tasks".
func progressLegend(rs state.RunState) string {
	pass, fail := PassCount(rs), FailCount(rs)
	working, retry, waiting := bucketCounts(rs)
	var parts []string
	if pass > 0 {
		parts = append(parts, fmt.Sprintf("%d finished", pass))
	}
	if working > 0 {
		parts = append(parts, fmt.Sprintf("%d working", working))
	}
	if retry > 0 {
		parts = append(parts, fmt.Sprintf("%d sent back", retry))
	}
	if fail > 0 {
		parts = append(parts, fmt.Sprintf("%d failed", fail))
	}
	if waiting > 0 {
		parts = append(parts, fmt.Sprintf("%d waiting", waiting))
	}
	if len(parts) == 0 {
		return "No tasks"
	}
	return strings.Join(parts, " · ")
}

// bucketCounts tallies the "working"/"retry"/"waiting" TaskKind buckets not
// already covered by the existing PassCount/FailCount helpers (port of the
// non-pass/fail slices of task_status_counts, ringer.py:2957-2972).
func bucketCounts(rs state.RunState) (working, retry, waiting int) {
	for _, t := range rs.Tasks {
		switch TaskKind(t) {
		case "working":
			working++
		case "retry":
			retry++
		case "waiting":
			waiting++
		}
	}
	return working, retry, waiting
}

func taskWord(n int) string {
	if n == 1 {
		return "task"
	}
	return "tasks"
}

// passedPhrase/failedPhrase/runningPhrase/retryPhrase/waitingPhrase port the
// eponymous Python phrase helpers (ringer.py:2979-3006).
func passedPhrase(n int) string  { return fmt.Sprintf("%d finished and checked", n) }
func failedPhrase(n int) string  { return fmt.Sprintf("%d failed", n) }
func runningPhrase(n int) string { return fmt.Sprintf("%d working", n) }
func retryPhrase(n int) string   { return fmt.Sprintf("%d sent back", n) }
func waitingPhrase(n int) string {
	if n == 1 {
		return "1 is waiting"
	}
	return fmt.Sprintf("%d are waiting", n)
}

// joinPlainParts ports join_plain_html_parts (ringer.py:3056-3063): "a", "a
// and b", or "a, b, and c".
func joinPlainParts(parts []string) string {
	switch len(parts) {
	case 0:
		return ""
	case 1:
		return parts[0]
	case 2:
		return parts[0] + " and " + parts[1]
	default:
		return strings.Join(parts[:len(parts)-1], ", ") + ", and " + parts[len(parts)-1]
	}
}

// formatAgo renders seconds as a word-based "ago" duration (port of
// fmt_plain_ago, ringer.py:2174-2195): "9 seconds", "1 minute 5 seconds", "2
// hours". Distinct from FormatDuration's compact "9s"/"1m 03s" shape, which
// live_briefing_html does not use for its "started ... ago" clause.
func formatAgo(sec float64) string {
	total := int(sec)
	if total < 0 {
		total = 0
	}
	if total < 60 {
		return pluralUnit(total, "second")
	}
	minutes, secondsLeft := total/60, total%60
	if minutes < 60 {
		if secondsLeft == 0 {
			return pluralUnit(minutes, "minute")
		}
		return pluralUnit(minutes, "minute") + " " + pluralUnit(secondsLeft, "second")
	}
	hours, minutesLeft := minutes/60, minutes%60
	if minutesLeft == 0 {
		return pluralUnit(hours, "hour")
	}
	return pluralUnit(hours, "hour") + " " + pluralUnit(minutesLeft, "minute")
}

func pluralUnit(n int, unit string) string {
	if n == 1 {
		return fmt.Sprintf("%d %s", n, unit)
	}
	return fmt.Sprintf("%d %ss", n, unit)
}

// --- Deliverable classification, href, and text helpers (Task 8) ---
//
// Ports ringer.py's work-item classification (work_label_and_kind
// ringer.py:3394-3407, is_text/image_deliverable 3410-3415, image_data_uri
// 3418-3427) and the text-wrapper page title (deliverable_title 2738-2745).

var textDeliverableSuffixes = map[string]bool{".md": true, ".txt": true, ".log": true}
var imageDeliverableSuffixes = map[string]bool{".avif": true, ".gif": true, ".jpeg": true, ".jpg": true, ".png": true, ".svg": true, ".webp": true}

// imageMimeExtensions maps an image deliverable's suffix to its data-URI
// MIME type (port of image_data_uri's mimetypes.guess_type step,
// ringer.py:3423). Fixed to imageDeliverableSuffixes rather than delegating
// to Go's mime.TypeByExtension, which on some platforms consults system
// mime.types files — a fixed table keeps ImageDataURI's output identical on
// every machine (and every golden-file test run).
var imageMimeExtensions = map[string]string{
	".avif": "image/avif",
	".gif":  "image/gif",
	".jpeg": "image/jpeg",
	".jpg":  "image/jpeg",
	".png":  "image/png",
	".svg":  "image/svg+xml",
	".webp": "image/webp",
}

func IsTextDeliverable(name string) bool {
	return textDeliverableSuffixes[strings.ToLower(filepath.Ext(name))]
}
func IsImageDeliverable(name string) bool {
	return imageDeliverableSuffixes[strings.ToLower(filepath.Ext(name))]
}

// DeliverableKind labels a deliverable for the results page (port of
// work_label_and_kind's kind half, ringer.py:3394-3407).
func DeliverableKind(name string) string {
	switch ext := strings.ToLower(filepath.Ext(name)); {
	case ext == ".html" || ext == ".htm":
		return "web page"
	case imageDeliverableSuffixes[ext]:
		return "image"
	case textDeliverableSuffixes[ext]:
		return "document"
	default:
		return "download"
	}
}

// deliverableLabel builds a work-item's link text: a prettified filename
// stem plus its kind (port of work_label_and_kind's label half,
// ringer.py:3394-3407) — e.g. "Chart — image", or "Work — download" for an
// extension-only name.
func deliverableLabel(name string) string {
	return prettyStem(name) + " — " + DeliverableKind(name)
}

// stemOf returns name's filename stem — the final path element minus its
// last extension — mirroring Python's Path(name).stem, including its ""
// -> "" edge case that filepath.Base's Go-specific "." fallback would
// otherwise mask for an empty name.
func stemOf(name string) string {
	if name == "" {
		return ""
	}
	base := filepath.Base(name)
	return strings.TrimSuffix(base, filepath.Ext(base))
}

// prettyStem replaces "_"/"-" with spaces and upper-cases only the first
// rune (the rest is untouched), defaulting to "Work" for an empty stem —
// mirrors work_label_and_kind's `stem[:1].upper() + stem[1:]`
// (ringer.py:3396-3397).
func prettyStem(name string) string {
	stem := strings.TrimSpace(strings.NewReplacer("_", " ", "-", " ").Replace(stemOf(name)))
	if stem == "" {
		return "Work"
	}
	r := []rune(stem)
	return strings.ToUpper(string(r[0])) + string(r[1:])
}

// deliverableReportNames mirrors TASK_REPORT_FILENAMES (ringer.py:67):
// report.md takes priority over report.html when a task produced both.
var deliverableReportNames = []string{"report.md", "report.html"}

// DeliverableTitle labels a deliverable for its text-wrapper page heading
// (port of deliverable_title, ringer.py:2738-2745): worker.log -> "Work
// log"; a task report -> "What this worker produced"; else a
// Python-str.capitalize()-style prettified stem ("my_notes.md" -> "My
// notes"), or "Worker output" for an empty stem.
func DeliverableTitle(name string) string {
	lower := strings.ToLower(name)
	if lower == "worker.log" {
		return "Work log"
	}
	for _, report := range deliverableReportNames {
		if lower == report {
			return "What this worker produced"
		}
	}
	stem := strings.TrimSpace(strings.NewReplacer("_", " ", "-", " ").Replace(stemOf(name)))
	if stem == "" {
		return "Worker output"
	}
	return pyCapitalize(stem)
}

// pyCapitalize mirrors Python's str.capitalize(): upper-case the first
// rune, lower-case the rest.
func pyCapitalize(s string) string {
	r := []rune(s)
	return strings.ToUpper(string(r[0])) + strings.ToLower(string(r[1:]))
}

// WrapperRelPath is a text deliverable's wrapper-page path relative to the
// artifacts dir: view/<sanitize(runID)>/<sanitize(taskKey)>--<sanitize(sourceName)>.html.
func WrapperRelPath(runID, taskKey, sourceName string) string {
	return filepath.ToSlash(filepath.Join("view", artifact.SanitizeName(runID),
		artifact.SanitizeName(taskKey)+"--"+artifact.SanitizeName(sourceName)+".html"))
}

// DeliverableHref returns the artifacts-dir-relative link for a deliverable:
// a text deliverable links to its wrapper page; anything else links to the
// raw copied file. Both are relative to the artifacts dir so the link
// resolves the same over HTTP (`/artifacts/…`) and opened straight off disk
// (`file://…`).
func DeliverableHref(d state.Deliverable, runID, stateDir string) string {
	if IsTextDeliverable(d.Name) {
		return WrapperRelPath(runID, d.TaskKey, d.Name)
	}
	rel, err := filepath.Rel(artifact.ArtifactsDir(stateDir), d.Path)
	if err != nil {
		return d.Path
	}
	return filepath.ToSlash(rel)
}

// ImageDataURI reads a deliverable image and returns a data: URI for inline
// thumbnailing, or "" on a read error or an oversized file (port of
// image_data_uri, ringer.py:3418-3427). Python's image_data_uri itself has
// no size guard; deliverables are already capped at
// artifact.DeliverableMaxBytes (20 MiB) when harvested
// (internal/artifact/deliverables.go skips anything bigger), and
// ImageDataURI re-checks that same cap directly so a huge file read
// straight off disk is never base64-inlined into the page.
func ImageDataURI(path string) string {
	info, err := os.Stat(path)
	if err != nil || info.Size() > artifact.DeliverableMaxBytes {
		return ""
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	mimeType, ok := imageMimeExtensions[strings.ToLower(filepath.Ext(path))]
	if !ok {
		mimeType = "application/octet-stream"
	}
	return fmt.Sprintf("data:%s;base64,%s", mimeType, base64.StdEncoding.EncodeToString(data))
}

// emptyWorkNote is the per-bucket "nothing here yet" copy for a task with no
// deliverables (port of render_work_group's rows-empty branch,
// ringer.py:3259-3268).
func emptyWorkNote(kind string) string {
	switch kind {
	case "pass":
		return "Finished and checked — this worker filed nothing to the shelf."
	case "fail":
		return "Failed its check — nothing was delivered."
	case "working", "retry":
		return "Nothing delivered yet — still on it."
	default:
		return "Waiting its turn."
	}
}

// showVerified/verifiedLabel and showProof/proofLabel gate and word the
// verification-proof drawer, restricted to pass/fail buckets (port of
// render_work_group's verified_html construction, ringer.py:3274-3286). The
// gate matters for a "retry" task: its Verified/CheckTail still holds the
// FAILED first attempt (opSetResult isn't cleared by the retry's
// opSetStatus — see internal/runner/actor.go), so without the gate a
// mid-retry task would show attempt 1's stale verdict while attempt 2 runs.

func showVerified(t state.TaskView) bool {
	kind := TaskKind(t)
	return (kind == "pass" || kind == "fail") && strings.TrimSpace(t.Verified) != ""
}

func verifiedLabel(t state.TaskView) string {
	how := "How it was checked"
	if TaskKind(t) == "fail" {
		how = "What the check demanded"
	}
	return how + ": " + strings.TrimSpace(t.Verified)
}

func showProof(t state.TaskView) bool {
	kind := TaskKind(t)
	return (kind == "pass" || kind == "fail") && strings.TrimSpace(t.CheckTail) != ""
}

func proofLabel(t state.TaskView) string {
	if TaskKind(t) == "fail" {
		return "See why it failed"
	}
	return "See the proof"
}

func proofText(t state.TaskView) string { return strings.TrimSpace(t.CheckTail) }

// taskLink is one entry in a task's link row.
type taskLink struct {
	text, href string
}

// taskLinkItems computes a task's link row (port of render_task_links,
// ringer.py:3532-3591): "Read what it found" when a report.md/report.html
// deliverable exists (report.md wins if a task somehow produced both), then
// "view the work log" when the task has a log.
func taskLinkItems(runID string, t state.TaskView, stateDir string) []taskLink {
	var links []taskLink
	if d, ok := findReportDeliverable(t.Deliverables); ok {
		links = append(links, taskLink{text: "Read what it found", href: DeliverableHref(d, runID, stateDir)})
	}
	if t.LogPath != "" {
		links = append(links, taskLink{text: "view the work log", href: WrapperRelPath(runID, t.Key, "worker.log")})
	}
	return links
}

// findReportDeliverable returns the first deliverable named report.md or
// report.html (in that priority order), mirroring TASK_REPORT_FILENAMES'
// iteration order in render_task_links (ringer.py:3563-3575).
func findReportDeliverable(ds []state.Deliverable) (state.Deliverable, bool) {
	for _, name := range deliverableReportNames {
		for _, d := range ds {
			if d.Name == name {
				return d, true
			}
		}
	}
	return state.Deliverable{}, false
}
