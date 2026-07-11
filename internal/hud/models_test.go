package hud

import (
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/corruptmemory/ringer/internal/store"
)

func TestModelsPanelRendersFromStore(t *testing.T) {
	dir := t.TempDir()
	s, err := store.Open(filepath.Join(dir, "ringer.db"))
	if err != nil {
		t.Fatal(err)
	}
	for _, a := range []store.Attempt{
		{RunID: "r1", TaskKey: "t1", Engine: "codex", Model: "gpt-5.5", TaskType: "code", Verdict: "PASS", Tokens: 100, CreatedAt: "2026-07-10T00:00:01Z"},
		{RunID: "r1", TaskKey: "t2", Engine: "codex", Model: "gpt-5.5", TaskType: "code", Verdict: "PASS", Tokens: 100, CreatedAt: "2026-07-10T00:00:02Z"},
		{RunID: "r1", TaskKey: "t3", Engine: "codex", Model: "gpt-5.5", TaskType: "docs", Verdict: "PASS", Tokens: 100, CreatedAt: "2026-07-10T00:00:03Z"},
	} {
		if err := s.InsertAttempt(a); err != nil {
			t.Fatal(err)
		}
	}
	s.Close()

	srv := New(dir, nil)
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/hud/models", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("code=%d", rec.Code)
	}
	body := rec.Body.String()
	if !strings.Contains(body, "GPT-5.5") || !strings.Contains(body, "proven") {
		t.Fatalf("models panel missing data:\n%s", body)
	}
}

func TestModelsPanelEmptyStateNoDB(t *testing.T) {
	srv := New(t.TempDir(), nil) // no ringer.db yet
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/hud/models", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("empty-state must be 200, got %d", rec.Code)
	}
}
