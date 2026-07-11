package views

import (
	"fmt"

	"github.com/corruptmemory/ringer/internal/scoreboard"
)

// modelScoreboardTierColors mirrors statusColors' shape (artifact_render.go)
// but keys on scoreboard tier labels ("proven"/"probation") rather than run
// states.
var modelScoreboardTierColors = map[string]string{
	"proven":    "var(--pass)",
	"probation": "var(--waiting)",
}

// tierColor is a tier chip's background color, defaulting to --muted for any
// tier label outside modelScoreboardTierColors (there are only the two SQL
// emits today, but a chip must never go unstyled on an unrecognized value).
func tierColor(tier string) string {
	if c, ok := modelScoreboardTierColors[tier]; ok {
		return c
	}
	return "var(--muted)"
}

// modelName is a row's display name, falling back to its raw model slug when
// no identity was resolved (ModelDisplay == "" or unresolved == the slug
// itself).
func modelName(r scoreboard.Row) string {
	if r.ModelDisplay != "" {
		return r.ModelDisplay
	}
	return r.Model
}

// harnessOrUnknown blanks a resolved harness for display, matching
// renderModelsTable's (cmd/ringer/models.go) "unknown" fallback.
func harnessOrUnknown(h string) string {
	if h == "" {
		return "unknown"
	}
	return h
}

// pctString formats a 0..1 rate as a 2-decimal string ("%.2f"), matching
// renderModelsTable's percentage formatting.
func pctString(f float64) string { return fmt.Sprintf("%.2f", f) }

// scoreboardMsString renders a median duration (seconds) in whole
// milliseconds, blank when unknown. Named distinctly from cmd/ringer's
// msString — same shape, separate package, no cross-package reuse implied.
func scoreboardMsString(sec *float64) string {
	if sec == nil {
		return ""
	}
	return fmt.Sprintf("%d", int64(*sec*1000+0.5))
}

// scoreboardTokString renders a median token count, blank when unknown.
func scoreboardTokString(t *int64) string {
	if t == nil {
		return ""
	}
	return fmt.Sprintf("%d", *t)
}
