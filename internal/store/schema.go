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
CREATE TABLE IF NOT EXISTS schema_version (version INTEGER NOT NULL);
`

const schemaVersion = 1
