package hud

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/corruptmemory/ringer/internal/logging"
)

func testServer(t *testing.T, stateDir string) http.Handler {
	t.Helper()
	return New(stateDir, logging.Default()).Handler()
}

func TestRootRendersShell(t *testing.T) {
	srv := testServer(t, t.TempDir())
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	if !strings.HasPrefix(rec.Header().Get("Content-Type"), "text/html") {
		t.Fatalf("content-type = %q", rec.Header().Get("Content-Type"))
	}
	body := rec.Body.String()
	for _, want := range []string{
		`href="/static/ringside.css"`,
		`src="/static/vendor/htmx.min.js"`,
		`hx-get="/hud/runs"`,
		`hx-swap="morph"`,
		`id="runs-panel"`,
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("shell missing %q", want)
		}
	}
}

func TestHealthz(t *testing.T) {
	srv := testServer(t, t.TempDir())
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/healthz", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("healthz status = %d", rec.Code)
	}
}
