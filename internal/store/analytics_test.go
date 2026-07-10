package store

import (
	"path/filepath"
	"testing"

	"github.com/corruptmemory/ringer/internal/catalog"
)

func TestCatalogRoundTrip(t *testing.T) {
	s, err := Open(filepath.Join(t.TempDir(), "c.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	pm := 1.0
	if err := s.ReplaceCatalog([]catalog.Model{{ID: "a/b", Name: "A", PromptPerM: &pm, CompletionPerM: &pm}, {ID: "z/free", Free: true}}); err != nil {
		t.Fatal(err)
	}
	all, _ := s.CatalogModels()
	if len(all) != 2 {
		t.Fatalf("want 2 models, got %d", len(all))
	}
	free, _ := s.FreeCatalogModels()
	if len(free) != 1 || free[0].ID != "z/free" {
		t.Fatalf("free filter wrong: %+v", free)
	}
	if err := s.AppendCatalogEvents([]catalog.Event{{TS: "T", Kind: "added", ModelID: "a/b", Payload: map[string]any{"free": false}}}); err != nil {
		t.Fatal(err)
	}
	evs, _ := s.CatalogEvents(10)
	if len(evs) != 1 || evs[0].Kind != "added" {
		t.Fatalf("events round-trip wrong: %+v", evs)
	}
}
