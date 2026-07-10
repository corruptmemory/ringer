package hud

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/corruptmemory/ringer/internal/artifact"
	"github.com/corruptmemory/ringer/internal/state"
)

func TestArtifactsServeAndGuard(t *testing.T) {
	dir := t.TempDir()
	art := artifact.ArtifactsDir(dir)
	_ = os.MkdirAll(filepath.Join(art, "live"), 0o755)
	_ = os.WriteFile(filepath.Join(art, "live", "demo.html"), []byte("<h1>hi</h1>"), 0o644)
	_ = os.WriteFile(filepath.Join(dir, "secret.txt"), []byte("nope"), 0o644) // outside the tree
	srv := New(dir, nil).Handler()

	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/artifacts/live/demo.html", nil))
	if rec.Code != http.StatusOK || rec.Header().Get("Content-Type") != "text/html; charset=utf-8" {
		t.Fatalf("html serve: code=%d ct=%q", rec.Code, rec.Header().Get("Content-Type"))
	}
	for _, bad := range []string{"/artifacts/..%2fsecret.txt", "/artifacts/live/nope.html"} {
		r := httptest.NewRecorder()
		srv.ServeHTTP(r, httptest.NewRequest(http.MethodGet, bad, nil))
		if r.Code != http.StatusNotFound {
			t.Fatalf("%s: code=%d, want 404", bad, r.Code)
		}
	}
}

func TestArtifactsSymlinkEscapeDenied(t *testing.T) {
	dir := t.TempDir()
	art := artifact.ArtifactsDir(dir)
	if err := os.MkdirAll(art, 0o755); err != nil {
		t.Fatal(err)
	}
	// A secret OUTSIDE the artifact tree, and a symlink INSIDE pointing at it.
	secret := filepath.Join(t.TempDir(), "secret.txt")
	if err := os.WriteFile(secret, []byte("TOP-SECRET"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(secret, filepath.Join(art, "evil.txt")); err != nil {
		t.Skipf("symlink unsupported: %v", err)
	}
	srv := New(dir, nil).Handler()
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/artifacts/evil.txt", nil))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("symlink escape served (code %d) — must be 404; body=%q", rec.Code, rec.Body.String())
	}
	if strings.Contains(rec.Body.String(), "TOP-SECRET") {
		t.Fatal("served content from outside the artifact tree via symlink")
	}
}

func TestLogsTail(t *testing.T) {
	dir := t.TempDir()
	logPath := filepath.Join(t.TempDir(), "a.worker.log")
	_ = os.WriteFile(logPath, []byte(strings.Repeat("H", 6*1024)+strings.Repeat("T", 64*1024)), 0o644)
	_ = state.WriteRunState(dir, state.RunState{RunID: "run-1", Tasks: []state.TaskView{{Key: "a", LogPath: logPath}}})
	srv := New(dir, nil).Handler()

	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/logs/run-1/a", nil))
	if rec.Code != http.StatusOK || len(rec.Body.String()) != 64*1024 || strings.Contains(rec.Body.String(), "H") {
		t.Fatalf("tail wrong: code=%d len=%d", rec.Code, len(rec.Body.String()))
	}
	for _, bad := range []string{"/logs/run-1/nope", "/logs/..%2f..%2fetc/a"} {
		r := httptest.NewRecorder()
		srv.ServeHTTP(r, httptest.NewRequest(http.MethodGet, bad, nil))
		if r.Code != http.StatusNotFound {
			t.Fatalf("%s: code=%d, want 404", bad, r.Code)
		}
	}
}

func TestOpenFolderGuardsTraversal(t *testing.T) {
	srv := New(t.TempDir(), nil).Handler()
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/open-folder?run=..%2f..%2f..%2fetc", nil))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("escaping open-folder: code=%d, want 404", rec.Code)
	}
}
