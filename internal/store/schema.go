// internal/store/schema.go
package store

const schemaSQL = `
CREATE TABLE IF NOT EXISTS attempts (
  id           INTEGER PRIMARY KEY AUTOINCREMENT,
  run_id       TEXT NOT NULL,
  run_name     TEXT NOT NULL DEFAULT '',
  task_key     TEXT NOT NULL,
  engine       TEXT NOT NULL DEFAULT '',
  model        TEXT NOT NULL DEFAULT '',
  task_type    TEXT NOT NULL DEFAULT '',
  verdict      TEXT NOT NULL,
  retry        INTEGER NOT NULL DEFAULT 0,
  duration_s   REAL NOT NULL DEFAULT 0,
  tokens       INTEGER NOT NULL DEFAULT -1,
  check_output TEXT NOT NULL DEFAULT '',
  identity     TEXT NOT NULL DEFAULT '',
  created_at   TEXT NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_attempts_model_tasktype ON attempts(model, task_type);
CREATE INDEX IF NOT EXISTS idx_attempts_runtask ON attempts(run_id, task_key, id);

CREATE TABLE IF NOT EXISTS catalog_models (
  id               TEXT PRIMARY KEY,
  name             TEXT NOT NULL DEFAULT '',
  context_length   INTEGER NOT NULL DEFAULT 0,
  prompt_per_m     REAL,
  completion_per_m REAL,
  free             INTEGER NOT NULL DEFAULT 0,
  variable_pricing INTEGER NOT NULL DEFAULT 0,
  pricing_unknown  INTEGER NOT NULL DEFAULT 0,
  fetched_at       TEXT NOT NULL DEFAULT '',
  modality         TEXT NOT NULL DEFAULT ''
);
CREATE TABLE IF NOT EXISTS catalog_events (
  id        INTEGER PRIMARY KEY AUTOINCREMENT,
  ts        TEXT NOT NULL DEFAULT '',
  kind      TEXT NOT NULL DEFAULT '',
  model_id  TEXT NOT NULL DEFAULT '',
  payload   TEXT NOT NULL DEFAULT ''
);
CREATE INDEX IF NOT EXISTS idx_catalog_events_ts ON catalog_events(ts);

CREATE TABLE IF NOT EXISTS schema_version (version INTEGER NOT NULL);
`

const schemaVersion = 2
