// internal/store/analytics.go
package store

import (
	"encoding/json"

	"github.com/corruptmemory/ringer/internal/catalog"
)

func (s *Store) CatalogModels() ([]catalog.Model, error) { return s.queryCatalog("") }
func (s *Store) FreeCatalogModels() ([]catalog.Model, error) {
	return s.queryCatalog("WHERE free=1")
}

func (s *Store) queryCatalog(where string) ([]catalog.Model, error) {
	var out []catalog.Model
	err := withBusyRetry(func() error {
		out = out[:0]
		rows, err := s.db.Query(`SELECT id,name,context_length,prompt_per_m,completion_per_m,free,variable_pricing,pricing_unknown,fetched_at,modality FROM catalog_models ` + where)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var m catalog.Model
			var free, variable, unknown int
			var prompt, completion *float64
			if err := rows.Scan(&m.ID, &m.Name, &m.ContextLength, &prompt, &completion, &free, &variable, &unknown, &m.FetchedAt, &m.Modality); err != nil {
				return err
			}
			m.PromptPerM, m.CompletionPerM = prompt, completion
			m.Free, m.VariablePricing, m.PricingUnknown = free != 0, variable != 0, unknown != 0
			out = append(out, m)
		}
		return rows.Err()
	})
	if err != nil {
		return nil, err
	}
	catalog.SortModels(out)
	return out, nil
}

func (s *Store) ReplaceCatalog(models []catalog.Model) error {
	return withBusyRetry(func() error {
		tx, err := s.db.Begin()
		if err != nil {
			return err
		}
		defer tx.Rollback()
		if _, err := tx.Exec(`DELETE FROM catalog_models`); err != nil {
			return err
		}
		for _, m := range models {
			if m.ID == "" {
				continue
			}
			if _, err := tx.Exec(`INSERT INTO catalog_models(id,name,context_length,prompt_per_m,completion_per_m,free,variable_pricing,pricing_unknown,fetched_at,modality) VALUES (?,?,?,?,?,?,?,?,?,?)`,
				m.ID, m.Name, m.ContextLength, m.PromptPerM, m.CompletionPerM,
				b2i(m.Free), b2i(m.VariablePricing), b2i(m.PricingUnknown), m.FetchedAt, m.Modality); err != nil {
				return err
			}
		}
		return tx.Commit()
	})
}

func (s *Store) AppendCatalogEvents(events []catalog.Event) error {
	if len(events) == 0 {
		return nil
	}
	return withBusyRetry(func() error {
		tx, err := s.db.Begin()
		if err != nil {
			return err
		}
		defer tx.Rollback()
		for _, e := range events {
			payload, _ := json.Marshal(e.Payload)
			if _, err := tx.Exec(`INSERT INTO catalog_events(ts,kind,model_id,payload) VALUES (?,?,?,?)`,
				e.TS, e.Kind, e.ModelID, string(payload)); err != nil {
				return err
			}
		}
		return tx.Commit()
	})
}

func (s *Store) CatalogEvents(limit int) ([]catalog.Event, error) {
	var out []catalog.Event
	err := withBusyRetry(func() error {
		out = out[:0]
		rows, err := s.db.Query(`SELECT ts,kind,model_id,payload FROM catalog_events ORDER BY id DESC LIMIT ?`, limit)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var e catalog.Event
			var payload string
			if err := rows.Scan(&e.TS, &e.Kind, &e.ModelID, &payload); err != nil {
				return err
			}
			_ = json.Unmarshal([]byte(payload), &e.Payload)
			out = append(out, e)
		}
		return rows.Err()
	})
	return out, err
}

func b2i(b bool) int {
	if b {
		return 1
	}
	return 0
}
