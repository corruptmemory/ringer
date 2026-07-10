package hud

import (
	"encoding/json"
	"fmt"
	"net"
	"net/http"

	"github.com/a-h/templ"
	"github.com/corruptmemory/ringer/internal/logging"
	"github.com/go-chi/chi/v5"
)

// DefaultPort is the fixed Ringside port.
const DefaultPort = 8700

// Server serves the Ringside dashboard for one state directory.
type Server struct {
	stateDir string
	lg       logging.Logger
}

// New builds a Server rooted at stateDir.
func New(stateDir string, lg logging.Logger) *Server {
	if lg == nil {
		lg = logging.Default()
	}
	return &Server{stateDir: stateDir, lg: lg}
}

// Handler builds the chi router. Exposed for httptest.
func (s *Server) Handler() http.Handler {
	r := chi.NewRouter()
	r.Get("/", s.handleRoot)
	r.Get("/healthz", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) })
	r.Get("/hud/runs", s.handleRuns)
	r.Get("/hud/library", s.handleLibrary)
	r.Get("/hud/models", s.handleModels)
	r.Handle("/static/*", staticHandler())
	r.Get("/artifacts/*", s.handleArtifacts)
	r.Get("/logs/*", s.handleLogs)
	r.Get("/api/open-folder", s.handleOpenFolder)
	return r
}

// ListenAndServe binds 127.0.0.1:<port> and serves until error, failing
// loudly if the port is in use (no fallback scan, as upstream).
func (s *Server) ListenAndServe(port int) error {
	addr := fmt.Sprintf("127.0.0.1:%d", port)
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return fmt.Errorf("could not start Ringside on %s; that port may already be in use: %w", addr, err)
	}
	s.lg.Infof("Ringside: http://%s", addr)
	return http.Serve(ln, s.Handler())
}

// renderComponent writes a templ component as an HTML response, logging a
// render error (a half-written body is the best we can do once bytes flow).
// templ.Component is the interface every generated view satisfies.
func (s *Server) renderComponent(w http.ResponseWriter, r *http.Request, c templ.Component) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := c.Render(r.Context(), w); err != nil {
		s.lg.Warnf("hud: render: %v", err)
	}
}

// jsonUnmarshal is the shared decode helper for run-state files (Tasks 6, 8).
func jsonUnmarshal(data []byte, v any) error { return json.Unmarshal(data, v) }
