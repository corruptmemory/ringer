// internal/scoreboard/notes.go
package scoreboard

import (
	"os"
	"regexp"
	"sort"
	"strings"

	ringer "github.com/corruptmemory/ringer"
)

type NoteSection struct {
	Heading string
	Bullets []string
}

var (
	reWS       = regexp.MustCompile(`\s+`)
	reDate     = regexp.MustCompile(`\b\d{4}-\d{2}-\d{2}\b`)
	reHeading  = regexp.MustCompile(`^##\s+(.+?)\s*$`)
	reInlineBT = regexp.MustCompile("`([^`]*)`")
	reLink     = regexp.MustCompile(`\[([^\]]+)\]\([^)]+\)`)
	reEmph     = regexp.MustCompile(`[*_]{1,3}([^*_]+)[*_]{1,3}`)
	reNoteHead = regexp.MustCompile(`^-?\s*(\d{4}-\d{2}-\d{2})\s+(?:[-\x{2013}\x{2014}]+\s*)?(.*)$`)
)

func normalizeMatch(s string) string {
	return strings.ToLower(strings.TrimSpace(reWS.ReplaceAllString(s, " ")))
}

// ParseNotesSections ports ringer.py:5537-5586.
func ParseNotesSections(text string) []NoteSection {
	var sections []NoteSection
	var heading string
	haveHeading := false
	var bullets []string
	var active []string
	flushBullet := func() {
		if active != nil {
			t := strings.TrimSpace(strings.Join(active, "\n"))
			if reDate.MatchString(t) {
				bullets = append(bullets, t)
			}
			active = nil
		}
	}
	flushSection := func() {
		flushBullet()
		if haveHeading {
			sections = append(sections, NoteSection{Heading: heading, Bullets: append([]string(nil), bullets...)})
		}
	}
	for _, line := range strings.Split(text, "\n") {
		if m := reHeading.FindStringSubmatch(line); m != nil {
			flushSection()
			heading = strings.TrimSpace(m[1])
			haveHeading = true
			bullets = nil
			active = nil
			continue
		}
		if !haveHeading {
			continue
		}
		if strings.HasPrefix(line, "- ") {
			flushBullet()
			active = []string{strings.TrimSpace(line[2:])}
			continue
		}
		if active != nil && (strings.HasPrefix(line, "  ") || strings.TrimSpace(line) == "") {
			active = append(active, strings.TrimSpace(line))
			continue
		}
		flushBullet()
	}
	flushSection()
	return sections
}

const noteBoundary = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789._/:-"

func isBoundaryByte(b byte) bool { return strings.IndexByte(noteBoundary, b) >= 0 }

// matchesNeedle reports whether needle occurs in text delimited by non-boundary bytes.
func matchesNeedle(text, needle string) bool {
	if needle == "" {
		return false
	}
	for i := 0; ; {
		j := strings.Index(text[i:], needle)
		if j < 0 {
			return false
		}
		start := i + j
		end := start + len(needle)
		before := start == 0 || !isBoundaryByte(text[start-1])
		after := end == len(text) || !isBoundaryByte(text[end])
		if before && after {
			return true
		}
		i = start + 1
	}
}

// JudgmentNotes ports ringer.py:5589-5604.
func JudgmentNotes(model string, sections []NoteSection) []string {
	needle := normalizeMatch(model)
	if needle == "" {
		return nil
	}
	bestIdx := -1
	var bestExact, bestLen int
	for idx, sec := range sections {
		nh := normalizeMatch(sec.Heading)
		if !matchesNeedle(nh, needle) {
			continue
		}
		exact := 0
		if nh == needle {
			exact = 1
		}
		// best = higher exact, then longer heading, then earlier section (-index).
		if bestIdx == -1 || exact > bestExact || (exact == bestExact && len(nh) > bestLen) {
			bestIdx, bestExact, bestLen = idx, exact, len(nh)
		}
	}
	if bestIdx == -1 {
		return nil
	}
	// Defensive copy: callers (e.g. RenderNotes' newest-first sort, Task 10's
	// parse-once/reuse-across-polls) must not alias or mutate the parsed
	// sections' Bullets slice.
	return append([]string(nil), sections[bestIdx].Bullets...)
}

// LoadNotes returns the override notes file, or the embedded default.
//
// A non-empty overridePath that fails to read returns EMPTY sections (not the
// embedded default): a broken --notes-path must degrade visibly rather than
// silently masquerade as working. This matches LoadRegistry's empty-on-failure
// shape and Python parse_model_notes_sections (no embedded-fallback concept).
func LoadNotes(overridePath string) []NoteSection {
	data := ringer.ModelNotesMD
	if overridePath != "" {
		b, err := os.ReadFile(overridePath)
		if err != nil {
			return ParseNotesSections("")
		}
		data = b
	}
	return ParseNotesSections(string(data))
}

type RenderedNote struct{ Date, Body string }

func stripInlineMarkdown(s string) string {
	s = reInlineBT.ReplaceAllString(s, "$1")
	s = reLink.ReplaceAllString(s, "$1")
	s = reEmph.ReplaceAllString(s, "$1")
	return strings.TrimSpace(reWS.ReplaceAllString(s, " "))
}

// RenderNotes ports normalized_judgment_note + ordering (ringer.py:5614-5656),
// newest date first, capped at limit.
func RenderNotes(model string, sections []NoteSection, limit int) []RenderedNote {
	items := JudgmentNotes(model, sections)
	sort.SliceStable(items, func(i, j int) bool { return noteDateKey(items[i]) > noteDateKey(items[j]) })
	var out []RenderedNote
	for _, it := range items {
		if len(out) >= limit {
			break
		}
		m := reNoteHead.FindStringSubmatch(reWS.ReplaceAllString(it, " "))
		if m == nil {
			if body := stripInlineMarkdown(it); body != "" {
				out = append(out, RenderedNote{Body: body})
			}
			continue
		}
		body := stripInlineMarkdown(m[2])
		if body == "" {
			continue
		}
		out = append(out, RenderedNote{Date: humanizeShortDate(m[1]), Body: body})
	}
	return out
}

func noteDateKey(s string) string { return reDate.FindString(s) }

// humanizeShortDate renders 2026-07-06 -> "July 6" (ringer.py humanized_log_date, year stripped).
func humanizeShortDate(iso string) string {
	if len(iso) < 10 {
		return iso
	}
	// iso = YYYY-MM-DD; month names by index.
	months := []string{"January", "February", "March", "April", "May", "June", "July", "August", "September", "October", "November", "December"}
	y, mo, d := iso[:4], iso[5:7], iso[8:10]
	mi := int(mo[0]-'0')*10 + int(mo[1]-'0')
	if mi < 1 || mi > 12 {
		return iso
	}
	day := strings.TrimPrefix(d, "0")
	_ = y
	return months[mi-1] + " " + day
}
