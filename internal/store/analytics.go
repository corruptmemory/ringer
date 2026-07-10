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

type ScoreFilter struct{ TaskType, Model, Engine, Since string }

type ScoreModelRow struct {
	Model, Engine, Tier            string
	Tasks, Attempts, Retries       int
	Passed, Failed                 int
	FirstTryPassRate, PassRate     float64
	MedianDurationS, MedianTokensF *float64
	LastSeen                       string
	Cost                           *float64
}

// ScoreGroupRow is a rich per-(model, task_type) row: medians + the latest
// attempt's engine (for identity). Feeds the `models` table and HUD groups;
// the rollup's lean nested breakdown is a display subset of these.
type ScoreGroupRow struct {
	Model, TaskType, Engine        string
	Tasks, Attempts                int
	Passed, Failed                 int
	FirstTryPassRate, PassRate     float64
	MedianDurationS, MedianTokensF *float64
	LastSeen                       string
}

// scoreCTE resolves task-instances and applies all filters. Named params
// (:engine/:model/:task_type/:since) are bound positionally per query below.
const scoreCTE = `
WITH filtered AS (
  SELECT * FROM attempts WHERE (? = '' OR engine = ?)
),
inst AS (
  SELECT run_id, task_key, MIN(id) AS first_id, MAX(id) AS final_id, COUNT(*) AS n
  FROM filtered GROUP BY run_id, task_key
),
labeled AS (
  SELECT i.run_id, i.task_key, i.n AS attempts_in_task,
    CASE WHEN TRIM(ff.model) <> '' THEN TRIM(ff.model) ELSE TRIM(ff.engine) END AS model,
    ff.engine AS engine,
    CASE WHEN TRIM(ff.task_type) <> '' THEN TRIM(ff.task_type) ELSE '(untyped)' END AS task_type,
    UPPER(ff.verdict) AS final_verdict, ff.duration_s AS final_duration_s,
    ff.created_at AS final_created_at, UPPER(fs.verdict) AS first_verdict
  FROM inst i
  JOIN filtered ff ON ff.id = i.final_id
  JOIN filtered fs ON fs.id = i.first_id
),
sel AS (
  SELECT * FROM labeled
  WHERE (? = '' OR model = ?) AND (? = '' OR task_type = ?) AND (? = '' OR final_created_at >= ?)
)`

// bindCTE returns the 8 positional args the scoreCTE placeholders consume.
func (f ScoreFilter) bindCTE() []any {
	return []any{f.Engine, f.Engine, f.Model, f.Model, f.TaskType, f.TaskType, f.Since, f.Since}
}

func (s *Store) ScoreboardModelRows(f ScoreFilter) ([]ScoreModelRow, error) {
	q := scoreCTE + `,
tok AS (
  SELECT s.model AS model, median(a.tokens) AS median_tokens
  FROM sel s JOIN filtered a ON a.run_id = s.run_id AND a.task_key = s.task_key
  WHERE a.tokens >= 0 GROUP BY s.model
),
latest AS (
  SELECT model, engine FROM (
    SELECT model, engine, ROW_NUMBER() OVER (PARTITION BY model ORDER BY final_created_at DESC) AS rn FROM sel
  ) WHERE rn = 1
)
SELECT s.model, latest.engine,
  CASE WHEN COUNT(*) >= 3 THEN 'proven' ELSE 'probation' END AS tier,
  COUNT(*) AS tasks, SUM(s.attempts_in_task) AS attempts, SUM(s.attempts_in_task) - COUNT(*) AS retries,
  SUM(CASE WHEN s.final_verdict = 'PASS' THEN 1 ELSE 0 END) AS passed,
  SUM(CASE WHEN s.final_verdict <> 'PASS' THEN 1 ELSE 0 END) AS failed,
  1.0 * SUM(CASE WHEN s.first_verdict = 'PASS' THEN 1 ELSE 0 END) / COUNT(*) AS first_rate,
  1.0 * SUM(CASE WHEN s.final_verdict = 'PASS' THEN 1 ELSE 0 END) / COUNT(*) AS pass_rate,
  median(s.final_duration_s) AS median_duration_s, tok.median_tokens, MAX(s.final_created_at) AS last_seen,
  CASE
    WHEN tok.median_tokens IS NULL OR cm.id IS NULL OR cm.variable_pricing = 1 THEN NULL
    WHEN cm.free = 1 THEN 0.0
    ELSE tok.median_tokens * ((COALESCE(cm.prompt_per_m,0) + COALESCE(cm.completion_per_m,0)) / 2.0) / 1000000.0
  END AS cost
FROM sel s
LEFT JOIN tok ON tok.model = s.model
LEFT JOIN latest ON latest.model = s.model
LEFT JOIN catalog_models cm ON cm.id = s.model
GROUP BY s.model
ORDER BY CASE tier WHEN 'proven' THEN 0 WHEN 'probation' THEN 1 ELSE 3 END,
  first_rate DESC, pass_rate DESC,
  CASE WHEN cost IS NOT NULL THEN cost WHEN tok.median_tokens IS NULL THEN 0.0 ELSE 9e999 END,
  s.model`
	var out []ScoreModelRow
	err := withBusyRetry(func() error {
		out = out[:0]
		rows, err := s.db.Query(q, f.bindCTE()...)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var r ScoreModelRow
			var engine *string
			if err := rows.Scan(&r.Model, &engine, &r.Tier, &r.Tasks, &r.Attempts, &r.Retries,
				&r.Passed, &r.Failed, &r.FirstTryPassRate, &r.PassRate,
				&r.MedianDurationS, &r.MedianTokensF, &r.LastSeen, &r.Cost); err != nil {
				return err
			}
			if engine != nil {
				r.Engine = *engine
			}
			out = append(out, r)
		}
		return rows.Err()
	})
	return out, err
}

func (s *Store) ScoreboardGroupRows(f ScoreFilter) ([]ScoreGroupRow, error) {
	q := scoreCTE + `,
tok AS (
  SELECT s.model AS model, s.task_type AS task_type, median(a.tokens) AS median_tokens
  FROM sel s JOIN filtered a ON a.run_id = s.run_id AND a.task_key = s.task_key
  WHERE a.tokens >= 0 GROUP BY s.model, s.task_type
),
latest AS (
  SELECT model, task_type, engine FROM (
    SELECT model, task_type, engine, ROW_NUMBER() OVER (PARTITION BY model, task_type ORDER BY final_created_at DESC) AS rn FROM sel
  ) WHERE rn = 1
)
SELECT s.model, s.task_type, latest.engine,
  COUNT(*) AS tasks, SUM(s.attempts_in_task) AS attempts,
  SUM(CASE WHEN s.final_verdict = 'PASS' THEN 1 ELSE 0 END) AS passed,
  SUM(CASE WHEN s.final_verdict <> 'PASS' THEN 1 ELSE 0 END) AS failed,
  1.0 * SUM(CASE WHEN s.first_verdict = 'PASS' THEN 1 ELSE 0 END) / COUNT(*) AS first_rate,
  1.0 * SUM(CASE WHEN s.final_verdict = 'PASS' THEN 1 ELSE 0 END) / COUNT(*) AS pass_rate,
  median(s.final_duration_s) AS median_duration_s, tok.median_tokens, MAX(s.final_created_at) AS last_seen
FROM sel s
LEFT JOIN tok ON tok.model = s.model AND tok.task_type = s.task_type
LEFT JOIN latest ON latest.model = s.model AND latest.task_type = s.task_type
GROUP BY s.model, s.task_type
ORDER BY s.task_type, pass_rate DESC, first_rate DESC, s.model`
	var out []ScoreGroupRow
	err := withBusyRetry(func() error {
		out = out[:0]
		rows, err := s.db.Query(q, f.bindCTE()...)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var r ScoreGroupRow
			var engine *string
			if err := rows.Scan(&r.Model, &r.TaskType, &engine, &r.Tasks, &r.Attempts, &r.Passed, &r.Failed,
				&r.FirstTryPassRate, &r.PassRate, &r.MedianDurationS, &r.MedianTokensF, &r.LastSeen); err != nil {
				return err
			}
			if engine != nil {
				r.Engine = *engine
			}
			out = append(out, r)
		}
		return rows.Err()
	})
	return out, err
}
