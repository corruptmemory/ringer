package hud

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/corruptmemory/ringer/internal/artifact"
)

func TestHudLibraryReconcilesAndRenders(t *testing.T) {
	dir := t.TempDir()
	// A live entry whose run is NOT registered → must render as died.
	if err := artifact.WriteLibrary(dir, artifact.Library{Artifacts: map[string]artifact.Entry{
		"demo": {State: "live", CurrentRunID: "demo-1", Identity: "jim"},
	}}); err != nil {
		t.Fatal(err)
	}
	srv := New(dir, nil).Handler()
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/hud/library", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	body := rec.Body.String()
	if !strings.Contains(body, "demo") || !strings.Contains(body, `artifact-row died`) {
		t.Fatalf("library did not reconcile-then-render died: %s", body)
	}
	// And it persisted the flip (reconcile side effect).
	if artifact.ReadLibrary(dir).Artifacts["demo"].State != "died" {
		t.Fatal("reconcile flip not persisted")
	}
}

func TestHudModelsStub(t *testing.T) {
	srv := New(t.TempDir(), nil).Handler()
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/hud/models", nil))
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "Plan 5") {
		t.Fatalf("models stub: code=%d body=%q", rec.Code, rec.Body.String())
	}
}
