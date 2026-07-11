// cmd/ringer/models.go
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"text/tabwriter"

	"github.com/corruptmemory/ringer/internal/config"
	"github.com/corruptmemory/ringer/internal/hud/views"
	"github.com/corruptmemory/ringer/internal/scoreboard"
	"github.com/corruptmemory/ringer/internal/store"
)

type modelsCmd struct {
	TaskType string `long:"task-type"`
	Model    string `long:"model"`
	Engine   string `long:"engine"`
	Since    string `long:"since" description:"only tasks whose latest attempt is on/after YYYY-MM-DD"`
	Explore  bool   `long:"explore" description:"tiers + untested catalog candidates"`
	HTML     bool   `long:"html" description:"write the scoreboard HTML to the default path"`
	HTMLOut  string `long:"html-out" description:"write the scoreboard HTML to this path (overrides --html default)"`
	Open     bool   `long:"open" description:"open the written HTML in a browser"`
	JSON     bool   `long:"json"`
}

func (c *modelsCmd) Execute(args []string) error {
	// A stray positional is a user error (a mistyped flag, a shell-glob that
	// expanded) — fail loud rather than silently ignore it (this project's
	// no-silent-failures rule).
	if len(args) > 0 {
		return fmt.Errorf("models: unexpected argument %q", args[0])
	}
	cfg, err := loadConfig()
	if err != nil {
		return err
	}
	s, err := store.Open(cfg.DBPath())
	if err != nil {
		return err
	}
	defer s.Close()
	reg := scoreboard.LoadRegistry(cfg.ModelIdentityPath())
	f := scoreboard.Filter{TaskType: c.TaskType, Model: c.Model, Engine: c.Engine, Since: c.Since}

	if c.JSON {
		rollup, err := scoreboard.Scoreboard(s, f, reg)
		if err != nil {
			return err
		}
		groups, err := scoreboard.Groups(s, f, reg)
		if err != nil {
			return err
		}
		return json.NewEncoder(os.Stdout).Encode(map[string]any{"rollup": rollup, "groups": groups})
	}
	if c.Explore {
		return runExplore(os.Stdout, s, f, reg)
	}
	if c.HTMLRequested() {
		return c.writeHTML(cfg, s, f, reg)
	}
	groups, err := scoreboard.Groups(s, f, reg)
	if err != nil {
		return err
	}
	renderModelsTable(os.Stdout, groups)
	return nil
}

func (c *modelsCmd) HTMLRequested() bool { return c.HTML || c.Open || c.HTMLOut != "" }

func renderModelsTable(w io.Writer, groups []scoreboard.Group) {
	tw := tabwriter.NewWriter(w, 0, 2, 2, ' ', 0)
	fmt.Fprintln(tw, "task_type\tmodel\tharness\ttasks\tattempts\tpassed\tfailed\tpass\tfirst\tdur_ms\ttokens\tlast_seen")
	for _, g := range groups {
		display := g.ModelDisplay
		if display == "" {
			display = g.Model
		} else if display != g.Model {
			display = fmt.Sprintf("%s (%s)", display, g.Model)
		}
		harness := g.Harness
		if harness == "" {
			harness = "unknown"
		}
		fmt.Fprintf(tw, "%s\t%s\t%s\t%d\t%d\t%d\t%d\t%.2f\t%.2f\t%s\t%s\t%s\n",
			g.TaskType, display, harness, g.Tasks, g.Attempts, g.Passed, g.Failed,
			g.PassRate, g.FirstTryPassRate, msString(g.MedianDurationS), tokString(g.MedianTokens), g.LastSeen)
	}
	tw.Flush()
	fmt.Fprintln(w, "Judgment layer: docs/MODEL-NOTES.md")
}

func msString(sec *float64) string {
	if sec == nil {
		return ""
	}
	return fmt.Sprintf("%d", int64(*sec*1000+0.5))
}
func tokString(t *int64) string {
	if t == nil {
		return ""
	}
	return fmt.Sprintf("%d", *t)
}

func runExplore(w io.Writer, s *store.Store, f scoreboard.Filter, reg scoreboard.Registry) error {
	groups, err := scoreboard.Groups(s, f, reg)
	if err != nil {
		return err
	}
	fmt.Fprintln(w, "TIERS")
	if len(groups) == 0 {
		fmt.Fprintln(w, "  no local evidence")
	}
	tested := map[string]bool{}
	for _, g := range groups {
		label := "probation"
		if g.Tasks >= 3 {
			label = "proven"
		}
		fmt.Fprintf(w, "  %-9s %s task_type=%s tasks=%d first=%.2f pass=%.2f\n", label, g.Model, g.TaskType, g.Tasks, g.FirstTryPassRate, g.PassRate)
		tested[g.Model] = true
	}
	models, err := s.CatalogModels()
	if err != nil {
		return err
	}
	fmt.Fprintln(w, "CANDIDATES")
	cands := scoreboard.ExploreCandidates(models, tested)
	if len(cands) == 0 {
		fmt.Fprintln(w, "  no untested text->text candidates with context >= 32000")
	}
	for _, m := range cands {
		marker := ""
		if m.Free {
			marker = " FREE"
		}
		fmt.Fprintf(w, "  untested %s ctx=%d%s\n", m.ID, m.ContextLength, marker)
	}
	return nil
}

func (c *modelsCmd) writeHTML(cfg *config.AppConfig, s *store.Store, f scoreboard.Filter, reg scoreboard.Registry) error {
	path := c.resolveHTMLPath(cfg)
	if err := renderScoreboardHTMLFile(path, s, f, reg, scoreboard.LoadNotes(cfg.ModelNotesPath())); err != nil {
		return err
	}
	fmt.Printf("wrote %s\n", path)
	if c.Open {
		return openInBrowser(path)
	}
	return nil
}

// resolveHTMLPath picks the HTML output path: --html-out when set, else the
// default (<state_dir>/model-scoreboard.html) that bare --html / --open use.
func (c *modelsCmd) resolveHTMLPath(cfg *config.AppConfig) string {
	if c.HTMLOut != "" {
		return c.HTMLOut
	}
	return defaultScoreboardHTMLPath(cfg)
}

// defaultScoreboardHTMLPath is <state_dir>/model-scoreboard.html.
func defaultScoreboardHTMLPath(cfg *config.AppConfig) string {
	return filepath.Join(cfg.StateDirPath(), "model-scoreboard.html")
}

// renderScoreboardHTMLFile builds the tiered rollup (scoreboard.Scoreboard),
// resolves each row's judgment notes (scoreboard.RenderNotes, newest-first,
// capped at 5 — matching ringer.py's render_notes_list default), and renders
// views.ModelScoreboardPage to path.
func renderScoreboardHTMLFile(path string, s *store.Store, f scoreboard.Filter, reg scoreboard.Registry, notes []scoreboard.NoteSection) error {
	rows, err := scoreboard.Scoreboard(s, f, reg)
	if err != nil {
		return err
	}
	notesByModel := make(map[string][]scoreboard.RenderedNote, len(rows))
	for _, r := range rows {
		notesByModel[r.Model] = scoreboard.RenderNotes(r.Model, notes, 5)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	out, err := os.Create(path)
	if err != nil {
		return err
	}
	defer out.Close()
	return views.ModelScoreboardPage(rows, notesByModel).Render(context.Background(), out)
}

func init() {
	parser.AddCommand("models", "Per-model performance scoreboard",
		"Show the local per-model scoreboard from the SQLite eval store. "+
			"Default prints a table; --json emits the rollup+groups; --explore lists "+
			"tiers plus untested catalog candidates; --html writes the HTML report to "+
			"the default path, --html-out PATH writes it to PATH, and --open opens it.",
		&modelsCmd{})
}
