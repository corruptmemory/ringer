package scoreboard

import (
	"reflect"
	"testing"
)

const notesMD = `# notes

## codex (GPT-5-class)
- 2026-07-05 — carried the heavy lanes, clean first-attempt passes.
- no date here, should be dropped
- 2026-07-06 — passed on attempt 1, ~85k tokens.

## glm-5.2 via opencode (` + "`openrouter/z-ai/glm-5.2`" + `)
- 2026-07-07 — solid on refactors.
`

func TestParseAndMatchNotes(t *testing.T) {
	secs := ParseNotesSections(notesMD)
	if len(secs) != 2 {
		t.Fatalf("want 2 sections, got %d", len(secs))
	}
	// codex heading has 2 dated bullets (the undated one is dropped).
	codex := JudgmentNotes("codex", secs)
	if len(codex) != 2 {
		t.Fatalf("codex notes: want 2 dated bullets, got %d: %v", len(codex), codex)
	}
	// fuzzy match on the slug inside the glm heading.
	glm := JudgmentNotes("openrouter/z-ai/glm-5.2", secs)
	if len(glm) != 1 {
		t.Fatalf("glm notes: want 1, got %d", len(glm))
	}
	// unknown model -> no notes.
	if got := JudgmentNotes("nonesuch", secs); len(got) != 0 {
		t.Fatalf("unknown model returned notes: %v", got)
	}
	// render: date humanized + markdown/leading-dash stripped.
	rn := RenderNotes("codex", secs, 5)
	if len(rn) != 2 || rn[0].Date == "" || rn[0].Body == "" {
		t.Fatalf("RenderNotes wrong: %+v", rn)
	}
}

// TestLoadNotesMissingOverrideIsEmpty pins that a read failure on a non-empty
// override path returns empty sections (visible degradation), NOT the embedded
// default. Mirrors Task 2's LoadRegistry empty-on-failure shape + Python
// parse_model_notes_sections which has no embedded-fallback concept.
func TestLoadNotesMissingOverrideIsEmpty(t *testing.T) {
	secs := LoadNotes("/no/such/notes.md")
	if len(secs) != 0 {
		t.Fatalf("bad override path leaked notes: want 0 sections, got %d: %+v", len(secs), secs)
	}
	if got := JudgmentNotes("codex", secs); len(got) != 0 {
		t.Fatalf("bad override path leaked judgment notes for codex: %v", got)
	}
}

// TestRenderNotesDoesNotMutateSections proves RenderNotes (via JudgmentNotes)
// treats the parsed sections as read-only: the newest-first sort must operate
// on a defensive copy, not alias sections[bestIdx].Bullets in place. Without
// the copy, Task 10's parse-once/reuse-across-polls would reorder shared data.
func TestRenderNotesDoesNotMutateSections(t *testing.T) {
	secs := ParseNotesSections(notesMD)
	// Capture the pre-render bullet order of the codex section.
	var before [][]string
	for _, s := range secs {
		before = append(before, append([]string(nil), s.Bullets...))
	}
	_ = RenderNotes("codex", secs, 5)
	for i, s := range secs {
		if !reflect.DeepEqual(s.Bullets, before[i]) {
			t.Fatalf("RenderNotes mutated sections[%d].Bullets: before %v, after %v", i, before[i], s.Bullets)
		}
	}
}
