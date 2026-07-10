package hud

import (
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"time"

	"github.com/corruptmemory/ringer/internal/hud/views"
	"github.com/corruptmemory/ringer/internal/state"
)

func (s *Server) handleRoot(w http.ResponseWriter, r *http.Request) {
	s.renderComponent(w, r, views.Layout())
}

// hudRunsLimit caps how many recent runs the panel shows (upstream: 12).
const hudRunsLimit = 12

func (s *Server) handleRuns(w http.ResponseWriter, r *http.Request) {
	s.renderComponent(w, r, views.RunsPanel(s.scanRunStates()))
}

// scanRunStates reads <stateDir>/runs/*.json, newest-mtime first, capped.
func (s *Server) scanRunStates() []state.RunState {
	dir := filepath.Join(s.stateDir, "runs")
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	type stamped struct {
		mod time.Time
		rs  state.RunState
	}
	var out []stamped
	for _, e := range entries {
		if e.IsDir() || filepath.Ext(e.Name()) != ".json" {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		data, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err != nil {
			continue
		}
		var rs state.RunState
		if err := jsonUnmarshal(data, &rs); err != nil || rs.RunID == "" {
			continue
		}
		out = append(out, stamped{info.ModTime(), rs})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].mod.After(out[j].mod) })
	if len(out) > hudRunsLimit {
		out = out[:hudRunsLimit]
	}
	res := make([]state.RunState, len(out))
	for i, st := range out {
		res[i] = st.rs
	}
	return res
}
