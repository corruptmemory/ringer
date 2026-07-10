// cmd/ringer/db.go
package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/corruptmemory/ringer/internal/store"
)

type dbExportCmd struct {
	Out string `long:"out" description:"output JSONL path (default stdout)"`
}
type dbImportCmd struct {
	JSONL   string `long:"jsonl" required:"yes" description:"legacy eval-log JSONL to import"`
	RunsDir string `long:"runs-dir" description:"run-state dir for model backfill (default <state_dir>/runs)"`
	Mapping string `long:"mapping" description:"task_type mapping JSON"`
	DryRun  bool   `long:"dry-run"`
}
type dbIntegrityCmd struct{}
type dbCheckpointCmd struct{}

func (c *dbExportCmd) Execute(args []string) error {
	cfg, err := loadConfig()
	if err != nil {
		return err
	}
	s, err := store.Open(cfg.DBPath())
	if err != nil {
		return err
	}
	defer s.Close()
	rows, err := s.AllAttempts()
	if err != nil {
		return err
	}
	w := os.Stdout
	if c.Out != "" {
		f, err := os.Create(c.Out)
		if err != nil {
			return err
		}
		defer f.Close()
		w = f
	}
	enc := json.NewEncoder(w)
	for _, a := range rows {
		if err := enc.Encode(a); err != nil {
			return err
		}
	}
	return nil
}

func (c *dbImportCmd) Execute(args []string) error {
	cfg, err := loadConfig()
	if err != nil {
		return err
	}
	runsDir := c.RunsDir
	if runsDir == "" {
		runsDir = filepath.Join(cfg.StateDirPath(), "runs")
	}
	mapping := map[string]string{}
	if c.Mapping != "" {
		b, err := os.ReadFile(c.Mapping)
		if err != nil {
			return err
		}
		if err := json.Unmarshal(b, &mapping); err != nil {
			return fmt.Errorf("mapping: %w", err)
		}
	}
	f, err := os.Open(c.JSONL)
	if err != nil {
		return err
	}
	defer f.Close()

	runModel := runStateModelLookup(runsDir)
	var s *store.Store
	if !c.DryRun {
		s, err = store.Open(cfg.DBPath())
		if err != nil {
			return err
		}
		defer s.Close()
	}
	var imported, skipped int
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 1024*1024), 8*1024*1024)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" {
			continue
		}
		var row map[string]any
		if err := json.Unmarshal([]byte(line), &row); err != nil {
			skipped++
			continue
		}
		a := attemptFromJSONL(row, runModel, mapping)
		if a.RunID == "" || a.TaskKey == "" || a.Verdict == "" {
			skipped++
			continue
		}
		if !c.DryRun {
			if err := s.InsertAttempt(a); err != nil {
				return err
			}
		}
		imported++
	}
	if err := sc.Err(); err != nil {
		return err
	}
	fmt.Printf("db import: %d imported, %d skipped%s\n", imported, skipped, dryRunSuffix(c.DryRun))
	return nil
}

func dryRunSuffix(d bool) string {
	if d {
		return " (dry-run, nothing written)"
	}
	return ""
}

func (c *dbIntegrityCmd) Execute(args []string) error {
	return withStore(func(s *store.Store) error { return s.Integrity() })
}
func (c *dbCheckpointCmd) Execute(args []string) error {
	return withStore(func(s *store.Store) error { return s.Checkpoint() })
}

func withStore(fn func(*store.Store) error) error {
	cfg, err := loadConfig()
	if err != nil {
		return err
	}
	s, err := store.Open(cfg.DBPath())
	if err != nil {
		return err
	}
	defer s.Close()
	return fn(s)
}

// attemptFromJSONL maps a legacy/native JSONL row to a store.Attempt,
// tolerant of Python field names, applying the frozen backfill precedence.
func attemptFromJSONL(row map[string]any, runModel func(runID, taskKey string) string, mapping map[string]string) store.Attempt {
	get := func(keys ...string) string {
		for _, k := range keys {
			if v, ok := row[k]; ok && v != nil {
				if s := strings.TrimSpace(fmt.Sprintf("%v", v)); s != "" {
					return s
				}
			}
		}
		return ""
	}
	runID := get("run_id")
	taskKey := get("task_key")
	model := get("model")
	if model == "" {
		model = runModel(runID, taskKey) // backfill from run-state
	}
	taskType := get("task_type")
	if taskType == "" {
		taskType = taskTypeFromMapping(mapping, runID, taskKey)
	}
	return store.Attempt{
		RunID: runID, RunName: get("run_name"), TaskKey: taskKey,
		Engine: get("engine", "worker_engine"), Model: model, TaskType: taskType,
		Verdict: strings.ToUpper(get("verdict")), Retry: retryFrom(row),
		DurationS: durationSeconds(row), Tokens: tokensFrom(row),
		CheckOutput: get("check_output", "notes"), Identity: get("identity", "orchestrator"),
		CreatedAt: firstNonEmpty(get("created_at"), get("logged_at")),
	}
}

// taskTypeFromMapping ports the frozen precedence: "<run_id>:<task_key>" >
// "<run_id>" > longest "name:<prefix>" (ringer/scripts/backfill_model_log.py:94-123).
func taskTypeFromMapping(mapping map[string]string, runID, taskKey string) string {
	if runID == "" {
		return ""
	}
	if taskKey != "" {
		if v := mapping[runID+":"+taskKey]; v != "" {
			return v
		}
	}
	if v := mapping[runID]; v != "" {
		return v
	}
	// Frozen tier-3 contract (scripts/backfill_model_log.py:109-123): the
	// longest matching "name:<prefix>" wins the max search by LENGTH ALONE
	// (value-agnostic), and the winner's value is truthiness-gated only AFTER
	// the loop. So a longer prefix with an empty value wins the contest and
	// voids the tier — it must NOT fall back to a shorter non-empty prefix.
	best, bestLen := "", -1
	for k, v := range mapping {
		if !strings.HasPrefix(k, "name:") {
			continue
		}
		prefix := k[len("name:"):]
		if prefix == "" || !strings.HasPrefix(runID, prefix) {
			continue
		}
		if len(prefix) > bestLen {
			bestLen = len(prefix)
			best = v
		}
	}
	if best != "" {
		return best
	}
	return ""
}

// jsonNumber coerces a decoded JSON scalar (float64 from encoding/json, or a
// numeric string) to float64. ok is false for anything else (nil, bool,
// non-numeric string), so callers can tell "absent/unusable" from "0".
func jsonNumber(v any) (float64, bool) {
	switch t := v.(type) {
	case float64:
		return t, true
	case string:
		s := strings.TrimSpace(t)
		if s == "" {
			return 0, false
		}
		f, err := strconv.ParseFloat(s, 64)
		if err != nil {
			return 0, false
		}
		return f, true
	default:
		return 0, false
	}
}

// retryFrom ports model_log_row_is_retry (ringer.py:4501-4508) as an int
// count rather than a bool, since store.Attempt.Retry tracks retry count:
// an int/numeric "retry" > 0 is taken as that many retries, a bool/"true"
// "retry" is 1, "retry=true" inside notes (the legacy fallback marker) is 1,
// anything else is 0.
func retryFrom(row map[string]any) int {
	if v, ok := row["retry"]; ok && v != nil {
		switch t := v.(type) {
		case bool:
			if t {
				return 1
			}
			return 0
		case string:
			switch strings.ToLower(strings.TrimSpace(t)) {
			case "true":
				return 1
			case "false":
				return 0
			}
		}
		if f, ok := jsonNumber(v); ok {
			if f > 0 {
				return int(f)
			}
			return 0
		}
	}
	if notes, ok := row["notes"].(string); ok && strings.Contains(notes, "retry=true") {
		return 1
	}
	return 0
}

// durationSeconds prefers the Go-native "duration_s" field, else converts
// the legacy Python "duration_ms" field.
func durationSeconds(row map[string]any) float64 {
	if f, ok := jsonNumber(row["duration_s"]); ok {
		return f
	}
	if f, ok := jsonNumber(row["duration_ms"]); ok {
		return f / 1000.0
	}
	return 0
}

// tokensFrom prefers the Go-native "tokens" field, else the legacy Python
// "worker_tokens" field, else -1 (store.Attempt's "unknown" sentinel).
func tokensFrom(row map[string]any) int64 {
	if f, ok := jsonNumber(row["tokens"]); ok {
		return int64(f)
	}
	if f, ok := jsonNumber(row["worker_tokens"]); ok {
		return int64(f)
	}
	return -1
}

// firstNonEmpty returns the first non-empty string among vals.
func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if v != "" {
			return v
		}
	}
	return ""
}

// runStateEntry is the subset of a Ringer run-state file's `tasks[]` entries
// needed for model backfill.
type runStateEntry struct {
	Key   string `json:"key"`
	Model string `json:"model"`
}
type runStateFile struct {
	Tasks []runStateEntry `json:"tasks"`
}

// runStateModelLookup returns a runID/taskKey -> model lookup function that
// reads <runsDir>/<run_id>.json on first use per run_id (caching the parsed
// tasks, or the absence of a usable file, across the whole import) and
// matches tasks[].key==task_key -> task.model, porting
// model_from_run_state (ringer.py:4515-4519 via scripts/backfill_model_log.py:77-91).
func runStateModelLookup(runsDir string) func(runID, taskKey string) string {
	cache := map[string][]runStateEntry{}
	return func(runID, taskKey string) string {
		if runID == "" || taskKey == "" {
			return ""
		}
		tasks, ok := cache[runID]
		if !ok {
			tasks = loadRunStateTasks(runsDir, runID)
			cache[runID] = tasks
		}
		for _, t := range tasks {
			if t.Key == taskKey {
				return t.Model
			}
		}
		return ""
	}
}

func loadRunStateTasks(runsDir, runID string) []runStateEntry {
	b, err := os.ReadFile(filepath.Join(runsDir, runID+".json"))
	if err != nil {
		return nil
	}
	var rs runStateFile
	if err := json.Unmarshal(b, &rs); err != nil {
		return nil
	}
	return rs.Tasks
}

func init() {
	db, err := parser.AddCommand("db", "Eval-store maintenance", "Export/import/integrity/checkpoint the SQLite eval store.", &struct{}{})
	if err != nil {
		panic(err)
	}
	db.AddCommand("export", "Export attempts to JSONL", "", &dbExportCmd{})
	db.AddCommand("import", "Import legacy JSONL into SQLite", "", &dbImportCmd{})
	db.AddCommand("integrity", "PRAGMA integrity_check", "", &dbIntegrityCmd{})
	db.AddCommand("checkpoint", "wal_checkpoint(TRUNCATE)", "", &dbCheckpointCmd{})
}
