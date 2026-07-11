package scoreboard

import (
	"path/filepath"
	"testing"

	"github.com/corruptmemory/ringer/internal/catalog"
	"github.com/corruptmemory/ringer/internal/store"
)

func TestScoreboardResolvesIdentityAndNests(t *testing.T) {
	s, err := store.Open(filepath.Join(t.TempDir(), "sb.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	for _, a := range []store.Attempt{
		{RunID: "r1", TaskKey: "t1", Engine: "codex", Model: "gpt-5.5", TaskType: "code", Verdict: "PASS", Tokens: 100, CreatedAt: "2026-07-10T00:00:01Z"},
		{RunID: "r1", TaskKey: "t2", Engine: "codex", Model: "gpt-5.5", TaskType: "docs", Verdict: "PASS", Tokens: 100, CreatedAt: "2026-07-10T00:00:02Z"},
	} {
		if err := s.InsertAttempt(a); err != nil {
			t.Fatal(err)
		}
	}
	reg := LoadRegistry("") // embedded registry maps codex/gpt-5.5 -> "GPT-5.5"
	rows, err := Scoreboard(s, Filter{}, reg)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 {
		t.Fatalf("want 1 model row, got %d", len(rows))
	}
	r := rows[0]
	if r.ModelDisplay != "GPT-5.5" || r.Harness != "Codex CLI" {
		t.Fatalf("identity not resolved: display=%q harness=%q", r.ModelDisplay, r.Harness)
	}
	if len(r.TaskTypes) != 2 {
		t.Fatalf("want 2 task-type breakdowns, got %d", len(r.TaskTypes))
	}
	if r.MedianTokens == nil || *r.MedianTokens != 100 {
		t.Fatalf("median tokens wrong: %v", r.MedianTokens)
	}
}

func TestExploreCandidates(t *testing.T) {
	models := []catalog.Model{
		{ID: "tested/model", ContextLength: 64000, Modality: "text->text"},             // filtered: already tested
		{ID: "small/model", ContextLength: 8000, Modality: "text->text"},               // filtered: context too small
		{ID: "vision/model", ContextLength: 128000, Modality: "text+image->text"},      // filtered: not text->text
		{ID: "empty-modality/model", ContextLength: 32000, Modality: ""},               // kept: blank modality passes
		{ID: "plain-text/model", ContextLength: 40000, Modality: "text"},               // kept: bare "text" passes
		{ID: "cheap/model", ContextLength: 100000, Modality: "text->text", Free: true}, // kept
		{ID: "at-threshold/model", ContextLength: 32000, Modality: "text->text"},       // kept: exactly 32000
	}
	tested := map[string]bool{"tested/model": true}

	got := ExploreCandidates(models, tested)

	wantIDs := map[string]bool{
		"empty-modality/model": true,
		"plain-text/model":     true,
		"cheap/model":          true,
		"at-threshold/model":   true,
	}
	if len(got) != len(wantIDs) {
		t.Fatalf("ExploreCandidates: got %d candidates, want %d: %+v", len(got), len(wantIDs), got)
	}
	for _, m := range got {
		if !wantIDs[m.ID] {
			t.Errorf("ExploreCandidates: unexpected candidate %q", m.ID)
		}
	}

	// SortModels order: non-variable-pricing first, then ascending price sum,
	// then id — all these candidates have PromptPerM/CompletionPerM nil (sum
	// 0), so the tiebreak is purely lexicographic by id.
	var ids []string
	for _, m := range got {
		ids = append(ids, m.ID)
	}
	for i := 1; i < len(ids); i++ {
		if ids[i-1] > ids[i] {
			t.Errorf("ExploreCandidates: not in SortModels (id-ascending) order: %v", ids)
			break
		}
	}
}

func TestFormatShortCost(t *testing.T) {
	f := func(v float64) *float64 { return &v }
	cases := []struct {
		cost *float64
		want string
	}{
		{nil, "in plan"},
		{f(0), "free"},
		{f(0.0435), "~4¢"},
		{f(0.005), "<1¢"},
		{f(0.5), "$0.50"},
		{f(1.25), "$1.25"},
	}
	for _, c := range cases {
		if got := FormatShortCost(c.cost); got != c.want {
			t.Errorf("FormatShortCost(%v) = %q, want %q", c.cost, got, c.want)
		}
	}
}
