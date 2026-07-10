package hud

import (
	"net/http"

	"github.com/corruptmemory/ringer/internal/hud/views"
)

// handleModels renders the Plan-5 stub; the model-log analytics read the
// SQLite eval store and land in Plan 5.
func (s *Server) handleModels(w http.ResponseWriter, r *http.Request) {
	s.renderComponent(w, r, views.ModelsPanel())
}
