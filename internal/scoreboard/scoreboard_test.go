package scoreboard

import (
	"path/filepath"
	"testing"

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
