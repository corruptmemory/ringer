# Ringer Go Rewrite — Plan 1: Foundation & Spikes

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Stand up the Go module (scaffold, strict config, vendored jail, SQLite store core) and retire the three design risks (multi-process SQLite, opencode-as-ns-root, worktrees×jail) with committed, re-runnable evidence.

**Architecture:** Single static binary `ringer` (go-flags subcommands). This plan builds the load-bearing foundation packages plus the permanent multi-process store smoke test; two jail spikes are `-tags=spike` tests whose findings are recorded in a findings doc that gates Plan 2 decisions.

**Tech Stack:** Go 1.26, go-flags, BurntSushi/toml, modernc.org/sqlite, golang.org/x/sys. (chi/templ arrive in later plans.)

**Spec:** `docs/superpowers/specs/2026-07-08-ringer-go-rewrite-design.md` — its §9 frozen contracts and §7 pragma discipline are binding.

## Global Constraints

- Module path: `github.com/corruptmemory/ringer`; `go 1.26` in go.mod.
- Every build/test invocation goes through `./build.sh` — never raw `go build` / `go test` (repo convention).
- `CGO_ENABLED=0` always; the binary must be fully static.
- Allowed third-party deps in this plan: `github.com/jessevdk/go-flags`, `github.com/BurntSushi/toml`, `modernc.org/sqlite` (+ its transitive `modernc.org/libc`), `golang.org/x/sys`. Nothing else.
- Never upgrade `modernc.org/libc` independently of `modernc.org/sqlite` (no `go get -u modernc.org/libc`); MVS must resolve it from modernc's own go.mod.
- SQLite DSN must never contain `_txlock=immediate` (cznic issue #192). Pragmas are set by statement after open.
- Format with `gofmt` (build.sh enforces via `gofmt -l`).
- Tests: stdlib `testing`, table-driven or sequential, `t.TempDir()` for isolation. No testify.
- Commit after every green task; work happens on the `go-rewrite` branch (already checked out).
- `internal/jail` is vendored from `/home/jim/projects/flywheel/jail/` — do not "improve" it in this plan; provenance header + import-path fix only.

---

### Task 1: Module scaffold, build.sh, `version` subcommand

**Files:**
- Create: `go.mod`, `cmd/ringer/main.go`, `cmd/ringer/version.go`, `build.sh`
- Modify: `.gitignore` (add `/ringer` binary)
- Test: `cmd/ringer/version_test.go`

**Interfaces:**
- Consumes: nothing (first task).
- Produces: `parser *flags.Parser` in package `main` with global `--config` (later tasks add commands via `parser.AddCommand`); `Version() string`; `./build.sh [--test [--race]]` as the only build entry point.

- [ ] **Step 1: Write the failing test**

```go
// cmd/ringer/version_test.go
package main

import (
	"strings"
	"testing"
)

func TestVersionString(t *testing.T) {
	v := Version()
	if !strings.HasPrefix(v, "ringer ") {
		t.Fatalf("Version() = %q, want prefix %q", v, "ringer ")
	}
}
```

- [ ] **Step 2: Create go.mod and run the test to verify it fails**

```bash
cd /home/jim/projects/ringer
go mod init github.com/corruptmemory/ringer
go mod edit -go=1.26
```

Run: `go test ./cmd/ringer/` (build.sh doesn't exist yet — this is the one permitted raw invocation, per bootstrap)
Expected: FAIL — `undefined: Version`

- [ ] **Step 3: Write main.go, version.go, build.sh**

```go
// cmd/ringer/main.go
package main

import (
	"os"

	"github.com/jessevdk/go-flags"
)

type rootOptions struct {
	Config string `long:"config" description:"Path to config TOML (default: $RINGER_CONFIG or ~/.config/ringer/config.toml)"`
}

var opts rootOptions
var parser = flags.NewParser(&opts, flags.Default)

func main() {
	if _, err := parser.Parse(); err != nil {
		if flags.WroteHelp(err) {
			os.Exit(0)
		}
		os.Exit(1)
	}
}
```

```go
// cmd/ringer/version.go
package main

import "fmt"

const version = "0.1.0-dev"

func Version() string { return "ringer " + version }

type versionCommand struct{}

func (c *versionCommand) Execute(args []string) error {
	fmt.Println(Version())
	return nil
}

func init() {
	parser.AddCommand("version", "Print version", "Print the ringer version string.", &versionCommand{})
}
```

```bash
#!/usr/bin/env bash
# build.sh — the ONLY entry point for building and testing ringer.
set -euo pipefail
cd "$(dirname "$0")"

RACE=""
RUN_TESTS=0
for arg in "$@"; do
  case "$arg" in
    --test) RUN_TESTS=1 ;;
    --race) RACE="-race" ;;
    *) echo "usage: ./build.sh [--test [--race]]" >&2; exit 2 ;;
  esac
done

UNFORMATTED=$(gofmt -l cmd internal 2>/dev/null || true)
if [ -n "$UNFORMATTED" ]; then
  echo "gofmt needed on:" >&2; echo "$UNFORMATTED" >&2; exit 1
fi

go vet ./...
CGO_ENABLED=0 go build -o ringer ./cmd/ringer

if [ "$RUN_TESTS" = "1" ]; then
  # -race implies cgo-capable toolchain for the test binary only; the shipped
  # binary above is always CGO_ENABLED=0.
  go test $RACE ./...
fi
```

```bash
chmod +x build.sh
go get github.com/jessevdk/go-flags@v1.6.1
printf '\n/ringer\n' >> .gitignore
```

Note: `internal/` doesn't exist yet — `gofmt -l cmd internal` tolerates that via `|| true`.

- [ ] **Step 4: Run tests and the binary to verify pass**

Run: `./build.sh --test && ./ringer version`
Expected: tests pass; prints `ringer 0.1.0-dev`

- [ ] **Step 5: Commit**

```bash
git add go.mod go.sum cmd/ build.sh .gitignore
git commit -m "feat: Go module scaffold, build.sh, version subcommand"
```

---

### Task 2: Strict config loader (frozen TOML schema, loud removed-key errors)

**Files:**
- Create: `internal/config/config.go`, `internal/config/removed.go`
- Test: `internal/config/config_test.go`

**Interfaces:**
- Consumes: nothing.
- Produces:

```go
package config

type EngineConfig struct {
	Bin            string   `toml:"bin"`
	ArgsTemplate   []string `toml:"args_template"`
	SandboxArgs    []string `toml:"sandbox_args"`
	FullAccessArgs []string `toml:"full_access_args"`
	TokenRegex     string   `toml:"token_regex"`
	ModelDefault   string   `toml:"model_default"`
	Isolation      string   `toml:"isolation"`        // "", "none", "jail"
	JailStateDirs  []string `toml:"jail_state_dirs"`
}

type ArtifactConfig struct {
	Enabled   bool   `toml:"enabled"`
	Out       string `toml:"out"`
	ReportOut string `toml:"report_out"`
	IndexOut  string `toml:"index_out"`
}

type EvalConfig struct {
	DBPath string `toml:"db_path"` // empty -> <state_dir>/ringer.db
}

type AppConfig struct {
	IdentityDefault string                  `toml:"identity_default"`
	StateDir        string                  `toml:"state_dir"` // empty -> ~/.ringer
	AllowFullAccess bool                    `toml:"allow_full_access"`
	Eval            EvalConfig              `toml:"eval"`
	Artifact        ArtifactConfig          `toml:"artifact"`
	Engines         map[string]EngineConfig `toml:"engines"`
}

func Load(path string) (*AppConfig, error)   // strict: unknown keys are errors
func DefaultPath() string                     // $RINGER_CONFIG > ~/.config/ringer/config.toml
func (c *AppConfig) StateDirPath() string     // expanded state dir
func (c *AppConfig) DBPath() string           // eval.db_path or <state_dir>/ringer.db
```

`Load` on a missing file returns defaults (built-in codex engine added by the caller in Plan 2 — NOT here; YAGNI).

- [ ] **Step 1: Write the failing tests**

```go
// internal/config/config_test.go
package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeConfig(t *testing.T, body string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestLoadValid(t *testing.T) {
	p := writeConfig(t, `
identity_default = "desk"
state_dir = "/tmp/ringer-test-state"
allow_full_access = true

[eval]
db_path = "/tmp/alt.db"

[engines.opencode]
bin = "opencode"
args_template = ["run", "{spec}"]
isolation = "jail"
jail_state_dirs = ["~/.config/opencode"]
`)
	c, err := Load(p)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if c.IdentityDefault != "desk" || !c.AllowFullAccess {
		t.Errorf("top-level fields wrong: %+v", c)
	}
	e, ok := c.Engines["opencode"]
	if !ok || e.Isolation != "jail" || len(e.JailStateDirs) != 1 {
		t.Errorf("engine block wrong: %+v", e)
	}
	if c.DBPath() != "/tmp/alt.db" {
		t.Errorf("DBPath() = %q", c.DBPath())
	}
}

func TestLoadMissingFileGivesDefaults(t *testing.T) {
	c, err := Load(filepath.Join(t.TempDir(), "nope.toml"))
	if err != nil {
		t.Fatalf("missing file must yield defaults, got %v", err)
	}
	home, _ := os.UserHomeDir()
	if c.StateDirPath() != filepath.Join(home, ".ringer") {
		t.Errorf("StateDirPath() = %q", c.StateDirPath())
	}
	if c.DBPath() != filepath.Join(home, ".ringer", "ringer.db") {
		t.Errorf("DBPath() = %q", c.DBPath())
	}
}

func TestRemovedKeysFailLoudly(t *testing.T) {
	cases := []struct{ name, body, wantHint string }{
		{"eval.backend", "[eval]\nbackend = \"jsonl\"", "SQLite"},
		{"eval.postgres", "[eval.postgres]\nenv_file = \"x\"", "db export"},
		{"jsonl_path", "[eval]\njsonl_path = \"/tmp/x.jsonl\"", "db import"},
		{"dashboard_port_base", "dashboard_port_base = 8787", "8700"},
		{"hud_app_path", "hud_app_path = \"/Applications/Ringside.app\"", "hud"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := Load(writeConfig(t, tc.body))
			if err == nil {
				t.Fatal("want error for removed key, got nil")
			}
			if !strings.Contains(err.Error(), tc.wantHint) {
				t.Errorf("error %q lacks migration hint %q", err, tc.wantHint)
			}
		})
	}
}

func TestUnknownKeyFailsLoudly(t *testing.T) {
	_, err := Load(writeConfig(t, "identity_defualt = \"typo\"\n"))
	if err == nil || !strings.Contains(err.Error(), "identity_defualt") {
		t.Fatalf("typo key must be a load error naming the key, got %v", err)
	}
}

func TestInvalidIsolationRejected(t *testing.T) {
	_, err := Load(writeConfig(t, "[engines.x]\nbin = \"x\"\nisolation = \"bwrap\"\n"))
	if err == nil || !strings.Contains(err.Error(), "isolation") {
		t.Fatalf("want isolation validation error, got %v", err)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `./build.sh --test`
Expected: FAIL — package `internal/config` does not exist / undefined symbols.

- [ ] **Step 3: Implement**

```go
// internal/config/removed.go
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
```

```go
// internal/config/config.go
package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/BurntSushi/toml"
)

// (struct definitions exactly as in the Interfaces block above)

func DefaultPath() string {
	if p := os.Getenv("RINGER_CONFIG"); p != "" {
		return p
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "config.toml"
	}
	return filepath.Join(home, ".config", "ringer", "config.toml")
}

func expandHome(p string) string {
	if p == "~" || strings.HasPrefix(p, "~/") {
		if home, err := os.UserHomeDir(); err == nil {
			return filepath.Join(home, strings.TrimPrefix(strings.TrimPrefix(p, "~"), "/"))
		}
	}
	return p
}

func (c *AppConfig) StateDirPath() string {
	if c.StateDir != "" {
		return expandHome(c.StateDir)
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ".ringer"
	}
	return filepath.Join(home, ".ringer")
}

func (c *AppConfig) DBPath() string {
	if c.Eval.DBPath != "" {
		return expandHome(c.Eval.DBPath)
	}
	return filepath.Join(c.StateDirPath(), "ringer.db")
}

func Load(path string) (*AppConfig, error) {
	var c AppConfig
	md, err := toml.DecodeFile(path, &c)
	if err != nil {
		if os.IsNotExist(err) {
			return &AppConfig{}, nil // sane defaults without a config file
		}
		return nil, fmt.Errorf("config %s: %w", path, err)
	}
	for _, k := range md.Undecoded() {
		key := k.String()
		for removed, hint := range removedKeys {
			if key == removed || strings.HasPrefix(key, removed+".") {
				return nil, fmt.Errorf("config %s: key %q was removed in the Go rewrite — %s", path, key, hint)
			}
		}
		return nil, fmt.Errorf("config %s: unknown key %q (typo? removed?)", path, key)
	}
	for name, e := range c.Engines {
		switch e.Isolation {
		case "", "none", "jail":
		default:
			return nil, fmt.Errorf("config %s: engines.%s.isolation must be \"none\" or \"jail\", got %q", path, name, e.Isolation)
		}
	}
	return &c, nil
}
```

```bash
go get github.com/BurntSushi/toml@v1.5.0
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `./build.sh --test`
Expected: PASS (all `internal/config` tests green).

- [ ] **Step 5: Commit**

```bash
git add internal/config go.mod go.sum
git commit -m "feat: strict config loader with loud removed-key migration errors"
```

---

### Task 3: Identity resolution chain

**Files:**
- Create: `internal/config/identity.go`
- Test: `internal/config/identity_test.go`

**Interfaces:**
- Consumes: `AppConfig.IdentityDefault` (Task 2).
- Produces:

```go
// ResolveIdentity implements the frozen resolution order:
// explicit flag > FLEET_IDENTITY > RINGER_IDENTITY > .fleet-agent file
// (walking up from startDir) > cfg.IdentityDefault > short hostname.
func ResolveIdentity(flagValue string, cfg *AppConfig, startDir string) string
```

- [ ] **Step 1: Write the failing tests**

```go
// internal/config/identity_test.go
package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestResolveIdentityOrder(t *testing.T) {
	// Directory tree: root/.fleet-agent contains "repo-bot"; work in root/sub/deep.
	root := t.TempDir()
	deep := filepath.Join(root, "sub", "deep")
	if err := os.MkdirAll(deep, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, ".fleet-agent"), []byte("repo-bot\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg := &AppConfig{IdentityDefault: "cfg-default"}

	cases := []struct {
		name, flag, fleetEnv, ringerEnv, startDir string
		cfg                                       *AppConfig
		want                                      string
	}{
		{"flag wins over everything", "cli-id", "env-f", "env-r", deep, cfg, "cli-id"},
		{"FLEET_IDENTITY beats RINGER_IDENTITY", "", "env-f", "env-r", deep, cfg, "env-f"},
		{"RINGER_IDENTITY beats file", "", "", "env-r", deep, cfg, "env-r"},
		{"fleet-agent file found walking up", "", "", "", deep, cfg, "repo-bot"},
		{"config default when no file", "", "", "", t.TempDir(), cfg, "cfg-default"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("FLEET_IDENTITY", tc.fleetEnv)
			t.Setenv("RINGER_IDENTITY", tc.ringerEnv)
			if got := ResolveIdentity(tc.flag, tc.cfg, tc.startDir); got != tc.want {
				t.Errorf("got %q, want %q", got, tc.want)
			}
		})
	}
}

func TestResolveIdentityHostnameFallback(t *testing.T) {
	t.Setenv("FLEET_IDENTITY", "")
	t.Setenv("RINGER_IDENTITY", "")
	got := ResolveIdentity("", &AppConfig{}, t.TempDir())
	if got == "" {
		t.Fatal("hostname fallback must never return empty")
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `./build.sh --test`
Expected: FAIL — `undefined: ResolveIdentity`.

- [ ] **Step 3: Implement**

```go
// internal/config/identity.go
package config

import (
	"os"
	"path/filepath"
	"strings"
)

func ResolveIdentity(flagValue string, cfg *AppConfig, startDir string) string {
	if flagValue != "" {
		return flagValue
	}
	if v := os.Getenv("FLEET_IDENTITY"); v != "" {
		return v
	}
	if v := os.Getenv("RINGER_IDENTITY"); v != "" {
		return v
	}
	for dir := startDir; ; dir = filepath.Dir(dir) {
		b, err := os.ReadFile(filepath.Join(dir, ".fleet-agent"))
		if err == nil {
			if id := strings.TrimSpace(string(b)); id != "" {
				return id
			}
		}
		if dir == filepath.Dir(dir) { // reached filesystem root
			break
		}
	}
	if cfg != nil && cfg.IdentityDefault != "" {
		return cfg.IdentityDefault
	}
	host, err := os.Hostname()
	if err != nil || host == "" {
		return "ringer"
	}
	return strings.Split(host, ".")[0] // short hostname
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `./build.sh --test`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/config
git commit -m "feat: identity resolution chain (flag > env > .fleet-agent > config > hostname)"
```

---

### Task 4: Vendor the jail package from flywheel

**Files:**
- Create: `internal/jail/` — copy `jail.go`, `root.go`, `unshare.go`, `mount.go`, `preflight.go`, `jail_test.go`, `unshare_test.go` from `/home/jim/projects/flywheel/jail/`
- Test: the vendored `jail_test.go` + `unshare_test.go` (root-gated tests auto-skip)

**Interfaces:**
- Consumes: nothing.
- Produces (already defined by the vendored code — do not alter):

```go
type Jail interface {
	Setup(mounts []Mount) error
	Command(name string, args ...string) *exec.Cmd
	Teardown() error
	Root() string
}
func NewUnshareJail(root string) *UnshareJail
func (j *UnshareJail) SetDropUser(username string)
func CheckUnsharePreflight() PreflightResult
func BindMount(src, dst string, readOnly bool) Mount
func TmpfsMount(target string) Mount
func HostMounts(root string) []Mount
func BaseMounts(root string) []Mount
```

- [ ] **Step 1: Copy the package and fix provenance/import identity**

```bash
mkdir -p internal/jail
cp /home/jim/projects/flywheel/jail/*.go internal/jail/
```

Prepend to `internal/jail/jail.go` (above the package clause's doc comment):

```go
// Package jail is vendored from github.com/corruptmemory/flywheel (jail/,
// as of 2026-03-29). Rootless Linux isolation via unprivileged user
// namespaces + mount namespace + chroot. Do not diverge from upstream
// without noting it here; deps are stdlib + golang.org/x/sys only.
```

The package name is already `jail`; no import-path rewrites are needed because the files reference only stdlib and `golang.org/x/sys`. If any file imports a flywheel-internal path, that file's copy step must be flagged and resolved before commit (expected: none).

```bash
go get golang.org/x/sys@v0.21.0
```

- [ ] **Step 2: Run the vendored tests**

Run: `./build.sh --test`
Expected: PASS — mount-table construction tests run; `TestJailSetupTeardownAsRoot` / `TestJailBindMountAsRoot` skip (not root). If gofmt flags the copied files, run `gofmt -w internal/jail` (formatting-only change, allowed).

- [ ] **Step 3: Smoke the preflight on this machine**

```bash
./build.sh
cat > /tmp/jail-preflight-check.go <<'EOF'
//go:build ignore
package main

import (
	"fmt"
	"github.com/corruptmemory/ringer/internal/jail"
)

func main() { fmt.Printf("%+v\n", jail.CheckUnsharePreflight()) }
EOF
go run /tmp/jail-preflight-check.go
```

Expected: preflight result with OK=true on this Arch machine (userns enabled by default). Record the exact output in the Task 7 findings doc later. Delete `/tmp/jail-preflight-check.go` afterward.

- [ ] **Step 4: Commit**

```bash
git add internal/jail go.mod go.sum
git commit -m "feat: vendor jail package from flywheel (rootless userns isolation)"
```

---

### Task 5: Store core — open with pragma discipline, attempts schema, insert/count

**Files:**
- Create: `internal/store/store.go`, `internal/store/schema.go`
- Test: `internal/store/store_test.go`

**Interfaces:**
- Consumes: nothing (path comes from config at call sites).
- Produces:

```go
package store

type Store struct{ /* unexported */ }

type Attempt struct {
	RunID       string
	RunName     string
	TaskKey     string
	Engine      string
	Model       string
	TaskType    string
	Verdict     string  // PASS | FAIL | TIMEOUT | ERROR
	Retry       int
	DurationS   float64
	Tokens      int64   // -1 = unknown
	CheckOutput string
	Identity    string
	CreatedAt   string  // UTC RFC3339
}

func Open(path string) (*Store, error)      // creates schema if absent
func (s *Store) Close() error
func (s *Store) InsertAttempt(a Attempt) error   // retries bounded on BUSY
func (s *Store) CountAttempts() (int64, error)
func (s *Store) Checkpoint() error               // wal_checkpoint(TRUNCATE)
func (s *Store) Integrity() error                // PRAGMA integrity_check
```

- [ ] **Step 1: Write the failing tests**

```go
// internal/store/store_test.go
package store

import (
	"path/filepath"
	"testing"
)

func TestOpenInsertCount(t *testing.T) {
	s, err := Open(filepath.Join(t.TempDir(), "ringer.db"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer s.Close()

	a := Attempt{
		RunID: "r1", RunName: "demo", TaskKey: "alpha", Engine: "mock",
		Model: "m", TaskType: "probe", Verdict: "PASS", Retry: 0,
		DurationS: 1.5, Tokens: 42, CheckOutput: "ok", Identity: "test",
		CreatedAt: "2026-07-08T00:00:00Z",
	}
	if err := s.InsertAttempt(a); err != nil {
		t.Fatalf("InsertAttempt: %v", err)
	}
	n, err := s.CountAttempts()
	if err != nil || n != 1 {
		t.Fatalf("CountAttempts = %d, %v; want 1, nil", n, err)
	}
	if err := s.Checkpoint(); err != nil {
		t.Errorf("Checkpoint: %v", err)
	}
	if err := s.Integrity(); err != nil {
		t.Errorf("Integrity: %v", err)
	}
}

func TestOpenIsIdempotent(t *testing.T) {
	p := filepath.Join(t.TempDir(), "ringer.db")
	for i := 0; i < 2; i++ {
		s, err := Open(p)
		if err != nil {
			t.Fatalf("Open #%d: %v", i+1, err)
		}
		s.Close()
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `./build.sh --test`
Expected: FAIL — package `internal/store` does not exist.

- [ ] **Step 3: Implement**

```go
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
`
```

```go
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
		return se.Code() == 5 || se.Code() == 6 // SQLITE_BUSY, SQLITE_LOCKED
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
```

The `Attempt` struct goes in `store.go` exactly as declared in the Interfaces block.

```bash
go get modernc.org/sqlite@latest
go mod tidy
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `./build.sh --test`
Expected: PASS. Also verify the libc pin resolved from modernc: `go list -m modernc.org/libc` — record the version; it must match `modernc.org/sqlite`'s own go.mod requirement (check with `go mod graph | grep 'modernc.org/sqlite@' | grep libc`).

- [ ] **Step 5: Commit**

```bash
git add internal/store go.mod go.sum
git commit -m "feat: SQLite store core with spec pragma discipline (modernc)"
```

---

### Task 6: Multi-process store smoke test (permanent CI test; Spike #3 go/no-go)

**Files:**
- Create: `internal/store/multiprocess_test.go`

**Interfaces:**
- Consumes: `store.Open`, `InsertAttempt`, `CountAttempts`, `Integrity` (Task 5).
- Produces: the permanent evidence that modernc handles ringer's multi-process envelope. If this test fails irreparably, the driver decision escalates per spec §7 (swap seam to ncruces) — do NOT paper over it.

- [ ] **Step 1: Write the test (it should pass immediately if Task 5 is sound — the point is the evidence, not red/green)**

```go
// internal/store/multiprocess_test.go
package store

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// TestMultiProcessWrites spawns writer child processes against ONE database
// file — the exact topology of concurrent `ringer run` invocations plus the
// HUD reader. Deliberately over-stressed vs the real envelope (~10 writes/min):
// 5 procs x 200 rows as fast as they can go.
func TestMultiProcessWrites(t *testing.T) {
	if testing.Short() {
		t.Skip("multi-process smoke skipped in -short")
	}
	const nProcs, nRows = 5, 200
	dbPath := filepath.Join(t.TempDir(), "smoke.db")

	// Create schema up front so children race only on writes.
	s, err := Open(dbPath)
	if err != nil {
		t.Fatalf("parent Open: %v", err)
	}
	s.Close()

	procs := make([]*exec.Cmd, nProcs)
	for i := range procs {
		cmd := exec.Command(os.Args[0], "-test.run", "^TestSmokeChildProcess$", "-test.v")
		cmd.Env = append(os.Environ(),
			"STORE_SMOKE_DB="+dbPath,
			fmt.Sprintf("STORE_SMOKE_PROC=%d", i),
			fmt.Sprintf("STORE_SMOKE_ROWS=%d", nRows),
		)
		out, err := os.CreateTemp(t.TempDir(), "child-*.log")
		if err != nil {
			t.Fatal(err)
		}
		cmd.Stdout, cmd.Stderr = out, out
		if err := cmd.Start(); err != nil {
			t.Fatalf("start child %d: %v", i, err)
		}
		procs[i] = cmd
	}
	for i, cmd := range procs {
		if err := cmd.Wait(); err != nil {
			t.Errorf("child %d failed: %v", i, err)
		}
	}

	s, err = Open(dbPath)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	defer s.Close()
	n, err := s.CountAttempts()
	if err != nil {
		t.Fatalf("CountAttempts: %v", err)
	}
	if n != int64(nProcs*nRows) {
		t.Errorf("rows = %d, want %d (lost writes!)", n, nProcs*nRows)
	}
	if err := s.Integrity(); err != nil {
		t.Errorf("integrity after concurrent writes: %v", err)
	}
}

// TestSmokeChildProcess is the child body; it is a no-op unless the smoke
// env vars are present, so a plain `go test ./...` never runs it standalone.
func TestSmokeChildProcess(t *testing.T) {
	dbPath := os.Getenv("STORE_SMOKE_DB")
	if dbPath == "" {
		t.Skip("not a smoke child")
	}
	var proc, rows int
	fmt.Sscanf(os.Getenv("STORE_SMOKE_PROC"), "%d", &proc)
	fmt.Sscanf(os.Getenv("STORE_SMOKE_ROWS"), "%d", &rows)

	s, err := Open(dbPath)
	if err != nil {
		t.Fatalf("child open: %v", err)
	}
	defer s.Close()
	for i := 0; i < rows; i++ {
		a := Attempt{
			RunID:   fmt.Sprintf("run-%d", proc),
			TaskKey: fmt.Sprintf("task-%d-%d", proc, i),
			Verdict: "PASS", CreatedAt: "2026-07-08T00:00:00Z",
		}
		if err := s.InsertAttempt(a); err != nil {
			t.Fatalf("child %d insert %d: %v", proc, i, err)
		}
	}
}
```

- [ ] **Step 2: Run it and capture the evidence**

Run: `./build.sh --test` then specifically `go test -run TestMultiProcessWrites -v ./internal/store/ | tee /tmp/store-smoke.txt`
Expected: PASS — 1000 rows, integrity ok. If it FAILS with lost rows or corruption: stop, record the failure verbatim in the Task 7 findings doc, and raise the driver-swap decision (spec §7) before proceeding — that is this task succeeding at its job, not a task failure.

- [ ] **Step 3: Run it under race too**

Run: `go test -race -run TestMultiProcessWrites -v ./internal/store/`
Expected: PASS, no race reports (children are separate processes; this races the parent's own open/close paths).

- [ ] **Step 4: Commit**

```bash
git add internal/store/multiprocess_test.go
git commit -m "test: permanent multi-process SQLite smoke (5 procs x 200 rows)"
```

---

### Task 7: Spike — opencode under namespace-UID-0 (findings gate for Plan 2)

**Files:**
- Create: `internal/jail/spike_opencode_test.go` (build tag `spike`), `docs/superpowers/specs/2026-07-08-spike-findings.md`

**Interfaces:**
- Consumes: `jail.NewUnshareJail`, `jail.HostMounts`, `jail.BindMount`, `jail.TmpfsMount` (Task 4).
- Produces: a recorded YES/NO on "does opencode run as ns-root?" — Plan 2's engine work reads this to decide whether `SetDropUser` + UID-tax handling get built. Nothing imports this test.

- [ ] **Step 1: Write the spike test**

```go
//go:build spike

// internal/jail/spike_opencode_test.go
//
// SPIKE (design spec §13.1): does the opencode CLI tolerate running as
// namespace-UID-0? If yes, jailed workers need no SetDropUser, files land
// owned by the invoking user, and the UID-mapping cleanup tax vanishes.
// Run manually: go test -tags=spike -run TestSpikeOpencodeNsRoot -v ./internal/jail/
package jail

import (
	"os/exec"
	"path/filepath"
	"testing"
)

func TestSpikeOpencodeNsRoot(t *testing.T) {
	if _, err := exec.LookPath("opencode"); err != nil {
		t.Skip("opencode not installed")
	}
	if r := CheckUnsharePreflight(); !r.OK() {
		t.Skipf("userns preflight failed: %v", r)
	}
	root := t.TempDir()
	work := filepath.Join(root, "workspace")

	j := NewUnshareJail(root)
	mounts := append(HostMounts(root),
		BindMount(work, filepath.Join(root, "workspace"), false),
		TmpfsMount(filepath.Join(root, "tmp")),
	)
	// NOTE: no SetDropUser — that is the whole experiment.
	if err := j.Setup(mounts); err != nil {
		t.Fatalf("jail setup: %v", err)
	}
	defer j.Teardown()

	// Probe 1: does it start at all as ns-root?
	out, err := j.Command("opencode", "--version").CombinedOutput()
	t.Logf("opencode --version (ns-root):\nerr=%v\n%s", err, out)

	// Probe 2: id inside the jail, for the record.
	out2, _ := j.Command("id").CombinedOutput()
	t.Logf("id inside jail: %s", out2)

	if err != nil {
		t.Fatalf("VERDICT: opencode refuses ns-root (record in findings doc): %v", err)
	}
	t.Log("VERDICT: opencode tolerates ns-root — SetDropUser not needed for the opencode lane")
}
```

Note for the implementer: the vendored jail package's mount helpers take
paths as they exist in flywheel's API — if `HostMounts`/`BindMount`
signatures differ from the above when you read the vendored source, adapt
the *test* to the vendored API (the vendored code is immutable, the spike
is not). The probe intent is fixed: assemble host-toolchain-ro + workspace-rw
+ tmpfs scratch, no drop-user, run `opencode --version` and `id`.

- [ ] **Step 2: Run the spike and capture output**

Run: `go test -tags=spike -run TestSpikeOpencodeNsRoot -v ./internal/jail/ 2>&1 | tee /tmp/spike-opencode.txt`
Expected: either verdict is a valid outcome. Auth note: `opencode --version` needs no login; if it passes, optionally repeat with a trivial `opencode run` while logged in for a stronger signal.

- [ ] **Step 3: Record findings**

Create `docs/superpowers/specs/2026-07-08-spike-findings.md`:

```markdown
# Spike findings — 2026-07-08 (Plan 1)

## S1: opencode under namespace-UID-0 (spec §13.1)
- Command: `go test -tags=spike -run TestSpikeOpencodeNsRoot -v ./internal/jail/`
- Verdict: <PASTE: tolerates / refuses ns-root>
- Raw output: <paste the two t.Logf blocks verbatim>
- Plan 2 consequence: <SetDropUser wiring needed: yes/no; UID-tax handling needed: yes/no>

## S2: worktrees x jail (spec §13.2)
- (filled by Task 8)

## S3: modernc multi-process smoke (spec §13.3)
- Command: `go test -run TestMultiProcessWrites -v ./internal/store/`
- Verdict: <PASS/FAIL + row count + integrity result>
- libc pin: <output of `go list -m modernc.org/libc`>

## Jail preflight on this machine (Task 4 step 3)
- <paste CheckUnsharePreflight output>
```

- [ ] **Step 4: Commit**

```bash
git add internal/jail/spike_opencode_test.go docs/superpowers/specs/2026-07-08-spike-findings.md
git commit -m "spike: opencode under ns-root jail, findings recorded"
```

---

### Task 8: Spike — worktrees × jail (findings gate for Plan 2)

**Files:**
- Create: `internal/jail/spike_worktree_test.go` (build tag `spike`)
- Modify: `docs/superpowers/specs/2026-07-08-spike-findings.md` (fill §S2)

**Interfaces:**
- Consumes: `jail` package (Task 4).
- Produces: recorded answer to "what must be bind-mounted for git to work inside a jailed worktree?" — Plan 2's worktrees-mode mount table reads this.

- [ ] **Step 1: Write the spike test**

```go
//go:build spike

// internal/jail/spike_worktree_test.go
//
// SPIKE (design spec §13.2): a git worktree's .git is a FILE containing
// "gitdir: <parent-repo>/.git/worktrees/<name>". For git to work inside a
// jail that binds only the worktree rw, the parent repo must also be
// visible (read-only suffices for status/diff; commit needs write into
// .git/worktrees/<name>). This spike measures exactly what is needed.
// Run: go test -tags=spike -run TestSpikeWorktreeInJail -v ./internal/jail/
package jail

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func run(t *testing.T, dir string, name string, args ...string) string {
	t.Helper()
	cmd := exec.Command(name, args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("%s %v in %s: %v\n%s", name, args, dir, err, out)
	}
	return string(out)
}

func TestSpikeWorktreeInJail(t *testing.T) {
	if r := CheckUnsharePreflight(); !r.OK() {
		t.Skipf("userns preflight failed: %v", r)
	}
	scratch := t.TempDir()
	repo := filepath.Join(scratch, "repo")
	wt := filepath.Join(scratch, "wt-alpha")

	// Parent repo with one commit.
	os.MkdirAll(repo, 0o755)
	run(t, repo, "git", "init", "-q")
	run(t, repo, "git", "-c", "user.email=s@s", "-c", "user.name=s", "commit", "--allow-empty", "-m", "init", "-q")
	run(t, repo, "git", "worktree", "add", "-q", wt)

	root := filepath.Join(scratch, "jailroot")
	os.MkdirAll(root, 0o755)
	j := NewUnshareJail(root)
	mounts := append(HostMounts(root),
		BindMount(wt, filepath.Join(root, "workspace"), false),   // worktree rw
		BindMount(repo, filepath.Join(root, "parent-repo"), true), // parent RO — the experiment
		TmpfsMount(filepath.Join(root, "tmp")),
	)
	if err := j.Setup(mounts); err != nil {
		t.Fatalf("jail setup: %v", err)
	}
	defer j.Teardown()

	// Probe 1: git status inside the jailed worktree.
	// NOTE: the gitdir pointer contains the HOST path of the parent repo,
	// which inside the chroot is /parent-repo. Expect this to FAIL unless
	// the parent is mounted at the SAME path as on the host. Probe both.
	out1, err1 := j.Command("git", "-C", "/workspace", "status").CombinedOutput()
	t.Logf("git status with parent at /parent-repo (path-mismatched): err=%v\n%s", err1, out1)

	// Probe 2: remount parent at its host-identical path.
	j2root := filepath.Join(scratch, "jailroot2")
	os.MkdirAll(j2root, 0o755)
	j2 := NewUnshareJail(j2root)
	mounts2 := append(HostMounts(j2root),
		BindMount(wt, filepath.Join(j2root, wt), false),    // host-identical path
		BindMount(repo, filepath.Join(j2root, repo), true), // host-identical path, RO
		TmpfsMount(filepath.Join(j2root, "tmp")),
	)
	if err := j2.Setup(mounts2); err != nil {
		t.Fatalf("jail2 setup: %v", err)
	}
	defer j2.Teardown()
	out2, err2 := j2.Command("git", "-C", wt, "status").CombinedOutput()
	t.Logf("git status with host-identical paths, parent RO: err=%v\n%s", err2, out2)

	if err2 != nil {
		t.Fatal("VERDICT: even host-identical RO parent insufficient — record details")
	}
	t.Log("VERDICT: worktrees need host-identical mount paths; parent RO sufficient for status (commit needs .git/worktrees/<name> writable — probe in Plan 2 if uncommitted-diff pattern changes)")
}
```

- [ ] **Step 2: Run the spike and capture output**

Run: `go test -tags=spike -run TestSpikeWorktreeInJail -v ./internal/jail/ 2>&1 | tee /tmp/spike-worktree.txt`
Expected: probe 1 fails, probe 2 passes (hypothesis) — but whatever happens is the finding.

- [ ] **Step 3: Fill §S2 of the findings doc** with the two probes' verbatim verdict lines and the Plan 2 consequence (the worktrees-mode jail mount rule).

- [ ] **Step 4: Commit**

```bash
git add internal/jail/spike_worktree_test.go docs/superpowers/specs/2026-07-08-spike-findings.md
git commit -m "spike: worktree-in-jail mount requirements, findings recorded"
```

---

### Task 9: CI workflow

**Files:**
- Create: `.github/workflows/ci.yml`

**Interfaces:**
- Consumes: `./build.sh --test` (Task 1).
- Produces: green CI on every push/PR to any branch; Linux + macOS matrix. (`release.yml` is deleted in Plan 5's cutover, not here.)

- [ ] **Step 1: Write the workflow**

```yaml
# .github/workflows/ci.yml
name: ci
on:
  push:
    branches: ["**"]
  pull_request:

jobs:
  test:
    strategy:
      matrix:
        os: [ubuntu-latest, macos-latest]
    runs-on: ${{ matrix.os }}
    steps:
      - uses: actions/checkout@v4
      - uses: actions/setup-go@v5
        with:
          go-version: "1.26.x"
      - run: ./build.sh --test
```

Notes: jail root-gated tests skip automatically (not root); spike tests don't run (build tag); the multi-process smoke runs on both OSes — that's deliberate, macOS is a supported ringer platform.

- [ ] **Step 2: Validate locally**

Run: `./build.sh --test`
Expected: PASS — the workflow runs the identical command.

- [ ] **Step 3: Commit and push, verify CI**

```bash
git add .github/workflows/ci.yml
git commit -m "ci: build.sh --test on linux + macos"
git push -u origin go-rewrite
gh run watch --exit-status || echo "CI FAILED - investigate before proceeding"
```

Expected: both matrix legs green.

---

## Self-Review (completed at write time)

- **Spec coverage (Plan 1 scope):** spec §7 pragmas → Task 5; §7 smoke → Task 6; §13.1 → Task 7; §13.2 → Task 8; §13.3 → Task 6+findings; §3 strict config/removed keys → Task 2; identity chain → Task 3; jail vendoring (§2, §6) → Task 4; build.sh/`CGO_ENABLED=0` (§1, §4) → Task 1; CI (§10) → Task 9. Remaining spec sections are Plans 2–5 by design.
- **Placeholders:** none; every code step is complete. The one intentional adapt-point (vendored jail API signatures, Task 7) is explicitly bounded with fixed intent.
- **Type consistency:** `store.Attempt` fields match spec §7's frozen eval-row keys; `config.EngineConfig` matches spec §6's TOML keys; `ResolveIdentity` order matches spec §3.
