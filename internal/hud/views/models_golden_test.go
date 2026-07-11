package views

import (
	"strings"
	"testing"
)

// TestModelsPanelGolden locks the live-HUD /hud/models fragment's byte shape.
// Reuses models_scoreboard_test.go's fixedScoreboardRows fixture (same two
// rows: a proven gpt-5.5 with a resolved identity, and a probation
// untested-model with none) so the live table and the standalone
// ModelScoreboardPage report are exercised against one shared fixture.
func TestModelsPanelGolden(t *testing.T) {
	page := renderComponentString(t, ModelsPanel(fixedScoreboardRows()))
	assertGolden(t, "models_panel.golden.html", page)
	for _, must := range []string{
		"GPT-5.5", "Codex CLI", "proven", "probation",
		"untested-model", "id=\"models\"", "<table>",
	} {
		if !strings.Contains(page, must) {
			t.Errorf("models panel missing %q:\n%s", must, page)
		}
	}
}

// TestModelsPanelEmpty covers both the "no DB" and "DB with no attempts yet"
// paths (models.go passes nil rows for both), which must render the
// empty-state line rather than an empty <table>.
func TestModelsPanelEmpty(t *testing.T) {
	page := renderComponentString(t, ModelsPanel(nil))
	if !strings.Contains(page, "No model evidence yet.") {
		t.Errorf("empty models panel missing the empty-state line:\n%s", page)
	}
	if strings.Contains(page, "<table>") {
		t.Errorf("empty models panel should not render an empty table")
	}
}
