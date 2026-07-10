package hud

import (
	"net/http"

	"github.com/corruptmemory/ringer/internal/hud/views"
)

func (s *Server) handleRoot(w http.ResponseWriter, r *http.Request) {
	s.renderComponent(w, r, views.Layout())
}
