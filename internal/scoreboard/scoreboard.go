// internal/scoreboard/scoreboard.go
package scoreboard

import (
	"math"
	"sort"
	"strings"

	"github.com/corruptmemory/ringer/internal/catalog"
	"github.com/corruptmemory/ringer/internal/store"
)

type Filter struct{ TaskType, Model, Engine, Since string }

type TaskTypeRow struct {
	TaskType                   string
	Tasks, Attempts            int
	Passed, Failed             int
	FirstTryPassRate, PassRate float64
	LastSeen                   string
}

type Row struct {
	store.ScoreModelRow
	ModelDisplay, Harness, Access string
	MedianTokens                  *int64 // floored from MedianTokensF (Python // semantics)
	TaskTypes                     []TaskTypeRow
}

// Group is a rich per-(model, task_type) row with identity resolved — the
// flat `models` table / HUD groups view.
type Group struct {
	store.ScoreGroupRow
	ModelDisplay, Harness, Access string
	MedianTokens                  *int64
}

func floorTokens(f *float64) *int64 {
	if f == nil {
		return nil
	}
	v := int64(math.Floor(*f))
	return &v
}

// Groups returns the flat per-(model, task_type) rows, identity-resolved, in
// `models`-table order (task_type, pass_rate DESC, first DESC, model).
func Groups(s *store.Store, f Filter, reg Registry) ([]Group, error) {
	rows, err := s.ScoreboardGroupRows(store.ScoreFilter(f))
	if err != nil {
		return nil, err
	}
	out := make([]Group, 0, len(rows))
	for _, g := range rows {
		id := reg.Resolve(g.Engine, g.Model)
		out = append(out, Group{ScoreGroupRow: g, ModelDisplay: id.ModelDisplay, Harness: id.Harness, Access: id.Access, MedianTokens: floorTokens(g.MedianTokensF)})
	}
	return out, nil
}

// Scoreboard returns the tiered per-model rollup, identity-resolved, each
// with nested (lean) task-type breakdowns re-sorted by (-tasks, task_type).
// Identity resolution is Go-side (procedural fallback the SQL JOIN can't
// express). SQL order of the rollup is preserved.
func Scoreboard(s *store.Store, f Filter, reg Registry) ([]Row, error) {
	sf := store.ScoreFilter(f)
	models, err := s.ScoreboardModelRows(sf)
	if err != nil {
		return nil, err
	}
	groups, err := s.ScoreboardGroupRows(sf)
	if err != nil {
		return nil, err
	}
	nested := map[string][]TaskTypeRow{}
	for _, g := range groups {
		nested[g.Model] = append(nested[g.Model], TaskTypeRow{
			TaskType: g.TaskType, Tasks: g.Tasks, Attempts: g.Attempts, Passed: g.Passed, Failed: g.Failed,
			FirstTryPassRate: g.FirstTryPassRate, PassRate: g.PassRate, LastSeen: g.LastSeen,
		})
	}
	for m, tt := range nested {
		sort.SliceStable(tt, func(i, j int) bool {
			if tt[i].Tasks != tt[j].Tasks {
				return tt[i].Tasks > tt[j].Tasks // -tasks
			}
			return tt[i].TaskType < tt[j].TaskType
		})
		nested[m] = tt
	}
	out := make([]Row, 0, len(models))
	for _, m := range models {
		id := reg.Resolve(m.Engine, m.Model)
		out = append(out, Row{ScoreModelRow: m, ModelDisplay: id.ModelDisplay, Harness: id.Harness, Access: id.Access,
			MedianTokens: floorTokens(m.MedianTokensF), TaskTypes: nested[m.Model]})
	}
	return out, nil
}

// ExploreCandidates ports catalog_explore_candidates: untested, text->text,
// context_length >= 32000, in SortModels order.
func ExploreCandidates(models []catalog.Model, tested map[string]bool) []catalog.Model {
	var out []catalog.Model
	for _, m := range models {
		if tested[m.ID] || m.ContextLength < 32000 {
			continue
		}
		if !strings.Contains(m.Modality, "text->text") && m.Modality != "text" && m.Modality != "" {
			continue
		}
		out = append(out, m)
	}
	catalog.SortModels(out)
	return out
}
