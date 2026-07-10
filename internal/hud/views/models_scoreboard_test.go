package views

import (
	"strings"
	"testing"

	"github.com/corruptmemory/ringer/internal/scoreboard"
	"github.com/corruptmemory/ringer/internal/store"
)

func fixedScoreboardRows() []scoreboard.Row {
	tasks := int64(512)
	dur := 12.5
	return []scoreboard.Row{
		{
			ScoreModelRow: store.ScoreModelRow{
				Model: "gpt-5.5", Engine: "codex", Tier: "proven",
				Tasks: 4, Attempts: 5, Retries: 1, Passed: 4, Failed: 0,
				FirstTryPassRate: 0.75, PassRate: 1.0,
				MedianDurationS: &dur, MedianTokensF: nil, LastSeen: "2026-07-10T00:00:00Z",
			},
			ModelDisplay: "GPT-5.5", Harness: "Codex CLI", Access: "ChatGPT Plus",
			MedianTokens: &tasks,
			TaskTypes: []scoreboard.TaskTypeRow{
				{TaskType: "code", Tasks: 3, Attempts: 4, Passed: 3, Failed: 0, FirstTryPassRate: 0.67, PassRate: 1.0, LastSeen: "2026-07-10T00:00:00Z"},
				{TaskType: "docs", Tasks: 1, Attempts: 1, Passed: 1, Failed: 0, FirstTryPassRate: 1.0, PassRate: 1.0, LastSeen: "2026-07-09T00:00:00Z"},
			},
		},
		{
			ScoreModelRow: store.ScoreModelRow{
				Model: "untested-model", Engine: "opencode", Tier: "probation",
				Tasks: 1, Attempts: 1, Retries: 0, Passed: 0, Failed: 1,
				FirstTryPassRate: 0.0, PassRate: 0.0, LastSeen: "2026-07-08T00:00:00Z",
			},
			ModelDisplay: "", Harness: "", Access: "",
		},
	}
}

func fixedScoreboardNotes() map[string][]scoreboard.RenderedNote {
	return map[string][]scoreboard.RenderedNote{
		"gpt-5.5": {{Date: "July 9", Body: "sharp on Go generics, weak on shell quoting"}},
	}
}

func TestModelScoreboardGolden(t *testing.T) {
	page := renderComponentString(t, ModelScoreboardPage(fixedScoreboardRows(), fixedScoreboardNotes()))
	assertGolden(t, "models_scoreboard.golden.html", page)
	for _, must := range []string{
		"GPT-5.5", "gpt-5.5", "Codex CLI", "proven", "probation",
		"untested-model", "sharp on Go generics", "July 9", "code", "docs",
		"no judgment notes yet", "class=\"page\"",
	} {
		if !strings.Contains(page, must) {
			t.Errorf("model scoreboard page missing %q", must)
		}
	}
}

func TestModelScoreboardEmpty(t *testing.T) {
	page := renderComponentString(t, ModelScoreboardPage(nil, nil))
	if !strings.Contains(page, "No local model evidence matched these filters.") {
		t.Errorf("empty scoreboard page missing the no-evidence message:\n%s", page)
	}
	if strings.Contains(page, "<table>") {
		t.Errorf("empty scoreboard page should not render an empty table")
	}
}
