package main

import (
	"bytes"
	"path/filepath"
	"strings"
	"testing"

	"github.com/corruptmemory/ringer/internal/config"
	"github.com/corruptmemory/ringer/internal/scoreboard"
	"github.com/corruptmemory/ringer/internal/store"
	"github.com/jessevdk/go-flags"
)

func TestRenderModelsTable(t *testing.T) {
	var buf bytes.Buffer
	renderModelsTable(&buf, []scoreboard.Group{{
		ScoreGroupRow: store.ScoreGroupRow{Model: "gpt-5.5", TaskType: "code", Tasks: 3, Attempts: 4, Passed: 3, Failed: 0, FirstTryPassRate: 0.67, PassRate: 1.0, LastSeen: "2026-07-10T00:00:00Z"},
		ModelDisplay:  "GPT-5.5", Harness: "Codex CLI",
	}})
	out := buf.String()
	for _, want := range []string{"code", "GPT-5.5", "gpt-5.5", "Codex CLI", "task_type"} {
		if !strings.Contains(out, want) {
			t.Fatalf("models table missing %q:\n%s", want, out)
		}
	}
}

// TestRenderModelsTableEmptyDisplayFallback locks the display fallback: a row
// with no resolved ModelDisplay must fall back to the raw slug (never render a
// blank model cell), mirroring internal/hud/views' modelName() helper.
func TestRenderModelsTableEmptyDisplayFallback(t *testing.T) {
	var buf bytes.Buffer
	renderModelsTable(&buf, []scoreboard.Group{{
		ScoreGroupRow: store.ScoreGroupRow{Model: "raw-slug", TaskType: "code", Tasks: 1, Attempts: 1, Passed: 1, LastSeen: "2026-07-10T00:00:00Z"},
		// ModelDisplay left "" and Harness "" — nothing resolved.
	}})
	out := buf.String()
	if !strings.Contains(out, "raw-slug") {
		t.Errorf("empty display should fall back to the raw model slug:\n%s", out)
	}
	if !strings.Contains(out, "unknown") {
		t.Errorf("empty harness should render as \"unknown\":\n%s", out)
	}
}

// TestHTMLRequested locks the flag-combination gate that decides whether
// Execute takes the HTML-writing branch. The bug this replaces: the old
// optional-value --html string flag could not reach its own default-path
// write (and swallowed a space-form path silently). The new shape is
// unambiguous — three independent triggers, none of them a no-op.
func TestHTMLRequested(t *testing.T) {
	cases := []struct {
		name string
		cmd  modelsCmd
		want bool
	}{
		{"no html flags", modelsCmd{}, false},
		{"--html (default path)", modelsCmd{HTML: true}, true},
		{"--open (default path + open)", modelsCmd{Open: true}, true},
		{"--html-out PATH", modelsCmd{HTMLOut: "x"}, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.cmd.HTMLRequested(); got != tc.want {
				t.Errorf("HTMLRequested() = %v, want %v", got, tc.want)
			}
		})
	}
}

// TestResolveHTMLPath locks the path-resolution precedence: --html-out wins,
// else the <state_dir>/model-scoreboard.html default that bare --html/--open
// use.
func TestResolveHTMLPath(t *testing.T) {
	cfg := &config.AppConfig{StateDir: "/tmp/ringer-state"}
	def := filepath.Join("/tmp/ringer-state", "model-scoreboard.html")

	if got := (&modelsCmd{HTML: true}).resolveHTMLPath(cfg); got != def {
		t.Errorf("bare --html should resolve to the default path %q, got %q", def, got)
	}
	if got := (&modelsCmd{Open: true}).resolveHTMLPath(cfg); got != def {
		t.Errorf("--open should resolve to the default path %q, got %q", def, got)
	}
	if got := (&modelsCmd{HTMLOut: "/somewhere/out.html"}).resolveHTMLPath(cfg); got != "/somewhere/out.html" {
		t.Errorf("--html-out should win over the default, got %q", got)
	}
}

// TestModelsFlagParsing drives the real go-flags parser (in a fresh,
// non-global parser to avoid touching the package `parser`) to prove the new
// flag shape parses the way the fix intends: --html-out binds a space-form
// path (the exact case the old optional-value flag dropped silently), --html
// is a plain bool, and a stray positional survives parse so Execute's guard
// can reject it loudly.
func TestModelsFlagParsing(t *testing.T) {
	t.Run("--html-out binds a space-form path", func(t *testing.T) {
		var c modelsCmd
		p := flags.NewParser(&c, flags.None)
		rest, err := p.ParseArgs([]string{"--html-out", "/tmp/report.html"})
		if err != nil {
			t.Fatalf("ParseArgs: %v", err)
		}
		if c.HTMLOut != "/tmp/report.html" {
			t.Errorf("HTMLOut = %q, want /tmp/report.html", c.HTMLOut)
		}
		if len(rest) != 0 {
			t.Errorf("unexpected leftover args: %v", rest)
		}
	})

	t.Run("--html is a plain bool", func(t *testing.T) {
		var c modelsCmd
		p := flags.NewParser(&c, flags.None)
		if _, err := p.ParseArgs([]string{"--html"}); err != nil {
			t.Fatalf("ParseArgs: %v", err)
		}
		if !c.HTML {
			t.Error("--html should set HTML=true")
		}
	})

	t.Run("stray positional survives parse and Execute rejects it", func(t *testing.T) {
		var c modelsCmd
		p := flags.NewParser(&c, flags.None)
		rest, err := p.ParseArgs([]string{"stray"})
		if err != nil {
			t.Fatalf("ParseArgs: %v", err)
		}
		if len(rest) != 1 || rest[0] != "stray" {
			t.Fatalf("expected leftover positional [stray], got %v", rest)
		}
		if err := c.Execute(rest); err == nil {
			t.Error("Execute must reject a stray positional, got nil error")
		} else if !strings.Contains(err.Error(), "unexpected argument") {
			t.Errorf("error should name the unexpected argument, got %v", err)
		}
	})
}
