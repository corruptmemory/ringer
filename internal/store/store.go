// internal/store/store.go
package store

import (
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	sqlite "modernc.org/sqlite"
)

// JSON tags are snake_case (matching the `attempts` table's columns) rather
// than encoding/json's default PascalCase so `ringer db export`'s Go-native
// JSONL round-trips cleanly back through attemptFromJSONL's native-field
// lookups (cmd/ringer/db.go), which try these exact keys before falling back
// to legacy Python names (e.g. "engine" before "worker_engine").
type Attempt struct {
	RunID       string  `json:"run_id"`
	RunName     string  `json:"run_name"`
	TaskKey     string  `json:"task_key"`
	Engine      string  `json:"engine"`
	Model       string  `json:"model"`
	TaskType    string  `json:"task_type"`
	Verdict     string  `json:"verdict"` // PASS | FAIL | TIMEOUT | ERROR
	Retry       int     `json:"retry"`
	DurationS   float64 `json:"duration_s"`
	Tokens      int64   `json:"tokens"` // -1 = unknown
	CheckOutput string  `json:"check_output"`
	Identity    string  `json:"identity"`
	CreatedAt   string  `json:"created_at"` // UTC RFC3339
}

type Store struct{ db *sql.DB }

// Pragma discipline per the design spec §7. busy_timeout is a PRAGMA, and
// the DSN carries no _txlock (cznic issue #192).
var openPragmas = []string{
	"PRAGMA busy_timeout=5000;",
	"PRAGMA journal_mode=WAL;",
	"PRAGMA synchronous=NORMAL;",
}

func Open(path string) (*Store, error) {
	registerMedian()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, fmt.Errorf("store: %w", err)
	}
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, fmt.Errorf("store open %s: %w", path, err)
	}
	db.SetMaxOpenConns(1)
	for _, p := range openPragmas {
		p := p
		if err := withBusyRetry(func() error {
			_, err := db.Exec(p)
			return err
		}); err != nil {
			db.Close()
			return nil, fmt.Errorf("store pragma %q: %w", p, err)
		}
	}
	if _, err := db.Exec(schemaSQL); err != nil {
		db.Close()
		return nil, fmt.Errorf("store schema: %w", err)
	}
	if _, err := db.Exec(
		`INSERT INTO schema_version(version) SELECT ? WHERE NOT EXISTS (SELECT 1 FROM schema_version)`,
		schemaVersion,
	); err != nil {
		db.Close()
		return nil, fmt.Errorf("store schema_version: %w", err)
	}
	return &Store{db: db}, nil
}

func (s *Store) Close() error { return s.db.Close() }

// errCheckpointBusy is a sentinel wrapped into the error Checkpoint returns
// when `PRAGMA wal_checkpoint(TRUNCATE)` reports busy=1 in its result row.
// wal_checkpoint signals contention via that result row rather than a Go
// error/*sqlite.Error, so it can't be matched by the BUSY/LOCKED code check
// below. Extending isBusy to also recognize this sentinel lets Checkpoint
// reuse withBusyRetry's bounded backoff instead of a bespoke retry loop, so
// checkpoint contention gets the same treatment as write contention.
var errCheckpointBusy = errors.New("wal_checkpoint(TRUNCATE) busy: could not obtain checkpoint lock")

func isBusy(err error) bool {
	if errors.Is(err, errCheckpointBusy) {
		return true
	}
	var se *sqlite.Error
	if errors.As(err, &se) {
		code := se.Code() & 0xff      // primary result code; modernc stores extended codes
		return code == 5 || code == 6 // SQLITE_BUSY, SQLITE_LOCKED
	}
	return false
}

// withBusyRetry runs fn, retrying briefly on residual BUSY/LOCKED that the
// 5s busy_timeout did not absorb. Bounded: ~10 attempts over ~2.5s max.
func withBusyRetry(fn func() error) error {
	var err error
	for i := 0; i < 10; i++ {
		if err = fn(); err == nil || !isBusy(err) {
			return err
		}
		time.Sleep(time.Duration(50*(i+1)) * time.Millisecond)
	}
	return err
}

func (s *Store) InsertAttempt(a Attempt) error {
	return withBusyRetry(func() error {
		_, err := s.db.Exec(`INSERT INTO attempts
			(run_id, run_name, task_key, engine, model, task_type, verdict,
			 retry, duration_s, tokens, check_output, identity, created_at)
			VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?)`,
			a.RunID, a.RunName, a.TaskKey, a.Engine, a.Model, a.TaskType,
			a.Verdict, a.Retry, a.DurationS, a.Tokens, a.CheckOutput,
			a.Identity, a.CreatedAt)
		return err
	})
}

func (s *Store) CountAttempts() (int64, error) {
	var n int64
	err := withBusyRetry(func() error {
		return s.db.QueryRow(`SELECT COUNT(*) FROM attempts`).Scan(&n)
	})
	return n, err
}

// Checkpoint runs a TRUNCATE-mode WAL checkpoint. `PRAGMA wal_checkpoint`
// reports failure via a result row (busy, log, checkpointed), not a Go
// error — db.Exec would silently discard that row and Checkpoint would
// return nil even when nothing was truncated. Read the row explicitly and
// treat busy!=0 as a retryable failure via withBusyRetry so a run-end
// checkpoint that loses the race to a reader/writer is retried with bounded
// backoff instead of silently no-op'ing (design spec §7 / cznic issue #179).
func (s *Store) Checkpoint() error {
	return withBusyRetry(func() error {
		var busy, logFrames, checkpointed int
		if err := s.db.QueryRow(`PRAGMA wal_checkpoint(TRUNCATE);`).Scan(&busy, &logFrames, &checkpointed); err != nil {
			return err
		}
		if busy != 0 {
			return fmt.Errorf("wal_checkpoint(TRUNCATE) busy: could not obtain checkpoint lock (log=%d checkpointed=%d): %w", logFrames, checkpointed, errCheckpointBusy)
		}
		return nil
	})
}

func (s *Store) Integrity() error {
	var res string
	if err := s.db.QueryRow(`PRAGMA integrity_check;`).Scan(&res); err != nil {
		return err
	}
	if res != "ok" {
		return fmt.Errorf("integrity_check: %s", res)
	}
	return nil
}
