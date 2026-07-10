package catalog

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

type fakeStore struct {
	models []Model
	events []Event
}

func (f *fakeStore) CatalogModels() ([]Model, error) { return f.models, nil }
func (f *fakeStore) ReplaceCatalog(m []Model) error  { f.models = m; return nil }
func (f *fakeStore) AppendCatalogEvents(e []Event) error {
	f.events = append(f.events, e...)
	return nil
}

func writePayload(t *testing.T, dir, prompt string) string {
	t.Helper()
	p := filepath.Join(dir, "cat.json")
	body := `{"data":[{"id":"a/b","name":"A","context_length":1000,"pricing":{"prompt":"` + prompt + `","completion":"0.000001"},"architecture":{"modality":"text"}}]}`
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestRefreshDiffsAndReplaces(t *testing.T) {
	dir := t.TempDir()
	fs := &fakeStore{}

	res1, err := Refresh(fs, writePayload(t, dir, "0.000001"), time.Second, "T1")
	if err != nil {
		t.Fatal(err)
	}
	if len(res1.Models) != 1 || len(fs.models) != 1 || fs.models[0].ID != "a/b" {
		t.Fatalf("first refresh: models=%+v", fs.models)
	}

	res2, err := Refresh(fs, writePayload(t, dir, "0.000009"), time.Second, "T2")
	if err != nil {
		t.Fatal(err)
	}
	var sawPriceChange bool
	for _, e := range res2.Events {
		if e.Kind == "price_change" && e.ModelID == "a/b" {
			sawPriceChange = true
		}
	}
	if !sawPriceChange {
		t.Fatalf("second refresh did not emit price_change: %+v", res2.Events)
	}
}
