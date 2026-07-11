// internal/hud/models.go
package hud

import (
	"net/http"
	"path/filepath"

	"github.com/corruptmemory/ringer/internal/hud/views"
	"github.com/corruptmemory/ringer/internal/scoreboard"
	"github.com/corruptmemory/ringer/internal/store"
)

// handleModels renders the tiered per-model scoreboard from the SQLite eval
// store. A missing/empty DB yields an empty-state panel, never a 500.
func (s *Server) handleModels(w http.ResponseWriter, r *http.Request) {
	dbPath := filepath.Join(s.stateDir, "ringer.db")
	st, err := store.Open(dbPath)
	if err != nil {
		s.lg.Warnf("hud: models: open store: %v", err)
		s.renderComponent(w, r, views.ModelsPanel(nil))
		return
	}
	defer st.Close()
	rows, err := scoreboard.Scoreboard(st, scoreboard.Filter{}, scoreboard.LoadRegistry(""))
	if err != nil {
		s.lg.Warnf("hud: models: scoreboard: %v", err)
		rows = nil
	}
	s.renderComponent(w, r, views.ModelsPanel(rows))
}
