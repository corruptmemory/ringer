package store

import (
	"path/filepath"
	"testing"

	"github.com/corruptmemory/ringer/internal/catalog"
)

func seed(t *testing.T, s *Store, rows []Attempt) {
	t.Helper()
	for _, a := range rows {
		if err := s.InsertAttempt(a); err != nil {
			t.Fatal(err)
		}
	}
}

func TestScoreboardModelRows(t *testing.T) {
	s, err := Open(filepath.Join(t.TempDir(), "sb.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	// proven: model M (codex), 3 task-instances. Task t2 fails attempt-1 then passes attempt-2 (retry).
	seed(t, s, []Attempt{
		{RunID: "r1", TaskKey: "t1", Engine: "codex", Model: "M", TaskType: "code", Verdict: "PASS", Retry: 0, DurationS: 10, Tokens: 100, CreatedAt: "2026-07-10T00:00:01Z"},
		{RunID: "r1", TaskKey: "t2", Engine: "codex", Model: "M", TaskType: "code", Verdict: "FAIL", Retry: 0, DurationS: 5, Tokens: 50, CreatedAt: "2026-07-10T00:00:02Z"},
		{RunID: "r1", TaskKey: "t2", Engine: "codex", Model: "M", TaskType: "code", Verdict: "PASS", Retry: 1, DurationS: 8, Tokens: 60, CreatedAt: "2026-07-10T00:00:03Z"},
		{RunID: "r1", TaskKey: "t3", Engine: "codex", Model: "M", TaskType: "docs", Verdict: "PASS", Retry: 0, DurationS: 20, Tokens: 200, CreatedAt: "2026-07-10T00:00:04Z"},
		// probation: model N (grok), 1 failing task-instance.
		{RunID: "r2", TaskKey: "u1", Engine: "grok", Model: "N", TaskType: "code", Verdict: "FAIL", Retry: 0, DurationS: 3, Tokens: 30, CreatedAt: "2026-07-10T00:00:05Z"},
	})
	rows, err := s.ScoreboardModelRows(ScoreFilter{})
	if err != nil {
		t.Fatal(err)
	}
	byModel := map[string]ScoreModelRow{}
	for _, r := range rows {
		byModel[r.Model] = r
	}
	m := byModel["M"]
	if m.Tier != "proven" || m.Tasks != 3 || m.Attempts != 4 || m.Retries != 1 {
		t.Fatalf("M rollup wrong: %+v", m)
	}
	if m.Passed != 3 || m.Failed != 0 { // t2's FINAL verdict is PASS
		t.Fatalf("M pass/fail wrong: %+v", m)
	}
	if m.FirstTryPassRate != 2.0/3.0 { // t1,t3 first-try pass; t2 first-try fail
		t.Fatalf("M first-try rate wrong: %v", m.FirstTryPassRate)
	}
	if m.Engine != "codex" {
		t.Fatalf("M latest engine wrong: %q", m.Engine)
	}
	// Dual-median attribution lock: the DURATION median is over the per-instance
	// FINAL durations (10, 8, 20 -> 10); the TOKEN median is over EVERY attempt
	// row's tokens (100, 50, 60, 200 -> (60+100)/2 = 80), NOT final-only (which
	// would be median(100, 60, 200) = 100). Asserting 80 (not 100) locks the two
	// distinct attributions apart.
	if m.MedianDurationS == nil || *m.MedianDurationS != 10 {
		t.Fatalf("M median duration wrong: got %v want 10 (median of final durations 10,8,20)", pf(m.MedianDurationS))
	}
	if m.MedianTokensF == nil || *m.MedianTokensF != 80 {
		t.Fatalf("M median tokens wrong: got %v want 80 (all-rows median of 100,50,60,200; final-only would be 100)", pf(m.MedianTokensF))
	}
	n := byModel["N"]
	if n.Tier != "probation" || n.PassRate != 0 {
		t.Fatalf("N rollup wrong: %+v", n)
	}
	// proven sorts before probation.
	if rows[0].Model != "M" {
		t.Fatalf("ordering: proven M must precede probation N, got %q first", rows[0].Model)
	}

	// Cost lock: populate the catalog for M (non-free, non-variable) and re-query.
	// Cost = median_tokens * ((prompt+completion)/2) / 1e6 = 80*(2+4)/2/1e6 =
	// 0.00024. Model N has no catalog row, so its cost stays unknown (nil).
	p2, p4 := 2.0, 4.0
	if err := s.ReplaceCatalog([]catalog.Model{{ID: "M", PromptPerM: &p2, CompletionPerM: &p4}}); err != nil {
		t.Fatal(err)
	}
	rows2, err := s.ScoreboardModelRows(ScoreFilter{})
	if err != nil {
		t.Fatal(err)
	}
	byModel2 := map[string]ScoreModelRow{}
	for _, r := range rows2 {
		byModel2[r.Model] = r
	}
	if m2 := byModel2["M"]; m2.Cost == nil || *m2.Cost != 80*(2+4)/2/1e6 {
		t.Fatalf("M cost wrong: got %v want %v", pf(m2.Cost), 80*(2+4)/2/1e6)
	}
	if n2 := byModel2["N"]; n2.Cost != nil { // no catalog row -> cost unknown
		t.Fatalf("N cost should be nil (no catalog row), got %v", pf(n2.Cost))
	}

	// Populated-filter coverage: Engine=grok isolates model N; TaskType=docs
	// isolates model M's single docs task-instance (t3).
	grok, err := s.ScoreboardModelRows(ScoreFilter{Engine: "grok"})
	if err != nil {
		t.Fatal(err)
	}
	if len(grok) != 1 || grok[0].Model != "N" {
		t.Fatalf("Engine=grok filter: want exactly [N], got %+v", grok)
	}
	docs, err := s.ScoreboardModelRows(ScoreFilter{TaskType: "docs"})
	if err != nil {
		t.Fatal(err)
	}
	if len(docs) != 1 || docs[0].Model != "M" || docs[0].Tasks != 1 {
		t.Fatalf("TaskType=docs filter: want exactly [M with Tasks=1], got %+v", docs)
	}
}

// pf dereferences a *float64 for %v-formatting in failure messages (nil-safe).
func pf(f *float64) any {
	if f == nil {
		return nil
	}
	return *f
}
