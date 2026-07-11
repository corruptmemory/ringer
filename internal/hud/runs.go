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
	// An orphan run — not Done but absent from the pid-pruned active-runs
	// registry (its orchestrator crashed or was hard-killed) — is flagged
	// Died so the HUD renders it "died" instead of perpetually "working".
	// Same liveness signal artifact.ReconcileDeadRuns uses for the library.
	// Only trust the signal when active-runs read cleanly (else leave as-is,
	// so a transient read error never falsely buries a live run).
	active, activeErr := state.ReadActiveRuns(s.stateDir)
	res := make([]state.RunState, len(out))
	for i, st := range out {
		rs := st.rs
		if activeErr == nil && !rs.Done {
			if _, ok := active[rs.RunID]; !ok {
				rs.Died = true
			}
		}
		res[i] = rs
	}
	return res
}
