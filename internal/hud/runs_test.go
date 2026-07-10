package hud

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/corruptmemory/ringer/internal/state"
)

func TestHudRunsRendersFromGoState(t *testing.T) {
	dir := t.TempDir()
	if err := state.WriteRunState(dir, state.RunState{
		RunID: "demo-1", RunName: "demo", Identity: "jim", Done: true,
		StartedAt: "2026-07-09T00:00:00Z", UpdatedAt: "2026-07-09T00:00:05Z",
		Tasks: []state.TaskView{
			{Key: "alpha", Engine: "mock", Status: "passed", StartedAt: "2026-07-09T00:00:00Z", EndedAt: "2026-07-09T00:00:03Z", LogPath: "/x/alpha.log"},
			{Key: "bravo", Engine: "mock", Status: "failed", StartedAt: "2026-07-09T00:00:00Z", EndedAt: "2026-07-09T00:00:05Z"},
		},
	}); err != nil {
		t.Fatal(err)
	}
	srv := New(dir, nil).Handler()
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/hud/runs", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	body := rec.Body.String()
	for _, want := range []string{
		"demo",           // run name
		"alpha", "bravo", // task keys
		`class="run fail"`,       // finished-with-failure bucket (derived, no adapter)
		"finished &amp; checked", // passed task label (templ HTML-escapes the &)
		"failed",                 // failed task label
		"/logs/demo-1/alpha",     // log link for the task that has a log path
		"1 passed · 1 failed",    // OutcomeText
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("runs fragment missing %q\n---\n%s", want, body)
		}
	}
}

func TestHudRunsEmpty(t *testing.T) {
	srv := New(t.TempDir(), nil).Handler()
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/hud/runs", nil))
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "No runs yet") {
		t.Fatalf("empty runs: code=%d body=%q", rec.Code, rec.Body.String())
	}
}
