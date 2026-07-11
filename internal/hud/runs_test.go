package hud

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

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

func TestScanRunStatesSortCapSkip(t *testing.T) {
	dir := t.TempDir()
	// 14 valid runs with increasing mtimes + 1 garbage file.
	base := time.Date(2026, 7, 9, 0, 0, 0, 0, time.UTC)
	for i := 0; i < 14; i++ {
		id := fmt.Sprintf("run-%02d", i)
		if err := state.WriteRunState(dir, state.RunState{RunID: id, RunName: id}); err != nil {
			t.Fatal(err)
		}
		mt := base.Add(time.Duration(i) * time.Minute)
		if err := os.Chtimes(filepath.Join(dir, "runs", id+".json"), mt, mt); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(dir, "runs", "garbage.json"), []byte("{ not json"), 0o644); err != nil {
		t.Fatal(err)
	}
	got := New(dir, nil).scanRunStates()
	if len(got) != 12 {
		t.Fatalf("cap: got %d runs, want 12", len(got))
	}
	// Newest-first: run-13 (latest mtime) must be first, and the oldest two
	// (run-00, run-01) must have been dropped by the cap.
	if got[0].RunID != "run-13" {
		t.Fatalf("newest-first: got[0] = %q, want run-13", got[0].RunID)
	}
	for _, r := range got {
		if r.RunID == "run-00" || r.RunID == "run-01" {
			t.Fatalf("cap should have dropped the two oldest, found %q", r.RunID)
		}
	}
	// garbage.json skipped, not counted or panicking.
}

func TestScanRunStatesMarksOrphanDied(t *testing.T) {
	dir := t.TempDir()
	// Orphan: not Done, never registered in active-runs (crashed / hard-killed).
	if err := state.WriteRunState(dir, state.RunState{
		RunID: "ghost-1", RunName: "ghost", Done: false,
		Tasks: []state.TaskView{{Key: "t", Status: "running"}},
	}); err != nil {
		t.Fatal(err)
	}
	// Live: not Done, registered with this test's (alive) pid → must stay live.
	if err := state.WriteRunState(dir, state.RunState{
		RunID: "alive-1", RunName: "alive", Done: false, PID: os.Getpid(),
		Tasks: []state.TaskView{{Key: "t", Status: "running"}},
	}); err != nil {
		t.Fatal(err)
	}
	if err := state.RegisterActiveRun(dir, "alive-1", "jim", "alive", "/w", os.Getpid(), "2026-07-09T00:00:00Z"); err != nil {
		t.Fatal(err)
	}
	byID := map[string]state.RunState{}
	for _, r := range New(dir, nil).scanRunStates() {
		byID[r.RunID] = r
	}
	if !byID["ghost-1"].Died {
		t.Fatal("orphan run (absent from pid-pruned active-runs) must be marked Died")
	}
	if byID["alive-1"].Died {
		t.Fatal("live run (registered with an alive pid) must NOT be marked Died")
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
