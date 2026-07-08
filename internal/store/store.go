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

type Attempt struct {
	RunID       string
	RunName     string
	TaskKey     string
	Engine      string
	Model       string
	TaskType    string
	Verdict     string // PASS | FAIL | TIMEOUT | ERROR
	Retry       int
	DurationS   float64
	Tokens      int64 // -1 = unknown
	CheckOutput string
	Identity    string
	CreatedAt   string // UTC RFC3339
}

type Store struct{ db *sql.DB }

// Pragma discipline per the design spec §7. busy_timeout is a PRAGMA, and
// the DSN carries no _txlock (cznic issue #192).
var openPragmas = []string{
	"PRAGMA journal_mode=WAL;",
	"PRAGMA busy_timeout=5000;",
	"PRAGMA synchronous=NORMAL;",
}

func Open(path string) (*Store, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, fmt.Errorf("store: %w", err)
	}
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, fmt.Errorf("store open %s: %w", path, err)
	}
	db.SetMaxOpenConns(1)
	for _, p := range openPragmas {
		if _, err := db.Exec(p); err != nil {
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

func isBusy(err error) bool {
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

func (s *Store) Checkpoint() error {
	_, err := s.db.Exec(`PRAGMA wal_checkpoint(TRUNCATE);`)
	return err
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
