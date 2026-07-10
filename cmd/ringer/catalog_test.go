package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/corruptmemory/ringer/internal/catalog"
)

func TestRenderCatalogTable(t *testing.T) {
	var buf bytes.Buffer
	p := 0.5
	renderCatalogTable(&buf, []catalog.Model{{ID: "a/b", ContextLength: 128000, PromptPerM: &p, CompletionPerM: &p}, {ID: "z/free", Free: true}})
	out := buf.String()
	// Header carries all five Go-authoritative columns.
	for _, col := range []string{"id", "$/M in", "$/M out", "ctx", "FREE"} {
		if !strings.Contains(out, col) {
			t.Fatalf("catalog table header missing %q:\n%s", col, out)
		}
	}
	// The free model shows the FREE marker; its priced sibling renders its price.
	if !strings.Contains(out, "a/b") || !strings.Contains(out, "FREE") {
		t.Fatalf("catalog table wrong:\n%s", out)
	}
	if !strings.Contains(out, "0.5") {
		t.Fatalf("paid model price 0.5 should render:\n%s", out)
	}
	// Only the free row is marked FREE — the priced a/b row must not be.
	for _, line := range strings.Split(strings.TrimSpace(out), "\n") {
		if strings.Contains(line, "a/b") && strings.Contains(line, "FREE") {
			t.Fatalf("priced model a/b must not be marked FREE:\n%s", line)
		}
	}
}

func TestDescribeEvent(t *testing.T) {
	t.Run("added with free carries a FREE suffix", func(t *testing.T) {
		got := describeCatalogEvent(catalog.Event{TS: "2026-07-10T00:00:00Z", Kind: "added", ModelID: "a/b", Payload: map[string]any{"free": true}})
		if !strings.Contains(got, "a/b") || !strings.Contains(got, "added") {
			t.Fatalf("describe wrong: %q", got)
		}
		if !strings.Contains(got, " FREE") {
			t.Fatalf("added+free event should carry a FREE suffix: %q", got)
		}
	})

	t.Run("added without free has no FREE suffix", func(t *testing.T) {
		got := describeCatalogEvent(catalog.Event{TS: "2026-07-10T00:00:00Z", Kind: "added", ModelID: "a/b", Payload: map[string]any{"free": false}})
		if strings.Contains(got, "FREE") {
			t.Fatalf("added event without free must not carry a FREE suffix: %q", got)
		}
	})

	t.Run("price_change names the kind and shows the transitions", func(t *testing.T) {
		got := describeCatalogEvent(catalog.Event{TS: "2026-07-10T00:00:00Z", Kind: "price_change", ModelID: "a/b", Payload: map[string]any{
			"old_prompt_per_m": 3.0, "new_prompt_per_m": 4.0,
			"old_completion_per_m": 6.0, "new_completion_per_m": 8.0,
		}})
		if !strings.Contains(got, "price_change") || !strings.Contains(got, "->") {
			t.Fatalf("price_change describe wrong: %q", got)
		}
	})
}

// TestCatalogExecuteStrayPositional locks the no-silent-failures guard: a stray
// positional (e.g. `catalog --refresh typo`) must error loudly, not be dropped.
// The guard sits at the top of Execute (before any store access), so no config
// or DB setup is needed to exercise it.
func TestCatalogExecuteStrayPositional(t *testing.T) {
	if err := (&catalogCmd{}).Execute([]string{"typo"}); err == nil {
		t.Fatal("Execute must reject a stray positional, got nil error")
	} else if !strings.Contains(err.Error(), "unexpected argument") {
		t.Fatalf("error should name the unexpected argument, got %v", err)
	}
}

// TestCatalogEmptyErrorTailoredToSource locks fix #2: an empty --file snapshot
// must not advise `--refresh` (wrong advice — refresh rewrites the DB, not the
// file), while an empty DB (no --file) keeps the refresh hint.
func TestCatalogEmptyErrorTailoredToSource(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.toml")
	if err := os.WriteFile(cfgPath, []byte("state_dir = "+strconv.Quote(dir)+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	prevConfig := opts.Config
	opts.Config = cfgPath
	t.Cleanup(func() { opts.Config = prevConfig })

	t.Run("empty --file names the file, not --refresh", func(t *testing.T) {
		emptyFile := filepath.Join(dir, "empty.json")
		if err := os.WriteFile(emptyFile, []byte(`{"data": []}`), 0o644); err != nil {
			t.Fatal(err)
		}
		err := (&catalogCmd{File: emptyFile}).Execute(nil)
		if err == nil {
			t.Fatal("empty --file catalog should error")
		}
		if !strings.Contains(err.Error(), emptyFile) {
			t.Errorf("empty --file error should name the file, got %v", err)
		}
		if strings.Contains(err.Error(), "--refresh") {
			t.Errorf("empty --file error must not advise --refresh, got %v", err)
		}
	})

	t.Run("empty DB keeps the --refresh hint", func(t *testing.T) {
		err := (&catalogCmd{}).Execute(nil)
		if err == nil {
			t.Fatal("empty DB catalog should error")
		}
		if !strings.Contains(err.Error(), "--refresh") {
			t.Errorf("empty DB error should advise --refresh, got %v", err)
		}
	})
}
