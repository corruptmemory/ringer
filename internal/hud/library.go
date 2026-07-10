package hud

import (
	"net/http"
	"time"

	"github.com/corruptmemory/ringer/internal/artifact"
	"github.com/corruptmemory/ringer/internal/hud/views"
)

func (s *Server) handleLibrary(w http.ResponseWriter, r *http.Request) {
	// Reconcile dead runs on every poll (upstream parity); non-fatal.
	if _, err := artifact.ReconcileDeadRuns(s.stateDir, time.Now().UTC().Format(time.RFC3339)); err != nil {
		s.lg.Warnf("hud: reconcile library: %v", err)
	}
	s.renderComponent(w, r, views.LibraryPanel(artifact.ReadLibrary(s.stateDir)))
}
