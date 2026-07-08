package config

// removedKeys maps config keys that existed in the Python ringer to the
// migration hint a user needs. Matching is on the exact undecoded key path.
var removedKeys = map[string]string{
	"eval.backend":        "the [eval] backend selector is gone: eval rows now live in SQLite at <state_dir>/ringer.db",
	"eval.jsonl_path":     "runs.jsonl is no longer written; seed history with `ringer db import --jsonl <path>`",
	"eval.postgres":       "the Postgres eval sink is gone; use `ringer db export` for external aggregation",
	"dashboard_port_base": "the per-run dashboard server is gone; Ringside serves everything on :8700",
	"hud_app_path":        "the Tauri HUD app is gone; `ringer hud` serves the web dashboard",
}
