# Ringer Go Plan 5a — Analytics Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Port Ringer's model-performance analytics (the `models` scoreboard, the OpenRouter `catalog`, and `db` maintenance) from `ringer.py` to Go, filling the HUD's `/hud/models` panel from SQLite — the last Python-only feature before the Plan-5c cutover can delete `ringer.py`.

**Architecture:** The Go `attempts` table (written per-attempt by the runner since Plan 2) is the primary eval store. Analytics is **SQL over `attempts`**: a scoreboard query groups attempts into task-instances (window functions), aggregates counts/rates/medians (median via a registered pure-Go SQLite aggregate), and JOINs `catalog_models` (cost) — no in-Go re-implementation of SUM/AVG/JOIN. The OpenRouter catalog is fetched, diffed, and persisted into `catalog_models` / `catalog_events` (directly queried by the `catalog` command). The model-identity registry and MODEL-NOTES ship **embedded** (`go:embed`, config-path override) because Python resolved them repo-relative (`__file__`) — which breaks for an installed binary; identity display + judgment notes are procedural (fallback rules, fuzzy text match), so they resolve in Go, not SQL.

**Tech Stack:** Go 1.26, `CGO_ENABLED=0`; `modernc.org/sqlite` v1.53.0 (custom aggregate via `sqlite.RegisterFunction`); go-flags subcommands; `net/http` for the OpenRouter fetch; templ for the `/hud/models` panel and the `models --html` scoreboard page; `math/big` `Rat` for exact price parsing; table-driven + golden tests via `./build.sh --test`.

## Global Constraints

Every task's requirements implicitly include this section.

- **Build/test ONLY via `./build.sh` and `./build.sh --test [--race]`.** Never invoke `go build` / `go test` / `templ generate` directly. The one sanctioned exception is regenerating golden fixtures with `go test ./<pkg> -run <Name> -update` (documented per task).
- **SQLite discipline (frozen, spec §7):** pragmas in order `busy_timeout=5000` → `journal_mode=WAL` → `synchronous=NORMAL`; `SetMaxOpenConns(1)`; **never** `_txlock=immediate` (cznic #192); explicit `wal_checkpoint(TRUNCATE)` only at run end; never bump `modernc.org/libc` independently of `modernc.org/sqlite`. The existing `internal/store` `Open`/`withBusyRetry`/`isBusy`/`Checkpoint`/`Integrity` machinery is correct — extend it, do not rewrite it.
- **`attempts` schema is Go-authoritative** (spec §9.4), columns frozen by Plan 1/2: `run_id, run_name, task_key, engine, model, task_type, verdict, retry (INT), duration_s (REAL seconds), tokens (INT, -1 = unknown), check_output, identity (run-operator identity, NOT model display), created_at (UTC RFC3339)`. Analytics reads these fields — Python's JSONL field names (`logged_at`, `duration_ms`, `worker_tokens`, `worker_engine`) do **not** exist here; the mappings are: `logged_at→created_at`, `duration_ms→duration_s*1000` for ms-display, `worker_tokens→tokens` (with `-1` meaning unknown → excluded from aggregates), `worker_engine/engine→engine`, retry-flag → `retry > 0`.
- **Frozen contracts (spec §9):** `registry/model-identity.toml` format (§9.7); the SKILL.md CLI surface (§9.6) — `models [--task-type|--explore|--open]`, `catalog --changes`; the scoreboard tier rule (proven ≥3 tasks else probation); mock-worker / nudge-hook / run-state schemas are untouched by this plan.
- **`db import` backfill precedence (frozen, spec §3):** task_type lookup precedence `"<run_id>:<task_key>"` > `"<run_id>"` > `"name:<prefix>"` (longest prefix wins); model backfilled from `<runs-dir>/<run_id>.json` `tasks[].key==task_key → task.model`; never overwrite an existing non-empty value.
- **Data placement (house rule — `queryable-data-goes-in-sqlite`):** data queried relationally (SUM/AVG/COUNT/JOIN/WHERE) lives in SQLite (`attempts`, `catalog_models`, `catalog_events`); data resolved procedurally or matched as prose lives in Go (identity fallback rules, MODEL-NOTES fuzzy match) and ships embedded. Push aggregation INTO SQL — including `median()` via a registered Go aggregate; concede nothing to Go loops that SQL can do.
- **Package layering (never reverse):** `store` (leaf; imports `modernc.org/sqlite` + `internal/catalog` for column types) ← `catalog` (leaf; stdlib + `net/http`) ← `scoreboard` (imports `store`, `catalog`) ← `hud`/`cmd`. No package reaches into `runner`. The HUD reads the store read-side only.
- **Golden fidelity:** the scoreboard's aggregate output must match `ringer.py models --json` for the same eval rows; lock it with a golden/table test seeded from representative attempts. Where Go-authoritative rounding differs (float `duration_s` median vs Python integer-ms median), the Go output is the new authority — document the divergence, don't chase sub-millisecond parity.

---

## File Structure

**New files:**
- `internal/store/median.go` — registers the `median()` SQLite aggregate (pure-Go `AggregateFunction`) on the driver at package init.
- `internal/store/analytics.go` — read/write methods for the analytics tables: `ScoreboardRows`, `ModelTaskTypeRows`, `ReplaceCatalog`, `AppendCatalogEvents`, `CatalogModels`, `CatalogEvents`, `ReplaceIdentity`, `ExportAttempts`, `ImportAttempts`, plus the row structs.
- `internal/catalog/catalog.go` — `Model`, `Event`, pricing helpers, `Normalize*`, `Fetch`, `LoadSnapshotFile`.
- `internal/catalog/diff.go` — `Diff(old, new []Model, ts string) []Event`.
- `internal/catalog/catalog_test.go`, `internal/catalog/diff_test.go`.
- `internal/scoreboard/identity.go` — `ModelIdentity`, `Registry`, `Resolve`, `LoadRegistry` (embedded TOML + override), `SyncIdentity(store)`.
- `internal/scoreboard/notes.go` — `ParseNotesSections`, `JudgmentNotes`, embedded MODEL-NOTES + override, note-render helpers for `--html`.
- `internal/scoreboard/scoreboard.go` — `Scoreboard(store, Filter)` orchestration: SQL rows + Go identity fallback + notes attach; `Filter`, `Row`, `TaskTypeRow`.
- `internal/scoreboard/embed.go` — `//go:embed` of the shipped `registry/model-identity.toml` and `docs/MODEL-NOTES.md`.
- `internal/scoreboard/*_test.go`.
- `cmd/ringer/models.go`, `cmd/ringer/catalog.go`, `cmd/ringer/db.go` + tests.
- `internal/hud/views/models.templ` (replace the stub component) + `internal/hud/models_scoreboard.templ` for `models --html`.

**Modified files:**
- `internal/store/schema.go` — schema v2: `catalog_models`, `catalog_events`, `identity`, `identity_defaults` tables; `schemaVersion` 1→2.
- `internal/config/config.go` — `AppConfig` gains `[scoreboard]` overrides (`model_identity_path`, `model_notes_path`) and `[catalog]` (`source`, `path`); accessors with embedded-default fallback.
- `internal/hud/server.go`, `internal/hud/models.go` — HUD reads the store scoreboard for `/hud/models`; `Server` gains the DB path.
- `cmd/ringer/run.go` — background, throttled catalog auto-refresh on `run`/`demo`.
- `config.sample.toml` — document the new `[scoreboard]`/`[catalog]` keys (the Python-era `[eval]` cleanup is Plan-5c).

## Task Overview

1. Store schema v2 + `median()` UDF
2. Model-identity registry (embedded + override) + `Resolve` + `SyncIdentity`
3. Catalog core — types, pricing, normalize, fetch, snapshot parse
4. Catalog diff + refresh + persistence (`catalog_models`/`catalog_events`)
5. Scoreboard — the SQL query + tiers/cost + Go identity fallback
6. Model notes (embedded + fuzzy match)
7. `models` subcommand (table / `--json` / `--explore` / `--html --open`)
8. `catalog` subcommand (`--refresh/--source/--file/--free/--changes/--json`)
9. `db` subcommand (`export` / `import` / `integrity` / `checkpoint`)
10. `/hud/models` panel from SQLite
11. `run` catalog auto-refresh (background, 24h throttle)

---

## Task 1: Store schema v2 + `median()` UDF

**Files:**
- Modify: `internal/store/schema.go`
- Create: `internal/store/median.go`
- Modify: `internal/store/store.go` (call `registerMedian()` once)
- Test: `internal/store/median_test.go`, `internal/store/schema_test.go`

**Interfaces:**
- Consumes: existing `store.Open(path) (*Store, error)`, `withBusyRetry`.
- Produces: schema v2 tables (`catalog_models`, `catalog_events`, `identity`, `identity_defaults`); a SQL-callable `median(x)` aggregate returning `NULL` for an empty set, the middle value for odd counts, and the mean of the two middle values for even counts (float result; callers round for display). Later tasks depend on these tables and on `median()`.

**Background:** modernc.org/sqlite v1.53.0 exports `sqlite.RegisterFunction(name string, impl *sqlite.FunctionImpl) error`. `FunctionImpl.MakeAggregate func(ctx sqlite.FunctionContext) (sqlite.AggregateFunction, error)` registers an aggregate; the `AggregateFunction` interface is `Step(ctx *FunctionContext, rowArgs []driver.Value) error`, `WindowInverse(...) error`, `WindowValue(*FunctionContext) (driver.Value, error)`, `Final(*FunctionContext)`. Registration is process-global and applies to every connection opened *after* the call, so register at package init before any `Open`. We use `median()` only as a plain aggregate (never an `OVER(...)` window), so `WindowInverse` will not be called — implement it as a no-op error guard.

- [ ] **Step 1: Write the failing test for `median()`**

Create `internal/store/median_test.go`:

```go
package store

import (
	"path/filepath"
	"testing"
)

func TestMedianAggregate(t *testing.T) {
	s, err := Open(filepath.Join(t.TempDir(), "m.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if _, err := s.db.Exec(`CREATE TABLE t(g TEXT, v REAL)`); err != nil {
		t.Fatal(err)
	}
	rows := [][2]any{{"odd", 1.0}, {"odd", 5.0}, {"odd", 3.0}, // median 3
		{"even", 10.0}, {"even", 20.0}, {"even", 40.0}, {"even", 30.0}, // median 25
		{"one", 7.0}} // median 7
	for _, r := range rows {
		if _, err := s.db.Exec(`INSERT INTO t(g,v) VALUES (?,?)`, r[0], r[1]); err != nil {
			t.Fatal(err)
		}
	}
	want := map[string]float64{"odd": 3, "even": 25, "one": 7}
	got := map[string]float64{}
	res, err := s.db.Query(`SELECT g, median(v) FROM t GROUP BY g`)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Close()
	for res.Next() {
		var g string
		var m float64
		if err := res.Scan(&g, &m); err != nil {
			t.Fatal(err)
		}
		got[g] = m
	}
	for g, w := range want {
		if got[g] != w {
			t.Fatalf("median[%s]=%v want %v", g, got[g], w)
		}
	}
	// Empty set -> NULL (not 0).
	var n any
	if err := s.db.QueryRow(`SELECT median(v) FROM t WHERE g='none'`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != nil {
		t.Fatalf("median over empty set = %v, want NULL", n)
	}
}
```

- [ ] **Step 2: Run it to confirm it fails**

Run: `./build.sh --test 2>&1 | grep -A3 TestMedianAggregate`
Expected: FAIL — `no such function: median`.

- [ ] **Step 3: Implement `median()` registration**

Create `internal/store/median.go`:

```go
// internal/store/median.go
package store

import (
	"database/sql/driver"
	"sort"

	sqlite "modernc.org/sqlite"
)

// medianAgg accumulates numeric argument values and returns their median.
// Used only as a plain aggregate (never an OVER(...) window), so
// WindowInverse is a guard, not a real sliding-window removal.
type medianAgg struct{ vals []float64 }

func (a *medianAgg) Step(_ *sqlite.FunctionContext, args []driver.Value) error {
	if len(args) == 0 || args[0] == nil {
		return nil // SQL semantics: aggregates skip NULL inputs
	}
	switch v := args[0].(type) {
	case int64:
		a.vals = append(a.vals, float64(v))
	case float64:
		a.vals = append(a.vals, v)
	}
	return nil
}

func (a *medianAgg) WindowInverse(_ *sqlite.FunctionContext, _ []driver.Value) error {
	return errMedianNotWindow
}

func (a *medianAgg) WindowValue(_ *sqlite.FunctionContext) (driver.Value, error) {
	n := len(a.vals)
	if n == 0 {
		return nil, nil // empty set -> NULL
	}
	s := append([]float64(nil), a.vals...)
	sort.Float64s(s)
	mid := n / 2
	if n%2 == 1 {
		return s[mid], nil
	}
	return (s[mid-1] + s[mid]) / 2.0, nil
}

func (a *medianAgg) Final(_ *sqlite.FunctionContext) {}

var errMedianNotWindow = driverError("median() is not supported as a window function")

type driverError string

func (e driverError) Error() string { return string(e) }

// registerMedian installs median() on every connection opened afterward.
// Idempotent-safe: guarded so repeated calls (test + prod Open) don't error.
func registerMedian() {
	if medianRegistered {
		return
	}
	medianRegistered = true
	err := sqlite.RegisterFunction("median", &sqlite.FunctionImpl{
		NArgs:         1,
		Deterministic: true,
		MakeAggregate: func(_ sqlite.FunctionContext) (sqlite.AggregateFunction, error) {
			return &medianAgg{}, nil
		},
	})
	if err != nil {
		panic("store: register median(): " + err.Error())
	}
}

var medianRegistered bool
```

- [ ] **Step 4: Call `registerMedian()` from `Open` (before opening the DB)**

In `internal/store/store.go`, at the very top of `Open`, before `sql.Open`:

```go
func Open(path string) (*Store, error) {
	registerMedian()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, fmt.Errorf("store: %w", err)
	}
	// ... unchanged ...
```

- [ ] **Step 5: Extend the schema to v2**

Replace `internal/store/schema.go` body:

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
```

**Design decision (deviates from spec §7 + an earlier in-session discussion — banner for review):** spec §7 lists `identity` / `catalog_models` / `catalog_events` tables. We keep `catalog_models` + `catalog_events` (the `catalog` command queries them: `--free` = `WHERE free=1`, `--changes` = `SELECT catalog_events`). We **drop the `identity` table**: model-identity resolution is procedural (Python's `enrich_model_groups_with_identity` resolves each model from its *latest* attempt's `(engine, model)` pair, then applies fallback rules — openrouter-prefix strip / engine-default / unknown), so a SQL JOIN can't do it faithfully, and nothing queries identity relationally. It resolves in Go over the ~dozens of aggregated scoreboard rows (Task 5). This is the house data-placement rule applied precisely: relational → table, procedural → code.

- [ ] **Step 6: Write the schema migration test**

Create `internal/store/schema_test.go` (idempotent open + version stamp + new tables exist). A v1 DB (only `attempts`/`schema_version`) must open cleanly and gain the new tables — `CREATE TABLE IF NOT EXISTS` makes this automatic; the `schema_version` row is written once by the existing `Open` guard, so an existing v1 DB keeps `version=1` while a fresh DB gets `version=2`. That is acceptable for this plan (no destructive migration; the tables are additive). Assert new tables are queryable:

```go
package store

import (
	"path/filepath"
	"testing"
)

func TestSchemaV2TablesExist(t *testing.T) {
	s, err := Open(filepath.Join(t.TempDir(), "s.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	for _, tbl := range []string{"attempts", "catalog_models", "catalog_events", "schema_version"} {
		if _, err := s.db.Exec(`SELECT COUNT(*) FROM ` + tbl); err != nil {
			t.Fatalf("table %s not queryable: %v", tbl, err)
		}
	}
}
```

- [ ] **Step 7: Run tests, confirm green**

Run: `./build.sh --test 2>&1 | grep -E "internal/store|FAIL|ok"`
Expected: `ok ... internal/store`.

- [ ] **Step 8: Commit**

```bash
git add internal/store/
git commit -m "store: schema v2 (catalog/identity tables) + median() SQLite aggregate"
```

---

## Task 2: Model-identity registry (embedded + override) + `Resolve`

**Files:**
- Create: `assets.go` (repo root, `package ringer`) — `//go:embed` of the curated registry + notes.
- Create: `internal/scoreboard/identity.go`
- Create: `internal/scoreboard/identity_test.go`
- Modify: `internal/config/config.go` — `[scoreboard]` override keys

**Interfaces:**
- Consumes: BurntSushi/toml (existing dep); the root `ringer` embed package.
- Produces:
  - `scoreboard.ModelIdentity struct { ModelDisplay, Harness, Access, Confidence, Source string }`
  - `scoreboard.Registry` with `Resolve(engine, modelKey string) ModelIdentity` (Task 5 calls this per aggregated model row).
  - `scoreboard.ParseRegistry(data []byte) (Registry, error)` and `scoreboard.LoadRegistry(overridePath string) Registry` — override file or embedded default; malformed/missing → empty registry.
  - `config.AppConfig` gains `Scoreboard ScoreboardConfig` with `ModelIdentityPath`/`ModelNotesPath` (empty → embedded); accessors `ModelIdentityPath()`/`ModelNotesPath()` return the expanded override or `""`.

**Note:** identity resolution is entirely Go-side (no `identity` table — see Task 1's banner). This package holds the pure registry + `Resolve`; Task 5 calls `Resolve(latestEngine, model)` per aggregated scoreboard row.

**Background — Python `resolve` (ringer.py:4749-4780) semantics to port verbatim:** look up `(engine, modelKey-or-engine-default)`; if listed → that identity. Else, if `engine=="opencode"` and rawKey has prefix `openrouter/` → display = key with `openrouter/` stripped, harness/access from engine meta (or `OpenCode`/`OpenRouter API`), confidence `fallback`. Else if engine meta exists and a lookup key exists → display = lookup key, engine harness/access, confidence `fallback`. Else → all fields = engine name (or `unknown`), access/confidence `unknown`. TOML load (ringer.py:4786-4833): per engine, `harness = harness || engine`, `access = access || "unknown"`, record `default_model_key`, build engine-meta identity `{display: engine, harness, access, confidence:"engine"}`; per model, identity `{display: display||key, harness, access, confidence, source}`.

- [ ] **Step 1: Create the root embed package**

First verify the repo root has no conflicting `.go` file: `ls *.go 2>/dev/null` (expected: none). Create `assets.go` at the repo root:

```go
// Package ringer embeds the curated reference assets that ship inside the
// static binary: the model-identity registry and the MODEL-NOTES judgment
// layer. They live at the repo root (registry/, docs/) — frozen locations
// kept past cutover — and //go:embed cannot reach upward from a nested
// package, so the embed directives live here at the module root. Config
// keys ([scoreboard] model_identity_path / model_notes_path) override these
// defaults with a live on-disk file.
package ringer

import _ "embed"

//go:embed registry/model-identity.toml
var ModelIdentityTOML []byte

//go:embed docs/MODEL-NOTES.md
var ModelNotesMD []byte
```

- [ ] **Step 2: Add `[scoreboard]` config keys**

In `internal/config/config.go`, add the struct and field, and an accessor:

```go
type ScoreboardConfig struct {
	ModelIdentityPath string `toml:"model_identity_path"` // empty -> embedded registry/model-identity.toml
	ModelNotesPath    string `toml:"model_notes_path"`    // empty -> embedded docs/MODEL-NOTES.md
}
```

Add `Scoreboard ScoreboardConfig \`toml:"scoreboard"\`` to `AppConfig`, and:

```go
// ModelIdentityPath returns the expanded override path for the identity
// registry, or "" to signal "use the embedded default".
func (c *AppConfig) ModelIdentityPath() string {
	if c.Scoreboard.ModelIdentityPath == "" {
		return ""
	}
	return ExpandUser(c.Scoreboard.ModelIdentityPath)
}

// ModelNotesPath is the override for MODEL-NOTES, or "" for the embedded default.
func (c *AppConfig) ModelNotesPath() string {
	if c.Scoreboard.ModelNotesPath == "" {
		return ""
	}
	return ExpandUser(c.Scoreboard.ModelNotesPath)
}
```

- [ ] **Step 3: Write the failing `Resolve` test**

Create `internal/scoreboard/identity_test.go`:

```go
package scoreboard

import "testing"

const testRegistry = `
[engines.codex]
harness = "Codex CLI"
access = "OAuth plan"
default_model_key = "gpt-5.5"
[engines.codex.models."gpt-5.5"]
display = "GPT-5.5"
confidence = "verified"
source = "x"

[engines.opencode]
harness = "OpenCode"
access = "OpenRouter API"
[engines.opencode.models."openrouter/z-ai/glm-5.2"]
display = "GLM 5.2"
confidence = "verified"
`

func TestResolve(t *testing.T) {
	reg, err := ParseRegistry([]byte(testRegistry))
	if err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		engine, key                       string
		wantDisplay, wantHarness, wantAcc string
	}{
		{"codex", "gpt-5.5", "GPT-5.5", "Codex CLI", "OAuth plan"},        // listed
		{"codex", "", "GPT-5.5", "Codex CLI", "OAuth plan"},              // engine default_model_key
		{"opencode", "openrouter/z-ai/glm-5.2", "GLM 5.2", "OpenCode", "OpenRouter API"}, // listed
		{"opencode", "openrouter/x/unlisted", "x/unlisted", "OpenCode", "OpenRouter API"}, // prefix-strip fallback
		{"ghost", "whatever", "ghost", "ghost", "unknown"},              // unknown engine
	}
	for _, c := range cases {
		got := reg.Resolve(c.engine, c.key)
		if got.ModelDisplay != c.wantDisplay || got.Harness != c.wantHarness || got.Access != c.wantAcc {
			t.Errorf("Resolve(%q,%q)=%+v want display=%q harness=%q access=%q",
				c.engine, c.key, got, c.wantDisplay, c.wantHarness, c.wantAcc)
		}
	}
}

func TestLoadRegistryEmbeddedFallback(t *testing.T) {
	// Empty override path -> embedded registry loads and resolves the shipped codex default.
	reg := LoadRegistry("")
	if got := reg.Resolve("codex", "gpt-5.5"); got.Harness == "" || got.Harness == "codex" {
		t.Fatalf("embedded registry did not load codex identity: %+v", got)
	}
}
```

- [ ] **Step 4: Run it, confirm it fails**

Run: `./build.sh --test 2>&1 | grep -E "scoreboard|undefined|FAIL"`
Expected: FAIL — `ParseRegistry` / `LoadRegistry` undefined.

- [ ] **Step 5: Implement `identity.go`**

Create `internal/scoreboard/identity.go`:

```go
// internal/scoreboard/identity.go
package scoreboard

import (
	"os"
	"strings"

	"github.com/BurntSushi/toml"
	ringer "github.com/corruptmemory/ringer"
)

type ModelIdentity struct {
	ModelDisplay string
	Harness      string
	Access       string
	Confidence   string
	Source       string
}

type Registry struct {
	identities map[[2]string]ModelIdentity // key: {engine, modelKey}
	defaults   map[string]string           // engine -> default model key
	engineMeta map[string]ModelIdentity    // engine -> meta identity
}

func txt(s string) string { return strings.TrimSpace(s) }

// Resolve is a verbatim port of ringer.py:4749-4780.
func (r Registry) Resolve(engine, modelKey string) ModelIdentity {
	engineKey := txt(engine)
	rawKey := txt(modelKey)
	lookup := rawKey
	if lookup == "" {
		lookup = r.defaults[engineKey]
	}
	if id, ok := r.identities[[2]string{engineKey, lookup}]; ok {
		return id
	}
	meta, hasMeta := r.engineMeta[engineKey]
	if engineKey == "opencode" && strings.HasPrefix(rawKey, "openrouter/") {
		harness, access := "OpenCode", "OpenRouter API"
		if hasMeta {
			harness, access = meta.Harness, meta.Access
		}
		return ModelIdentity{
			ModelDisplay: strings.TrimPrefix(rawKey, "openrouter/"),
			Harness:      harness, Access: access,
			Confidence: "fallback", Source: "unlisted OpenRouter slug",
		}
	}
	if hasMeta && lookup != "" {
		return ModelIdentity{
			ModelDisplay: lookup, Harness: meta.Harness, Access: meta.Access,
			Confidence: "fallback", Source: "engine default model key",
		}
	}
	unknown := engineKey
	if unknown == "" {
		unknown = "unknown"
	}
	return ModelIdentity{ModelDisplay: unknown, Harness: unknown, Access: "unknown", Confidence: "unknown"}
}

type registryTOML struct {
	Engines map[string]struct {
		Harness         string `toml:"harness"`
		Access          string `toml:"access"`
		DefaultModelKey string `toml:"default_model_key"`
		Models          map[string]struct {
			Display    string `toml:"display"`
			Confidence string `toml:"confidence"`
			Source     string `toml:"source"`
		} `toml:"models"`
	} `toml:"engines"`
}

// ParseRegistry ports ringer.py:4786-4833. Malformed input -> empty registry, nil error only for a decode failure that Python swallowed; we surface decode errors for tests but LoadRegistry ignores them.
func ParseRegistry(data []byte) (Registry, error) {
	reg := Registry{
		identities: map[[2]string]ModelIdentity{},
		defaults:   map[string]string{},
		engineMeta: map[string]ModelIdentity{},
	}
	var doc registryTOML
	if err := toml.Unmarshal(data, &doc); err != nil {
		return reg, err
	}
	for engineName, raw := range doc.Engines {
		engine := txt(engineName)
		if engine == "" {
			continue
		}
		harness := txt(raw.Harness)
		if harness == "" {
			harness = engine
		}
		access := txt(raw.Access)
		if access == "" {
			access = "unknown"
		}
		if dk := txt(raw.DefaultModelKey); dk != "" {
			reg.defaults[engine] = dk
		}
		reg.engineMeta[engine] = ModelIdentity{ModelDisplay: engine, Harness: harness, Access: access, Confidence: "engine"}
		for keyRaw, m := range raw.Models {
			key := txt(keyRaw)
			if key == "" {
				continue
			}
			display := txt(m.Display)
			if display == "" {
				display = key
			}
			reg.identities[[2]string{engine, key}] = ModelIdentity{
				ModelDisplay: display, Harness: harness, Access: access,
				Confidence: txt(m.Confidence), Source: txt(m.Source),
			}
		}
	}
	return reg, nil
}

// LoadRegistry loads the override file at overridePath, or the embedded
// registry when overridePath == "". A read/parse failure yields an empty
// registry (Python parity: analytics degrades to raw model keys, never crashes).
func LoadRegistry(overridePath string) Registry {
	data := ringer.ModelIdentityTOML
	if overridePath != "" {
		if b, err := os.ReadFile(overridePath); err == nil {
			data = b
		}
	}
	reg, err := ParseRegistry(data)
	if err != nil {
		return Registry{identities: map[[2]string]ModelIdentity{}, defaults: map[string]string{}, engineMeta: map[string]ModelIdentity{}}
	}
	return reg
}
```

- [ ] **Step 6: Run tests, confirm green**

Run: `./build.sh --test 2>&1 | grep -E "internal/scoreboard|internal/config|FAIL|ok"`
Expected: all `ok`. (The root `ringer` package now compiles under `go build ./...` inside build.sh; `internal/scoreboard` has no `store` dependency yet — that arrives in Task 5.)

- [ ] **Step 7: Commit**

```bash
git add assets.go internal/scoreboard/identity.go internal/scoreboard/identity_test.go internal/config/config.go
git commit -m "scoreboard: model-identity registry (embedded + override) + Resolve"
```

---

## Task 3: Catalog core — types, pricing, normalize, fetch

**Files:**
- Create: `internal/catalog/catalog.go`
- Create: `internal/catalog/catalog_test.go`

**Interfaces:**
- Consumes: stdlib + `net/http` only (leaf package).
- Produces:
  - `catalog.Model` (JSON-tagged, matches the normalized-model shape) with `ID, Name string; ContextLength int; Modality string; PromptPerM, CompletionPerM *float64; VariablePricing, PricingUnknown, Free bool; FetchedAt string`.
  - `catalog.Normalize(raw map[string]any, fetchedAt string) Model` (port ringer.py:1268-1306).
  - `catalog.NormalizePayload(payload map[string]any, fetchedAt string) ([]Model, error)` — payload `data` array → sorted `[]Model` (ringer.py:1309-1331); error if `data` missing/not a list.
  - `catalog.Fetch(source string, timeout time.Duration) (map[string]any, error)` — http/https GET (UA `ringer`) or local file → JSON object (ringer.py:1334-1344).
  - `catalog.LoadModelsFile(path string) ([]Model, error)` — tolerant loader for `--file`: accepts a raw payload (`{"data":[...]}`), a snapshot (`{"models":[...]}`), or a bare `[...]` of Model JSON.
  - `catalog.SortModels([]Model)` — the stable display order (variable last, then by summed price, then id).
  - Constants `catalog.DefaultSource = "https://openrouter.ai/api/v1/models"`, `catalog.FetchTimeout = 30 * time.Second`, `catalog.AutoRefreshMaxAge = 24 * time.Hour`.

- [ ] **Step 1: Write the failing normalize test**

Create `internal/catalog/catalog_test.go`:

```go
package catalog

import "testing"

func f(v float64) *float64 { return &v }

func TestNormalize(t *testing.T) {
	cases := []struct {
		name string
		raw  map[string]any
		want Model
	}{
		{"paid", map[string]any{"id": "a/b", "name": "A B", "context_length": 128000,
			"pricing": map[string]any{"prompt": "0.0000005", "completion": "0.0000015"},
			"architecture": map[string]any{"modality": "text"}},
			Model{ID: "a/b", Name: "A B", ContextLength: 128000, Modality: "text",
				PromptPerM: f(0.5), CompletionPerM: f(1.5), Free: false}},
		{"free-slug", map[string]any{"id": "x/y:free",
			"pricing": map[string]any{"prompt": "0", "completion": "0"}},
			Model{ID: "x/y:free", Name: "x/y:free", PromptPerM: f(0), CompletionPerM: f(0), Free: true}},
		{"unknown-pricing", map[string]any{"id": "u/v", "pricing": map[string]any{}},
			Model{ID: "u/v", Name: "u/v", PricingUnknown: true, VariablePricing: true}},
		{"negative-variable", map[string]any{"id": "n/g",
			"pricing": map[string]any{"prompt": "-1", "completion": "0.0000001"}},
			Model{ID: "n/g", Name: "n/g", VariablePricing: true}},
	}
	for _, c := range cases {
		got := Normalize(c.raw, "")
		if got.ID != c.want.ID || got.Name != c.want.Name || got.Free != c.want.Free ||
			got.VariablePricing != c.want.VariablePricing || got.PricingUnknown != c.want.PricingUnknown ||
			!eqp(got.PromptPerM, c.want.PromptPerM) || !eqp(got.CompletionPerM, c.want.CompletionPerM) {
			t.Errorf("%s: Normalize=%+v want %+v", c.name, got, c.want)
		}
	}
}

func eqp(a, b *float64) bool {
	if a == nil || b == nil {
		return a == b
	}
	return *a == *b
}
```

- [ ] **Step 2: Run it, confirm it fails**

Run: `./build.sh --test 2>&1 | grep -E "internal/catalog|undefined|FAIL"`
Expected: FAIL — `Normalize` undefined.

- [ ] **Step 3: Implement `catalog.go`**

Create `internal/catalog/catalog.go`:

```go
// internal/catalog/catalog.go
package catalog

import (
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/url"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"
)

const (
	DefaultSource     = "https://openrouter.ai/api/v1/models"
	FetchTimeout      = 30 * time.Second
	AutoRefreshMaxAge = 24 * time.Hour
)

type Model struct {
	ID              string   `json:"id"`
	Name            string   `json:"name"`
	ContextLength   int      `json:"context_length"`
	Modality        string   `json:"modality"`
	PromptPerM      *float64 `json:"prompt_per_m"`
	CompletionPerM  *float64 `json:"completion_per_m"`
	VariablePricing bool     `json:"variable_pricing"`
	PricingUnknown  bool     `json:"pricing_unknown"`
	Free            bool     `json:"free"`
	FetchedAt       string   `json:"fetched_at"`
}

// priceOrNil parses a raw price value ("" / nil / bad -> nil = unknown). A
// valid parse (including "0" and negatives) returns the float.
func priceOrNil(v any) *float64 {
	if v == nil {
		return nil
	}
	s := strings.TrimSpace(fmt.Sprintf("%v", v))
	if s == "" {
		return nil
	}
	x, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return nil
	}
	return &x
}

func str(v any) string {
	if v == nil {
		return ""
	}
	return strings.TrimSpace(fmt.Sprintf("%v", v))
}

func toMap(v any) map[string]any {
	if m, ok := v.(map[string]any); ok {
		return m
	}
	return map[string]any{}
}

// Normalize ports ringer.py:1268-1306.
func Normalize(raw map[string]any, fetchedAt string) Model {
	pricing := toMap(raw["pricing"])
	arch := toMap(raw["architecture"])
	id := str(raw["id"])
	prompt := priceOrNil(pricing["prompt"])
	completion := priceOrNil(pricing["completion"])
	pricingUnknown := prompt == nil || completion == nil
	variable := pricingUnknown || (prompt != nil && *prompt < 0) || (completion != nil && *completion < 0)
	var promptPerM, completionPerM *float64
	if !variable {
		p := *prompt * 1e6
		c := *completion * 1e6
		promptPerM, completionPerM = &p, &c
	}
	free := false
	if !variable {
		free = strings.HasSuffix(id, ":free") || (*prompt == 0 && *completion == 0)
	}
	if strings.HasSuffix(id, ":free") {
		free = true
	}
	ctx := 0
	if n, ok := asInt(raw["context_length"]); ok {
		ctx = n
	}
	name := str(raw["name"])
	if name == "" {
		name = id
	}
	return Model{
		ID: id, Name: name, ContextLength: ctx, Modality: str(arch["modality"]),
		PromptPerM: promptPerM, CompletionPerM: completionPerM,
		VariablePricing: variable, PricingUnknown: pricingUnknown, Free: free, FetchedAt: fetchedAt,
	}
}

func asInt(v any) (int, bool) {
	switch n := v.(type) {
	case float64:
		return int(n), true
	case int:
		return n, true
	case json.Number:
		if i, err := n.Int64(); err == nil {
			return int(i), true
		}
	case string:
		if i, err := strconv.Atoi(strings.TrimSpace(n)); err == nil {
			return i, true
		}
	}
	return 0, false
}

// NormalizePayload ports ringer.py:1309-1320: payload.data (list) -> sorted models.
func NormalizePayload(payload map[string]any, fetchedAt string) ([]Model, error) {
	data, ok := payload["data"].([]any)
	if !ok {
		return nil, fmt.Errorf("catalog source must be a JSON object with a data array")
	}
	models := make([]Model, 0, len(data))
	for _, item := range data {
		m, ok := item.(map[string]any)
		if !ok {
			continue
		}
		nm := Normalize(m, fetchedAt)
		if nm.ID != "" {
			models = append(models, nm)
		}
	}
	SortModels(models)
	return models, nil
}

// sumPrice mirrors catalog_sort_key: variable models sort last (Inf).
func sumPrice(m Model) float64 {
	if m.VariablePricing {
		return math.Inf(1)
	}
	var s float64
	if m.PromptPerM != nil {
		s += *m.PromptPerM
	}
	if m.CompletionPerM != nil {
		s += *m.CompletionPerM
	}
	return s
}

func SortModels(models []Model) {
	sort.SliceStable(models, func(i, j int) bool {
		vi, vj := models[i].VariablePricing, models[j].VariablePricing
		if vi != vj {
			return !vi // non-variable first
		}
		si, sj := sumPrice(models[i]), sumPrice(models[j])
		if si != sj {
			return si < sj
		}
		return models[i].ID < models[j].ID
	})
}

// Fetch ports ringer.py:1334-1344.
func Fetch(source string, timeout time.Duration) (map[string]any, error) {
	u, err := url.Parse(source)
	var body []byte
	if err == nil && (u.Scheme == "http" || u.Scheme == "https") {
		req, _ := http.NewRequest(http.MethodGet, source, nil)
		req.Header.Set("User-Agent", "ringer")
		client := &http.Client{Timeout: timeout}
		resp, err := client.Do(req)
		if err != nil {
			return nil, fmt.Errorf("catalog fetch %s: %w", source, err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			return nil, fmt.Errorf("catalog fetch %s: HTTP %d", source, resp.StatusCode)
		}
		body, err = io.ReadAll(resp.Body)
		if err != nil {
			return nil, err
		}
	} else {
		body, err = os.ReadFile(expandUser(source))
		if err != nil {
			return nil, err
		}
	}
	var payload map[string]any
	if err := json.Unmarshal(body, &payload); err != nil {
		return nil, fmt.Errorf("catalog source must be a JSON object: %w", err)
	}
	return payload, nil
}

// LoadModelsFile tolerantly reads --file: raw payload, snapshot, or bare list.
func LoadModelsFile(path string) ([]Model, error) {
	body, err := os.ReadFile(expandUser(path))
	if err != nil {
		return nil, err
	}
	var doc any
	if err := json.Unmarshal(body, &doc); err != nil {
		return nil, err
	}
	switch v := doc.(type) {
	case map[string]any:
		if _, ok := v["data"].([]any); ok {
			return NormalizePayload(v, str(v["fetched_at"]))
		}
		if models, ok := v["models"].([]any); ok {
			return modelsFromList(models), nil
		}
	case []any:
		return modelsFromList(v), nil
	}
	return nil, fmt.Errorf("unrecognized catalog file shape: %s", path)
}

func modelsFromList(list []any) []Model {
	out := make([]Model, 0, len(list))
	for _, item := range list {
		b, err := json.Marshal(item)
		if err != nil {
			continue
		}
		var m Model
		if err := json.Unmarshal(b, &m); err == nil && m.ID != "" {
			out = append(out, m)
		}
	}
	SortModels(out)
	return out
}

func expandUser(p string) string {
	if strings.HasPrefix(p, "~/") {
		if home, err := os.UserHomeDir(); err == nil {
			return home + p[1:]
		}
	}
	return p
}
```

- [ ] **Step 4: Run the normalize test, confirm green**

Run: `./build.sh --test 2>&1 | grep -E "internal/catalog|FAIL|ok"`
Expected: `ok ... internal/catalog`.

- [ ] **Step 5: Commit**

```bash
git add internal/catalog/catalog.go internal/catalog/catalog_test.go
git commit -m "catalog: Model + normalize + fetch + tolerant file loader"
```

---

## Task 4: Catalog diff + refresh + DB persistence

**Files:**
- Create: `internal/catalog/diff.go`
- Create: `internal/catalog/diff_test.go`
- Create: `internal/store/analytics.go` — catalog read/write methods (this file grows in Tasks 5 + 9)
- Create: `internal/catalog/refresh.go` (orchestration reading old models via a store interface)
- Test: `internal/catalog/refresh_test.go`, `internal/store/analytics_test.go`

**Interfaces:**
- Consumes: `catalog.Model` (Task 3); schema v2 `catalog_models` / `catalog_events` (Task 1).
- Produces:
  - `catalog.Event{TS, Kind, ModelID string; Payload map[string]any}` and `catalog.Diff(old, new []Model, ts string) []Event` (port ringer.py:1374-1478).
  - store methods:
    - `store.CatalogModels() ([]catalog.Model, error)` — all rows, display order.
    - `store.FreeCatalogModels() ([]catalog.Model, error)` — `WHERE free=1`.
    - `store.ReplaceCatalog(models []catalog.Model) error` — DELETE + INSERT in one tx.
    - `store.AppendCatalogEvents(events []catalog.Event) error` — INSERT (payload JSON-encoded).
    - `store.CatalogEvents(limit int) ([]catalog.Event, error)` — newest first (`ORDER BY id DESC LIMIT`).
  - `catalog.Refresh(s CatalogStore, source string, timeout time.Duration, now string) (Result, error)` where `CatalogStore` is `interface { CatalogModels() ([]Model, error); ReplaceCatalog([]Model) error; AppendCatalogEvents([]Event) error }` (satisfied by `*store.Store`); reads old models from the store (the DB **is** the snapshot — no JSON file), fetches new, diffs, replaces the table and appends events. `Result{Models []Model; Events []Event}`.

**Design note (deliberate deviations from ringer.py, banner for review):** (1) the DB `catalog_models` table is the catalog — there is **no** `~/.ringer/openrouter-catalog.json` snapshot file and **no** `.changes.jsonl`; the diff basis is the current table rows and change history is the `catalog_events` table (`catalog --changes` = a SELECT). This deletes Python's `sync_state` mtime/offset machinery (the same "derived-read-model" cut the rewrite already made for eval rows). (2) The refresh is a single SQLite transaction (read-old → replace → append-events), so Python's `fcntl` `catalog_refresh_lock` is unnecessary — `SetMaxOpenConns(1)` + `busy_timeout` serialize concurrent refreshers.

- [ ] **Step 1: Write the failing diff test**

Create `internal/catalog/diff_test.go`:

```go
package catalog

import "testing"

func TestDiff(t *testing.T) {
	old := []Model{
		{ID: "keep", PromptPerM: f(1), CompletionPerM: f(2)},
		{ID: "goes", PromptPerM: f(1), CompletionPerM: f(1)},
		{ID: "reprice", PromptPerM: f(1), CompletionPerM: f(1)},
		{ID: "tofree", PromptPerM: f(1), CompletionPerM: f(1)},
	}
	nw := []Model{
		{ID: "keep", PromptPerM: f(1), CompletionPerM: f(2)},       // unchanged -> no event
		{ID: "reprice", PromptPerM: f(3), CompletionPerM: f(1)},    // price_change
		{ID: "tofree", PromptPerM: f(0), CompletionPerM: f(0), Free: true}, // price_change + went_free
		{ID: "brand", PromptPerM: f(5), CompletionPerM: f(5)},      // added
	}
	events := Diff(old, nw, "T")
	kinds := map[string]int{}
	for _, e := range events {
		kinds[e.Kind+":"+e.ModelID]++
	}
	for _, want := range []string{"added:brand", "removed:goes", "price_change:reprice", "price_change:tofree", "went_free:tofree"} {
		if kinds[want] == 0 {
			t.Errorf("missing event %s; got %v", want, kinds)
		}
	}
	if kinds["price_change:keep"] != 0 {
		t.Errorf("unchanged model emitted an event")
	}
}
```

- [ ] **Step 2: Run it, confirm it fails**

Run: `./build.sh --test 2>&1 | grep -E "internal/catalog|undefined|FAIL"`
Expected: FAIL — `Diff` undefined.

- [ ] **Step 3: Implement `diff.go`**

Create `internal/catalog/diff.go` (port ringer.py:1374-1478; `pricing_variable`/`pricing_fixed` handle the variable↔fixed transitions):

```go
// internal/catalog/diff.go
package catalog

import "sort"

type Event struct {
	TS      string
	Kind    string
	ModelID string
	Payload map[string]any
}

func perM(p *float64) float64 {
	if p == nil {
		return 0
	}
	return *p
}

func modelDetails(m Model) map[string]any {
	return map[string]any{
		"name": m.Name, "prompt_per_m": perM(m.PromptPerM), "completion_per_m": perM(m.CompletionPerM),
		"variable_pricing": m.VariablePricing, "pricing_unknown": m.PricingUnknown,
		"free": m.Free, "context_length": m.ContextLength, "modality": m.Modality,
	}
}

// Diff ports ringer.py:1374-1478. Events carry old/new per-M prices in Payload.
func Diff(old, nw []Model, ts string) []Event {
	oldByID := map[string]Model{}
	newByID := map[string]Model{}
	for _, m := range old {
		if m.ID != "" {
			oldByID[m.ID] = m
		}
	}
	for _, m := range nw {
		if m.ID != "" {
			newByID[m.ID] = m
		}
	}
	var events []Event
	added, removed, common := keyDiff(oldByID, newByID)
	for _, id := range added {
		m := newByID[id]
		p := modelDetails(m)
		events = append(events, Event{TS: ts, Kind: "added", ModelID: id, Payload: p})
	}
	for _, id := range removed {
		m := oldByID[id]
		p := modelDetails(m)
		events = append(events, Event{TS: ts, Kind: "removed", ModelID: id, Payload: p})
	}
	for _, id := range common {
		o, n := oldByID[id], newByID[id]
		oldPrompt, newPrompt := perM(o.PromptPerM), perM(n.PromptPerM)
		oldComp, newComp := perM(o.CompletionPerM), perM(n.CompletionPerM)
		payload := func() map[string]any {
			return map[string]any{
				"name": pick(n.Name, o.Name),
				"old_prompt_per_m": oldPrompt, "new_prompt_per_m": newPrompt,
				"old_completion_per_m": oldComp, "new_completion_per_m": newComp,
				"old_free": o.Free, "new_free": n.Free,
			}
		}
		if n.VariablePricing {
			if !o.VariablePricing {
				events = append(events, Event{TS: ts, Kind: "pricing_variable", ModelID: id, Payload: payload()})
			}
			continue
		}
		if o.VariablePricing {
			events = append(events, Event{TS: ts, Kind: "pricing_fixed", ModelID: id, Payload: payload()})
			if n.Free {
				events = append(events, Event{TS: ts, Kind: "went_free", ModelID: id, Payload: payload()})
			}
			continue
		}
		if oldPrompt != newPrompt || oldComp != newComp {
			events = append(events, Event{TS: ts, Kind: "price_change", ModelID: id, Payload: payload()})
		}
		if o.Free != n.Free {
			kind := "went_paid"
			if n.Free {
				kind = "went_free"
			}
			events = append(events, Event{TS: ts, Kind: kind, ModelID: id, Payload: payload()})
		}
	}
	return events
}

func keyDiff(oldByID, newByID map[string]Model) (added, removed, common []string) {
	for id := range newByID {
		if _, ok := oldByID[id]; !ok {
			added = append(added, id)
		} else {
			common = append(common, id)
		}
	}
	for id := range oldByID {
		if _, ok := newByID[id]; !ok {
			removed = append(removed, id)
		}
	}
	sort.Strings(added)
	sort.Strings(removed)
	sort.Strings(common)
	return
}

func pick(a, b string) string {
	if a != "" {
		return a
	}
	return b
}
```

- [ ] **Step 4: Implement the store catalog methods**

Create `internal/store/analytics.go`:

```go
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
```

- [ ] **Step 5: Implement `refresh.go`**

Create `internal/catalog/refresh.go`:

```go
// internal/catalog/refresh.go
package catalog

import "time"

// CatalogStore is the store surface Refresh needs (satisfied by *store.Store).
type CatalogStore interface {
	CatalogModels() ([]Model, error)
	ReplaceCatalog([]Model) error
	AppendCatalogEvents([]Event) error
}

type Result struct {
	Models []Model
	Events []Event
}

// Refresh fetches the catalog from source, diffs it against the current
// table, appends change events, and replaces the table. The DB is the
// snapshot; there is no JSON file. Events are appended before the table is
// replaced so a crash duplicates (recoverable) rather than loses events.
func Refresh(s CatalogStore, source string, timeout time.Duration, now string) (Result, error) {
	old, err := s.CatalogModels()
	if err != nil {
		return Result{}, err
	}
	payload, err := Fetch(source, timeout)
	if err != nil {
		return Result{}, err
	}
	models, err := NormalizePayload(payload, now)
	if err != nil {
		return Result{}, err
	}
	events := Diff(old, models, now)
	if err := s.AppendCatalogEvents(events); err != nil {
		return Result{}, err
	}
	if err := s.ReplaceCatalog(models); err != nil {
		return Result{}, err
	}
	return Result{Models: models, Events: events}, nil
}
```

- [ ] **Step 6: Write the refresh test (fake store — no import cycle)**

`store` imports `catalog`, so a `catalog` test cannot import `store`. Test `Refresh`'s orchestration against a fake `CatalogStore`; the real-DB persistence is covered by store-side tests of `ReplaceCatalog`/`CatalogModels` (add one there too, Step 6b). Create `internal/catalog/refresh_test.go`:

```go
package catalog

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

type fakeStore struct {
	models []Model
	events []Event
}

func (f *fakeStore) CatalogModels() ([]Model, error)   { return f.models, nil }
func (f *fakeStore) ReplaceCatalog(m []Model) error     { f.models = m; return nil }
func (f *fakeStore) AppendCatalogEvents(e []Event) error { f.events = append(f.events, e...); return nil }

func writePayload(t *testing.T, dir, prompt string) string {
	t.Helper()
	p := filepath.Join(dir, "cat.json")
	body := `{"data":[{"id":"a/b","name":"A","context_length":1000,"pricing":{"prompt":"` + prompt + `","completion":"0.000001"},"architecture":{"modality":"text"}}]}`
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestRefreshDiffsAndReplaces(t *testing.T) {
	dir := t.TempDir()
	fs := &fakeStore{}

	res1, err := Refresh(fs, writePayload(t, dir, "0.000001"), time.Second, "T1")
	if err != nil {
		t.Fatal(err)
	}
	if len(res1.Models) != 1 || len(fs.models) != 1 || fs.models[0].ID != "a/b" {
		t.Fatalf("first refresh: models=%+v", fs.models)
	}

	res2, err := Refresh(fs, writePayload(t, dir, "0.000009"), time.Second, "T2")
	if err != nil {
		t.Fatal(err)
	}
	var sawPriceChange bool
	for _, e := range res2.Events {
		if e.Kind == "price_change" && e.ModelID == "a/b" {
			sawPriceChange = true
		}
	}
	if !sawPriceChange {
		t.Fatalf("second refresh did not emit price_change: %+v", res2.Events)
	}
}
```

- [ ] **Step 6b: Add store-side catalog round-trip coverage**

In `internal/store/analytics_test.go` (create), assert `ReplaceCatalog` → `CatalogModels`/`FreeCatalogModels` and `AppendCatalogEvents` → `CatalogEvents` round-trip a `catalog.Model{Free:true}` and an `Event`:

```go
package store

import (
	"path/filepath"
	"testing"

	"github.com/corruptmemory/ringer/internal/catalog"
)

func TestCatalogRoundTrip(t *testing.T) {
	s, err := Open(filepath.Join(t.TempDir(), "c.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	pm := 1.0
	if err := s.ReplaceCatalog([]catalog.Model{{ID: "a/b", Name: "A", PromptPerM: &pm, CompletionPerM: &pm}, {ID: "z/free", Free: true}}); err != nil {
		t.Fatal(err)
	}
	all, _ := s.CatalogModels()
	if len(all) != 2 {
		t.Fatalf("want 2 models, got %d", len(all))
	}
	free, _ := s.FreeCatalogModels()
	if len(free) != 1 || free[0].ID != "z/free" {
		t.Fatalf("free filter wrong: %+v", free)
	}
	if err := s.AppendCatalogEvents([]catalog.Event{{TS: "T", Kind: "added", ModelID: "a/b", Payload: map[string]any{"free": false}}}); err != nil {
		t.Fatal(err)
	}
	evs, _ := s.CatalogEvents(10)
	if len(evs) != 1 || evs[0].Kind != "added" {
		t.Fatalf("events round-trip wrong: %+v", evs)
	}
}
```

- [ ] **Step 7: Run tests, confirm green**

Run: `./build.sh --test 2>&1 | grep -E "internal/catalog|internal/store|FAIL|ok"`
Expected: all `ok`.

- [ ] **Step 8: Commit**

```bash
git add internal/catalog/ internal/store/analytics.go
git commit -m "catalog: diff + refresh into catalog_models/catalog_events (DB is the snapshot)"
```

---

## Task 5: Scoreboard — the SQL query + tiers/cost + Go identity

**Files:**
- Modify: `internal/store/analytics.go` — `ScoreboardModelRows`, `ScoreboardTaskTypeRows`, filter/row types
- Create: `internal/scoreboard/scoreboard.go` — `Scoreboard(s, Filter, Registry)`, `Row`, `TaskTypeRow`
- Test: `internal/store/scoreboard_query_test.go`, `internal/scoreboard/scoreboard_test.go`

**Interfaces:**
- Consumes: `attempts` table; `median()` UDF (Task 1); `catalog_models` (Task 1); `scoreboard.Registry`/`Resolve` (Task 2).
- Produces:
  - `store.ScoreFilter{TaskType, Model, Engine, Since string}` (each `""` = no filter).
  - `store.ScoreModelRow{Model, Engine, Tier string; Tasks, Attempts, Retries, Passed, Failed int; FirstTryPassRate, PassRate float64; MedianDurationS, MedianTokensF *float64; LastSeen string; Cost *float64}` — `Engine` is the latest attempt's engine (for identity); `Cost` nil = unknown/variable/no-catalog.
  - `store.ScoreGroupRow` (rich per-(model, task_type): counts, rates, `MedianDurationS`/`MedianTokensF *float64`, `Engine` = latest attempt's engine).
  - `store.ScoreboardModelRows(f ScoreFilter) ([]ScoreModelRow, error)` (ORDER BY tier/first/pass/cost/model, in SQL) and `store.ScoreboardGroupRows(f ScoreFilter) ([]ScoreGroupRow, error)` (ORDER BY task_type, pass_rate DESC, first_rate DESC, model — the `models` table order).
  - `scoreboard.Filter` (= the four filter strings); `scoreboard.Group` (the store group row + resolved `ModelDisplay/Harness/Access` + floored `MedianTokens *int64`) and `scoreboard.Groups(s, f, reg) ([]Group, error)` — the flat CLI/HUD groups; `scoreboard.Row` (rollup: the store model row **plus** `ModelDisplay, Harness, Access string; MedianTokens *int64; TaskTypes []TaskTypeRow`), `scoreboard.TaskTypeRow` (lean); `scoreboard.Scoreboard(s, f, reg) ([]Row, error)` — the tiered rollup with identity resolved and nested (lean) task-types built from the group rows (re-sorted per model by `-tasks, task_type`). Both preserve their SQL/Python order.

**Task-instance model (faithful to Python's `group_model_log_tasks`):** the runner writes ≤2 attempts per `(run_id, task_key)` in id-order (attempt 1 `retry=0`, then optionally `retry=1`), so a task-instance = the `(run_id, task_key)` group; `MIN(id)` = first attempt, `MAX(id)` = final. `model` = `TRIM(final.model)` or `TRIM(final.engine)`; `task_type` = `TRIM(final.task_type)` or `'(untyped)'`. Duration median is over per-instance **final** `duration_s`; token median is over **every** attempt row's `tokens` (excluding `-1` = unknown), attributed to the instance's final model. Cost lookup keys `catalog_models.id = model` **exactly** (no `openrouter/`-strip — Python does not strip either, ringer.py:5831); mismatches → `Cost` nil (faithful; document that opencode slugs rarely match a catalog id).

- [ ] **Step 1: Write the failing scoreboard query test (store side)**

Create `internal/store/scoreboard_query_test.go`. Seed attempts covering: a proven model (3 tasks, one with a retry that then passes), a probation model (1 failing task); assert counts, first-try vs pass rates, tier, retries, ordering:

```go
package store

import (
	"path/filepath"
	"testing"
)

func seed(t *testing.T, s *Store, rows []Attempt) {
	t.Helper()
	for _, a := range rows {
		if err := s.InsertAttempt(a); err != nil {
			t.Fatal(err)
		}
	}
}

func TestScoreboardModelRows(t *testing.T) {
	s, err := Open(filepath.Join(t.TempDir(), "sb.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	// proven: model M (codex), 3 task-instances. Task t2 fails attempt-1 then passes attempt-2 (retry).
	seed(t, s, []Attempt{
		{RunID: "r1", TaskKey: "t1", Engine: "codex", Model: "M", TaskType: "code", Verdict: "PASS", Retry: 0, DurationS: 10, Tokens: 100, CreatedAt: "2026-07-10T00:00:01Z"},
		{RunID: "r1", TaskKey: "t2", Engine: "codex", Model: "M", TaskType: "code", Verdict: "FAIL", Retry: 0, DurationS: 5, Tokens: 50, CreatedAt: "2026-07-10T00:00:02Z"},
		{RunID: "r1", TaskKey: "t2", Engine: "codex", Model: "M", TaskType: "code", Verdict: "PASS", Retry: 1, DurationS: 8, Tokens: 60, CreatedAt: "2026-07-10T00:00:03Z"},
		{RunID: "r1", TaskKey: "t3", Engine: "codex", Model: "M", TaskType: "docs", Verdict: "PASS", Retry: 0, DurationS: 20, Tokens: 200, CreatedAt: "2026-07-10T00:00:04Z"},
		// probation: model N (grok), 1 failing task-instance.
		{RunID: "r2", TaskKey: "u1", Engine: "grok", Model: "N", TaskType: "code", Verdict: "FAIL", Retry: 0, DurationS: 3, Tokens: 30, CreatedAt: "2026-07-10T00:00:05Z"},
	})
	rows, err := s.ScoreboardModelRows(ScoreFilter{})
	if err != nil {
		t.Fatal(err)
	}
	byModel := map[string]ScoreModelRow{}
	for _, r := range rows {
		byModel[r.Model] = r
	}
	m := byModel["M"]
	if m.Tier != "proven" || m.Tasks != 3 || m.Attempts != 4 || m.Retries != 1 {
		t.Fatalf("M rollup wrong: %+v", m)
	}
	if m.Passed != 3 || m.Failed != 0 { // t2's FINAL verdict is PASS
		t.Fatalf("M pass/fail wrong: %+v", m)
	}
	if m.FirstTryPassRate != 2.0/3.0 { // t1,t3 first-try pass; t2 first-try fail
		t.Fatalf("M first-try rate wrong: %v", m.FirstTryPassRate)
	}
	if m.Engine != "codex" {
		t.Fatalf("M latest engine wrong: %q", m.Engine)
	}
	n := byModel["N"]
	if n.Tier != "probation" || n.PassRate != 0 {
		t.Fatalf("N rollup wrong: %+v", n)
	}
	// proven sorts before probation.
	if rows[0].Model != "M" {
		t.Fatalf("ordering: proven M must precede probation N, got %q first", rows[0].Model)
	}
}
```

- [ ] **Step 2: Run it, confirm it fails**

Run: `./build.sh --test 2>&1 | grep -E "internal/store|undefined|FAIL"`
Expected: FAIL — `ScoreboardModelRows` undefined.

- [ ] **Step 3: Implement the store scoreboard queries**

Append to `internal/store/analytics.go`. The CTE prefix is shared; keep it in a const:

```go
type ScoreFilter struct{ TaskType, Model, Engine, Since string }

type ScoreModelRow struct {
	Model, Engine, Tier             string
	Tasks, Attempts, Retries        int
	Passed, Failed                  int
	FirstTryPassRate, PassRate      float64
	MedianDurationS, MedianTokensF  *float64
	LastSeen                        string
	Cost                            *float64
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
```

- [ ] **Step 4: Run the store query test, confirm green**

Run: `./build.sh --test 2>&1 | grep -E "internal/store|FAIL|ok"`
Expected: `ok ... internal/store`.

- [ ] **Step 5: Write the scoreboard-package failing test**

Create `internal/scoreboard/scoreboard_test.go` — seed attempts, assert identity resolution + nested task-types on the assembled `Row`:

```go
package scoreboard

import (
	"path/filepath"
	"testing"

	"github.com/corruptmemory/ringer/internal/store"
)

func TestScoreboardResolvesIdentityAndNests(t *testing.T) {
	s, err := store.Open(filepath.Join(t.TempDir(), "sb.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	for _, a := range []store.Attempt{
		{RunID: "r1", TaskKey: "t1", Engine: "codex", Model: "gpt-5.5", TaskType: "code", Verdict: "PASS", Tokens: 100, CreatedAt: "2026-07-10T00:00:01Z"},
		{RunID: "r1", TaskKey: "t2", Engine: "codex", Model: "gpt-5.5", TaskType: "docs", Verdict: "PASS", Tokens: 100, CreatedAt: "2026-07-10T00:00:02Z"},
	} {
		if err := s.InsertAttempt(a); err != nil {
			t.Fatal(err)
		}
	}
	reg := LoadRegistry("") // embedded registry maps codex/gpt-5.5 -> "GPT-5.5"
	rows, err := Scoreboard(s, Filter{}, reg)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 {
		t.Fatalf("want 1 model row, got %d", len(rows))
	}
	r := rows[0]
	if r.ModelDisplay != "GPT-5.5" || r.Harness != "Codex CLI" {
		t.Fatalf("identity not resolved: display=%q harness=%q", r.ModelDisplay, r.Harness)
	}
	if len(r.TaskTypes) != 2 {
		t.Fatalf("want 2 task-type breakdowns, got %d", len(r.TaskTypes))
	}
	if r.MedianTokens == nil || *r.MedianTokens != 100 {
		t.Fatalf("median tokens wrong: %v", r.MedianTokens)
	}
}
```

- [ ] **Step 6: Implement `scoreboard.go`**

Create `internal/scoreboard/scoreboard.go`:

```go
// internal/scoreboard/scoreboard.go
package scoreboard

import (
	"math"
	"sort"

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
```

- [ ] **Step 7: Run tests, confirm green**

Run: `./build.sh --test 2>&1 | grep -E "internal/scoreboard|internal/store|FAIL|ok"`
Expected: all `ok`.

- [ ] **Step 8: Commit**

```bash
git add internal/store/analytics.go internal/store/scoreboard_query_test.go internal/scoreboard/scoreboard.go internal/scoreboard/scoreboard_test.go
git commit -m "scoreboard: SQL aggregation (tiers/cost/median UDF) + Go identity resolution"
```

---

## Task 6: Model notes (embedded + fuzzy match)

**Files:**
- Create: `internal/scoreboard/notes.go`
- Create: `internal/scoreboard/notes_test.go`

**Interfaces:**
- Consumes: the root `ringer.ModelNotesMD` embed (Task 2); `config.ModelNotesPath()`.
- Produces:
  - `scoreboard.NoteSection{Heading string; Bullets []string}` (ordered — the earliest-section tiebreak needs order).
  - `scoreboard.ParseNotesSections(text string) []NoteSection` (port ringer.py:5537-5586: `##` headings; `- ` bullets with indented/blank continuations; keep only bullets containing a `\d{4}-\d{2}-\d{2}` date).
  - `scoreboard.JudgmentNotes(model string, sections []NoteSection) []string` (port ringer.py:5589-5604: normalized (ws-collapsed, lowercased) heading fuzzy-match with `A-Za-z0-9._/:-` word boundaries; best = exact, then longest heading, then earliest section).
  - `scoreboard.LoadNotes(overridePath string) []NoteSection` — override file or embedded default.
  - `scoreboard.RenderedNote{Date, Body string}` and `scoreboard.RenderNotes(model string, sections []NoteSection, limit int) []RenderedNote` — normalize a bullet to `(short humanized date, markdown-stripped body)`, newest date first, capped (for `--html` + HUD).

**Go note (no lookaround):** Go's `regexp` has no lookbehind/lookahead, so `JudgmentNotes` implements Python's `(?<![b])needle(?![b])` boundary check by scanning for each occurrence of `needle` in the normalized heading and testing the adjacent runes against the boundary set `A-Za-z0-9._/:-` manually.

- [ ] **Step 1: Write the failing notes test**

Create `internal/scoreboard/notes_test.go`:

```go
package scoreboard

import "testing"

const notesMD = `# notes

## codex (GPT-5-class)
- 2026-07-05 — carried the heavy lanes, clean first-attempt passes.
- no date here, should be dropped
- 2026-07-06 — passed on attempt 1, ~85k tokens.

## glm-5.2 via opencode (` + "`openrouter/z-ai/glm-5.2`" + `)
- 2026-07-07 — solid on refactors.
`

func TestParseAndMatchNotes(t *testing.T) {
	secs := ParseNotesSections(notesMD)
	if len(secs) != 2 {
		t.Fatalf("want 2 sections, got %d", len(secs))
	}
	// codex heading has 2 dated bullets (the undated one is dropped).
	codex := JudgmentNotes("codex", secs)
	if len(codex) != 2 {
		t.Fatalf("codex notes: want 2 dated bullets, got %d: %v", len(codex), codex)
	}
	// fuzzy match on the slug inside the glm heading.
	glm := JudgmentNotes("openrouter/z-ai/glm-5.2", secs)
	if len(glm) != 1 {
		t.Fatalf("glm notes: want 1, got %d", len(glm))
	}
	// unknown model -> no notes.
	if got := JudgmentNotes("nonesuch", secs); len(got) != 0 {
		t.Fatalf("unknown model returned notes: %v", got)
	}
	// render: date humanized + markdown/leading-dash stripped.
	rn := RenderNotes("codex", secs, 5)
	if len(rn) != 2 || rn[0].Date == "" || rn[0].Body == "" {
		t.Fatalf("RenderNotes wrong: %+v", rn)
	}
}
```

- [ ] **Step 2: Run it, confirm it fails**

Run: `./build.sh --test 2>&1 | grep -E "internal/scoreboard|undefined|FAIL"`
Expected: FAIL — `ParseNotesSections` undefined.

- [ ] **Step 3: Implement `notes.go`**

Create `internal/scoreboard/notes.go`:

```go
// internal/scoreboard/notes.go
package scoreboard

import (
	"os"
	"regexp"
	"sort"
	"strings"

	ringer "github.com/corruptmemory/ringer"
)

type NoteSection struct {
	Heading string
	Bullets []string
}

var (
	reWS       = regexp.MustCompile(`\s+`)
	reDate     = regexp.MustCompile(`\b\d{4}-\d{2}-\d{2}\b`)
	reHeading  = regexp.MustCompile(`^##\s+(.+?)\s*$`)
	reInlineBT = regexp.MustCompile("`([^`]*)`")
	reLink     = regexp.MustCompile(`\[([^\]]+)\]\([^)]+\)`)
	reEmph     = regexp.MustCompile(`[*_]{1,3}([^*_]+)[*_]{1,3}`)
	reNoteHead = regexp.MustCompile(`^-?\s*(\d{4}-\d{2}-\d{2})\s+(?:[-\x{2013}\x{2014}]+\s*)?(.*)$`)
)

func normalizeMatch(s string) string { return strings.ToLower(strings.TrimSpace(reWS.ReplaceAllString(s, " "))) }

// ParseNotesSections ports ringer.py:5537-5586.
func ParseNotesSections(text string) []NoteSection {
	var sections []NoteSection
	var heading string
	haveHeading := false
	var bullets []string
	var active []string
	flushBullet := func() {
		if active != nil {
			t := strings.TrimSpace(strings.Join(active, "\n"))
			if reDate.MatchString(t) {
				bullets = append(bullets, t)
			}
			active = nil
		}
	}
	flushSection := func() {
		flushBullet()
		if haveHeading {
			sections = append(sections, NoteSection{Heading: heading, Bullets: append([]string(nil), bullets...)})
		}
	}
	for _, line := range strings.Split(text, "\n") {
		if m := reHeading.FindStringSubmatch(line); m != nil {
			flushSection()
			heading = strings.TrimSpace(m[1])
			haveHeading = true
			bullets = nil
			active = nil
			continue
		}
		if !haveHeading {
			continue
		}
		if strings.HasPrefix(line, "- ") {
			flushBullet()
			active = []string{strings.TrimSpace(line[2:])}
			continue
		}
		if active != nil && (strings.HasPrefix(line, "  ") || strings.TrimSpace(line) == "") {
			active = append(active, strings.TrimSpace(line))
			continue
		}
		flushBullet()
	}
	flushSection()
	return sections
}

const noteBoundary = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789._/:-"

func isBoundaryByte(b byte) bool { return strings.IndexByte(noteBoundary, b) >= 0 }

// matchesNeedle reports whether needle occurs in text delimited by non-boundary bytes.
func matchesNeedle(text, needle string) bool {
	if needle == "" {
		return false
	}
	for i := 0; ; {
		j := strings.Index(text[i:], needle)
		if j < 0 {
			return false
		}
		start := i + j
		end := start + len(needle)
		before := start == 0 || !isBoundaryByte(text[start-1])
		after := end == len(text) || !isBoundaryByte(text[end])
		if before && after {
			return true
		}
		i = start + 1
	}
}

// JudgmentNotes ports ringer.py:5589-5604.
func JudgmentNotes(model string, sections []NoteSection) []string {
	needle := normalizeMatch(model)
	if needle == "" {
		return nil
	}
	bestIdx := -1
	var bestExact, bestLen int
	for idx, sec := range sections {
		nh := normalizeMatch(sec.Heading)
		if !matchesNeedle(nh, needle) {
			continue
		}
		exact := 0
		if nh == needle {
			exact = 1
		}
		// best = higher exact, then longer heading, then earlier section (-index).
		if bestIdx == -1 || exact > bestExact || (exact == bestExact && len(nh) > bestLen) {
			bestIdx, bestExact, bestLen = idx, exact, len(nh)
		}
	}
	if bestIdx == -1 {
		return nil
	}
	return sections[bestIdx].Bullets
}

// LoadNotes returns the override notes file, or the embedded default.
func LoadNotes(overridePath string) []NoteSection {
	data := ringer.ModelNotesMD
	if overridePath != "" {
		if b, err := os.ReadFile(overridePath); err == nil {
			data = b
		}
	}
	return ParseNotesSections(string(data))
}

type RenderedNote struct{ Date, Body string }

func stripInlineMarkdown(s string) string {
	s = reInlineBT.ReplaceAllString(s, "$1")
	s = reLink.ReplaceAllString(s, "$1")
	s = reEmph.ReplaceAllString(s, "$1")
	return strings.TrimSpace(reWS.ReplaceAllString(s, " "))
}

// RenderNotes ports normalized_judgment_note + ordering (ringer.py:5614-5656),
// newest date first, capped at limit.
func RenderNotes(model string, sections []NoteSection, limit int) []RenderedNote {
	items := JudgmentNotes(model, sections)
	sort.SliceStable(items, func(i, j int) bool { return noteDateKey(items[i]) > noteDateKey(items[j]) })
	var out []RenderedNote
	for _, it := range items {
		if len(out) >= limit {
			break
		}
		m := reNoteHead.FindStringSubmatch(reWS.ReplaceAllString(it, " "))
		if m == nil {
			if body := stripInlineMarkdown(it); body != "" {
				out = append(out, RenderedNote{Body: body})
			}
			continue
		}
		body := stripInlineMarkdown(m[2])
		if body == "" {
			continue
		}
		out = append(out, RenderedNote{Date: humanizeShortDate(m[1]), Body: body})
	}
	return out
}

func noteDateKey(s string) string { return reDate.FindString(s) }

// humanizeShortDate renders 2026-07-06 -> "July 6" (ringer.py humanized_log_date, year stripped).
func humanizeShortDate(iso string) string {
	if len(iso) < 10 {
		return iso
	}
	// iso = YYYY-MM-DD; month names by index.
	months := []string{"January", "February", "March", "April", "May", "June", "July", "August", "September", "October", "November", "December"}
	y, mo, d := iso[:4], iso[5:7], iso[8:10]
	mi := int(mo[0]-'0')*10 + int(mo[1]-'0')
	if mi < 1 || mi > 12 {
		return iso
	}
	day := strings.TrimPrefix(d, "0")
	_ = y
	return months[mi-1] + " " + day
}
```

- [ ] **Step 4: Run tests, confirm green**

Run: `./build.sh --test 2>&1 | grep -E "internal/scoreboard|FAIL|ok"`
Expected: `ok ... internal/scoreboard`.

- [ ] **Step 5: Commit**

```bash
git add internal/scoreboard/notes.go internal/scoreboard/notes_test.go
git commit -m "scoreboard: MODEL-NOTES parse + fuzzy judgment-note match (embedded + override)"
```

---

## Task 7: `models` subcommand

**Files:**
- Create: `cmd/ringer/models.go`
- Create: `cmd/ringer/models_test.go`
- Create: `internal/hud/views/models_scoreboard.templ` — the `--html` page
- Modify: `internal/scoreboard/scoreboard.go` — add `ExploreCandidates` helper (untested catalog models)

**Interfaces:**
- Consumes: `scoreboard.Groups`/`Scoreboard`/`LoadRegistry`/`LoadNotes`; `store.Open`, `store.CatalogModels`; `config`.
- Produces: `ringer models` with `--task-type --model --engine --since --explore --html [PATH] --open --json`. Output shape is Go-authoritative (golden-locked; §9.6 freezes only that these flags exist, not the bytes).
  - `scoreboard.ExploreCandidates(models []catalog.Model, tested map[string]bool) []catalog.Model` — untested, text→text modality, `context_length >= 32000`, sorted by SortModels (port `catalog_explore_candidates`).

- [ ] **Step 1: Write the failing command test**

Create `cmd/ringer/models_test.go` — seed a store via the runner's public API is heavy; instead seed with `store.InsertAttempt` and drive the command's inner render function. Structure `models.go` so its rendering is a pure function `renderModelsTable(w io.Writer, groups []scoreboard.Group)` testable without a full command:

```go
package main

import (
	"bytes"
	"strings"
	"testing"

	"github.com/corruptmemory/ringer/internal/scoreboard"
	"github.com/corruptmemory/ringer/internal/store"
)

func TestRenderModelsTable(t *testing.T) {
	var buf bytes.Buffer
	renderModelsTable(&buf, []scoreboard.Group{{
		ScoreGroupRow: store.ScoreGroupRow{Model: "gpt-5.5", TaskType: "code", Tasks: 3, Attempts: 4, Passed: 3, Failed: 0, FirstTryPassRate: 0.67, PassRate: 1.0, LastSeen: "2026-07-10T00:00:00Z"},
		ModelDisplay:  "GPT-5.5", Harness: "Codex CLI",
	}})
	out := buf.String()
	for _, want := range []string{"code", "GPT-5.5", "gpt-5.5", "Codex CLI", "task_type"} {
		if !strings.Contains(out, want) {
			t.Fatalf("models table missing %q:\n%s", want, out)
		}
	}
}
```

- [ ] **Step 2: Run it, confirm it fails**

Run: `./build.sh --test 2>&1 | grep -E "cmd/ringer|undefined|FAIL"`
Expected: FAIL — `renderModelsTable` undefined.

- [ ] **Step 3: Implement `models.go`**

Create `cmd/ringer/models.go`. Table columns (Go-authoritative): `task_type model(display+slug) harness tasks attempts passed failed pass first dur_ms tokens last_seen`. Percentages via `fmt` `%.2f`; median duration shown in ms (`*MedianDurationS * 1000`, rounded); blank when nil.

```go
// cmd/ringer/models.go
package main

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"runtime"
	"text/tabwriter"

	"github.com/corruptmemory/ringer/internal/catalog"
	"github.com/corruptmemory/ringer/internal/config"
	"github.com/corruptmemory/ringer/internal/scoreboard"
	"github.com/corruptmemory/ringer/internal/store"
)

type modelsCmd struct {
	TaskType string `long:"task-type"`
	Model    string `long:"model"`
	Engine   string `long:"engine"`
	Since    string `long:"since" description:"only tasks whose latest attempt is on/after YYYY-MM-DD"`
	Explore  bool   `long:"explore" description:"tiers + untested catalog candidates"`
	HTML     string `long:"html" optional:"yes" optional-value:"" description:"write the scoreboard HTML (default path if empty)"`
	Open     bool   `long:"open" description:"open the written HTML"`
	JSON     bool   `long:"json"`
}

func (c *modelsCmd) Execute(args []string) error {
	cfg, err := loadConfig()
	if err != nil {
		return err
	}
	s, err := store.Open(cfg.DBPath())
	if err != nil {
		return err
	}
	defer s.Close()
	reg := scoreboard.LoadRegistry(cfg.ModelIdentityPath())
	f := scoreboard.Filter{TaskType: c.TaskType, Model: c.Model, Engine: c.Engine, Since: c.Since}

	if c.JSON {
		rollup, err := scoreboard.Scoreboard(s, f, reg)
		if err != nil {
			return err
		}
		groups, err := scoreboard.Groups(s, f, reg)
		if err != nil {
			return err
		}
		return json.NewEncoder(os.Stdout).Encode(map[string]any{"rollup": rollup, "groups": groups})
	}
	if c.Explore {
		return runExplore(os.Stdout, s, f, reg)
	}
	if c.HTMLRequested() {
		return c.writeHTML(cfg, s, f, reg)
	}
	groups, err := scoreboard.Groups(s, f, reg)
	if err != nil {
		return err
	}
	renderModelsTable(os.Stdout, groups)
	return nil
}

func (c *modelsCmd) HTMLRequested() bool { return c.HTML != "" || c.Open }

func renderModelsTable(w io.Writer, groups []scoreboard.Group) {
	tw := tabwriter.NewWriter(w, 0, 2, 2, ' ', 0)
	fmt.Fprintln(tw, "task_type\tmodel\tharness\ttasks\tattempts\tpassed\tfailed\tpass\tfirst\tdur_ms\ttokens\tlast_seen")
	for _, g := range groups {
		display := g.ModelDisplay
		if display != g.Model && display != "" {
			display = fmt.Sprintf("%s (%s)", display, g.Model)
		}
		harness := g.Harness
		if harness == "" {
			harness = "unknown"
		}
		fmt.Fprintf(tw, "%s\t%s\t%s\t%d\t%d\t%d\t%d\t%.2f\t%.2f\t%s\t%s\t%s\n",
			g.TaskType, display, harness, g.Tasks, g.Attempts, g.Passed, g.Failed,
			g.PassRate, g.FirstTryPassRate, msString(g.MedianDurationS), tokString(g.MedianTokens), g.LastSeen)
	}
	tw.Flush()
	fmt.Fprintln(w, "Judgment layer: docs/MODEL-NOTES.md")
}

func msString(sec *float64) string {
	if sec == nil {
		return ""
	}
	return fmt.Sprintf("%d", int64(*sec*1000+0.5))
}
func tokString(t *int64) string {
	if t == nil {
		return ""
	}
	return fmt.Sprintf("%d", *t)
}

func runExplore(w io.Writer, s *store.Store, f scoreboard.Filter, reg scoreboard.Registry) error {
	groups, err := scoreboard.Groups(s, f, reg)
	if err != nil {
		return err
	}
	fmt.Fprintln(w, "TIERS")
	if len(groups) == 0 {
		fmt.Fprintln(w, "  no local evidence")
	}
	tested := map[string]bool{}
	for _, g := range groups {
		label := "probation"
		if g.Tasks >= 3 {
			label = "proven"
		}
		fmt.Fprintf(w, "  %-9s %s task_type=%s tasks=%d first=%.2f pass=%.2f\n", label, g.Model, g.TaskType, g.Tasks, g.FirstTryPassRate, g.PassRate)
		tested[g.Model] = true
	}
	models, err := s.CatalogModels()
	if err != nil {
		return err
	}
	fmt.Fprintln(w, "CANDIDATES")
	cands := scoreboard.ExploreCandidates(models, tested)
	if len(cands) == 0 {
		fmt.Fprintln(w, "  no untested text->text candidates with context >= 32000")
	}
	for _, m := range cands {
		marker := ""
		if m.Free {
			marker = " FREE"
		}
		fmt.Fprintf(w, "  untested %s ctx=%d%s\n", m.ID, m.ContextLength, marker)
	}
	return nil
}

func (c *modelsCmd) writeHTML(cfg *config.AppConfig, s *store.Store, f scoreboard.Filter, reg scoreboard.Registry) error {
	// Rendering delegated to internal/hud/views (Step 4). Default path when --html has no value.
	path := c.HTML
	if path == "" {
		path = defaultScoreboardHTMLPath(cfg)
	}
	if err := renderScoreboardHTMLFile(path, s, f, reg, scoreboard.LoadNotes(cfg.ModelNotesPath())); err != nil {
		return err
	}
	fmt.Printf("wrote %s\n", path)
	if c.Open {
		return openInBrowser(path)
	}
	return nil
}

func openInBrowser(path string) error {
	bin := "xdg-open"
	if runtime.GOOS == "darwin" {
		bin = "open"
	}
	return exec.Command(bin, path).Start()
}

func init() {
	parser.AddCommand("models", "Per-model performance scoreboard",
		"Show the local per-model scoreboard from the SQLite eval store.", &modelsCmd{})
}
```

Note: `loadConfig()` is the small shared helper (extract from `hud.go`/`run.go`'s repeated `opts.Config → DefaultPath → Load`); add it to `cmd/ringer/main.go` if not already present. `defaultScoreboardHTMLPath` = `<state_dir>/model-scoreboard.html`. `renderScoreboardHTMLFile` + the templ page land in Step 4.

- [ ] **Step 4: Implement `ExploreCandidates` + the `--html` templ page**

Add to `internal/scoreboard/scoreboard.go`:

```go
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
```

(Add the `catalog` import to `scoreboard.go`.) Create `internal/hud/views/models_scoreboard.templ` — a self-contained page (reuse `ArtifactCSS` via `pageHead`) listing the tiered rollup rows and, per model, its `RenderNotes` judgment notes. Add `renderScoreboardHTMLFile(path, s, f, reg, notes)` to `cmd/ringer/models.go` that builds `scoreboard.Scoreboard(...)`, opens the file, and renders `views.ModelScoreboardPage(rows, notesByModel)` to it. Golden-test the templ via the existing `renderComponentString` harness in `internal/hud/views`.

- [ ] **Step 5: Run tests, confirm green (regenerate templ + goldens)**

Run: `./build.sh --test 2>&1 | grep -E "cmd/ringer|internal/hud/views|FAIL|ok"`
Regenerate the scoreboard golden if needed: `go test ./internal/hud/views -run ModelScoreboard -update` (sanctioned), then re-run `./build.sh --test`.
Expected: all `ok`.

- [ ] **Step 6: Commit**

```bash
git add cmd/ringer/models.go cmd/ringer/models_test.go internal/hud/views/models_scoreboard.templ internal/hud/views/*_templ.go internal/scoreboard/scoreboard.go internal/hud/views/testdata/
git commit -m "cmd: models subcommand (table / --json / --explore / --html --open)"
```

---

## Task 8: `catalog` subcommand

**Files:**
- Create: `cmd/ringer/catalog.go`
- Create: `cmd/ringer/catalog_test.go`

**Interfaces:**
- Consumes: `catalog.Refresh/LoadModelsFile/SortModels/DefaultSource/FetchTimeout`; `store.CatalogModels/FreeCatalogModels/CatalogEvents`; `config`.
- Produces: `ringer catalog` with `--refresh --source --file --free --changes --json` (§9.6 freezes `catalog --changes`). Output is Go-authoritative (golden-locked).

- [ ] **Step 1: Write the failing render tests**

Create `cmd/ringer/catalog_test.go` — test the two pure renderers:

```go
package main

import (
	"bytes"
	"strings"
	"testing"

	"github.com/corruptmemory/ringer/internal/catalog"
)

func TestRenderCatalogTable(t *testing.T) {
	var buf bytes.Buffer
	p := 0.5
	renderCatalogTable(&buf, []catalog.Model{{ID: "a/b", ContextLength: 128000, PromptPerM: &p, CompletionPerM: &p}, {ID: "z/free", Free: true}})
	out := buf.String()
	if !strings.Contains(out, "a/b") || !strings.Contains(out, "FREE") {
		t.Fatalf("catalog table wrong:\n%s", out)
	}
}

func TestDescribeEvent(t *testing.T) {
	got := describeCatalogEvent(catalog.Event{TS: "2026-07-10T00:00:00Z", Kind: "added", ModelID: "a/b", Payload: map[string]any{"free": true}})
	if !strings.Contains(got, "a/b") || !strings.Contains(got, "added") {
		t.Fatalf("describe wrong: %q", got)
	}
}
```

- [ ] **Step 2: Run it, confirm it fails**

Run: `./build.sh --test 2>&1 | grep -E "cmd/ringer|undefined|FAIL"`
Expected: FAIL — `renderCatalogTable` undefined.

- [ ] **Step 3: Implement `catalog.go`**

Create `cmd/ringer/catalog.go`. Refresh uses a real timestamp (`time.Now().UTC().Format(time.RFC3339)`); table columns (Go-authoritative): `id  $/M in  $/M out  ctx  FREE`.

```go
// cmd/ringer/catalog.go
package main

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"text/tabwriter"
	"time"

	"github.com/corruptmemory/ringer/internal/catalog"
	"github.com/corruptmemory/ringer/internal/store"
)

type catalogCmd struct {
	Refresh bool   `long:"refresh"`
	Source  string `long:"source" description:"fetch source URL or file (default OpenRouter)"`
	File    string `long:"file" description:"read a catalog JSON file instead of the DB"`
	Free    bool   `long:"free" description:"only free models"`
	Changes bool   `long:"changes" description:"recent catalog change events"`
	JSON    bool   `long:"json"`
}

func (c *catalogCmd) Execute(args []string) error {
	cfg, err := loadConfig()
	if err != nil {
		return err
	}
	s, err := store.Open(cfg.DBPath())
	if err != nil {
		return err
	}
	defer s.Close()

	if c.Refresh {
		src := c.Source
		if src == "" {
			src = catalog.DefaultSource
		}
		if _, err := catalog.Refresh(s, src, catalog.FetchTimeout, time.Now().UTC().Format(time.RFC3339)); err != nil {
			return err
		}
	}
	if c.Changes {
		evs, err := s.CatalogEvents(20)
		if err != nil {
			return err
		}
		for _, e := range evs {
			fmt.Println(describeCatalogEvent(e))
		}
		return nil
	}

	var models []catalog.Model
	switch {
	case c.File != "":
		models, err = catalog.LoadModelsFile(c.File)
	case c.Free:
		models, err = s.FreeCatalogModels()
	default:
		models, err = s.CatalogModels()
	}
	if err != nil {
		return err
	}
	if c.Free && c.File != "" {
		models = filterFree(models)
	}
	if c.JSON {
		return json.NewEncoder(os.Stdout).Encode(models)
	}
	if len(models) == 0 {
		return fmt.Errorf("no catalog models; run 'ringer catalog --refresh'")
	}
	renderCatalogTable(os.Stdout, models)
	return nil
}

func filterFree(models []catalog.Model) []catalog.Model {
	var out []catalog.Model
	for _, m := range models {
		if m.Free {
			out = append(out, m)
		}
	}
	return out
}

func renderCatalogTable(w io.Writer, models []catalog.Model) {
	catalog.SortModels(models)
	tw := tabwriter.NewWriter(w, 0, 2, 2, ' ', 0)
	fmt.Fprintln(tw, "id\t$/M in\t$/M out\tctx\tFREE")
	for _, m := range models {
		marker := ""
		if m.Free {
			marker = "FREE"
		}
		fmt.Fprintf(tw, "%s\t%s\t%s\t%d\t%s\n", m.ID, price(m.PromptPerM, m.VariablePricing), price(m.CompletionPerM, m.VariablePricing), m.ContextLength, marker)
	}
	tw.Flush()
}

func price(v *float64, variable bool) string {
	if variable || v == nil {
		return "var"
	}
	if *v == 0 {
		return "0"
	}
	return fmt.Sprintf("%.4g", *v)
}

// describeCatalogEvent ports ringer.py:1597-1624 (Go-authoritative wording).
func describeCatalogEvent(e catalog.Event) string {
	switch e.Kind {
	case "price_change":
		return fmt.Sprintf("%s %s price_change: in %v->%v, out %v->%v", e.TS, e.ModelID,
			e.Payload["old_prompt_per_m"], e.Payload["new_prompt_per_m"], e.Payload["old_completion_per_m"], e.Payload["new_completion_per_m"])
	case "went_free", "went_paid":
		return fmt.Sprintf("%s %s %s", e.TS, e.ModelID, e.Kind)
	case "added":
		marker := ""
		if free, _ := e.Payload["free"].(bool); free {
			marker = " FREE"
		}
		return fmt.Sprintf("%s %s added%s", e.TS, e.ModelID, marker)
	case "removed":
		return fmt.Sprintf("%s %s removed", e.TS, e.ModelID)
	default:
		return fmt.Sprintf("%s %s %s", e.TS, e.ModelID, e.Kind)
	}
}

func init() {
	parser.AddCommand("catalog", "OpenRouter model catalog",
		"Show or refresh the local OpenRouter model catalog (stored in SQLite).", &catalogCmd{})
}
```

- [ ] **Step 4: Run tests, confirm green**

Run: `./build.sh --test 2>&1 | grep -E "cmd/ringer|FAIL|ok"`
Expected: `ok ... cmd/ringer`.

- [ ] **Step 5: Commit**

```bash
git add cmd/ringer/catalog.go cmd/ringer/catalog_test.go
git commit -m "cmd: catalog subcommand (refresh/free/changes/json against SQLite)"
```

---

## Task 9: `db` subcommand (export / import / integrity / checkpoint)

**Files:**
- Create: `cmd/ringer/db.go`
- Create: `cmd/ringer/db_test.go`
- Modify: `internal/store/analytics.go` — `AllAttempts`

**Interfaces:**
- Consumes: `store.Open/InsertAttempt/Integrity/Checkpoint`; the frozen backfill precedence (Global Constraints).
- Produces:
  - `store.AllAttempts() ([]Attempt, error)` — `SELECT ... ORDER BY id`.
  - `ringer db export [--out PATH]` — attempts → JSONL (Go-native `Attempt` JSON; one object per line; stdout if no `--out`).
  - `ringer db import --jsonl PATH [--runs-dir PATH] [--mapping PATH] [--dry-run]` — JSONL → `attempts`, tolerant of legacy Python field names, applying the backfill precedence for missing `model`/`task_type`. Replaces `scripts/backfill_model_log.py`.
  - `ringer db integrity` → `PRAGMA integrity_check`; `ringer db checkpoint` → `wal_checkpoint(TRUNCATE)`.

- [ ] **Step 1: Write the failing import/backfill test**

Create `cmd/ringer/db_test.go` — the field-mapping + backfill precedence is the risky part; test `attemptFromJSONL` directly:

```go
package main

import "testing"

func TestAttemptFromLegacyJSONL(t *testing.T) {
	// legacy row: Python names, no model/task_type -> backfilled.
	row := map[string]any{
		"run_id": "r1", "task_key": "t1", "worker_engine": "codex",
		"verdict": "PASS", "duration_ms": float64(8000), "worker_tokens": float64(120),
		"logged_at": "2026-07-10T00:00:00Z", "retry": false,
	}
	runModel := func(runID, taskKey string) string { return "gpt-5.5" }
	mapping := map[string]string{"r1:t1": "code"}
	a := attemptFromJSONL(row, runModel, mapping)
	if a.Engine != "codex" || a.Model != "gpt-5.5" || a.TaskType != "code" {
		t.Fatalf("mapping wrong: %+v", a)
	}
	if a.DurationS != 8.0 || a.Tokens != 120 || a.CreatedAt != "2026-07-10T00:00:00Z" {
		t.Fatalf("field conversion wrong: %+v", a)
	}
}

func TestTaskTypePrecedence(t *testing.T) {
	m := map[string]string{"r1:t1": "exact", "r1": "run", "name:r": "prefix"}
	if got := taskTypeFromMapping(m, "r1", "t1"); got != "exact" {
		t.Fatalf("want exact, got %q", got)
	}
	if got := taskTypeFromMapping(m, "r1", "zz"); got != "run" {
		t.Fatalf("want run, got %q", got)
	}
	if got := taskTypeFromMapping(m, "rX", "zz"); got != "prefix" {
		t.Fatalf("want prefix, got %q", got)
	}
}
```

- [ ] **Step 2: Run it, confirm it fails**

Run: `./build.sh --test 2>&1 | grep -E "cmd/ringer|undefined|FAIL"`
Expected: FAIL — `attemptFromJSONL` undefined.

- [ ] **Step 3: Implement `db.go`**

Create `cmd/ringer/db.go`:

```go
// cmd/ringer/db.go
package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
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

func (c *dbIntegrityCmd) Execute(args []string) error  { return withStore(func(s *store.Store) error { return s.Integrity() }) }
func (c *dbCheckpointCmd) Execute(args []string) error { return withStore(func(s *store.Store) error { return s.Checkpoint() }) }

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
	best, bestLen := "", -1
	for k, v := range mapping {
		if !strings.HasPrefix(k, "name:") {
			continue
		}
		prefix := k[len("name:"):]
		if prefix != "" && strings.HasPrefix(runID, prefix) && len(prefix) > bestLen && v != "" {
			best, bestLen = v, len(prefix)
		}
	}
	return best
}
```

Helpers `retryFrom` (int `retry` > 0, or `bool retry==true`, or `"retry=true"` in `notes`), `durationSeconds` (`duration_s`, else `duration_ms`/1000), `tokensFrom` (`tokens`, else `worker_tokens`, else -1), `firstNonEmpty`, and `runStateModelLookup(runsDir)` (returns a cached lookup reading `<runsDir>/<run_id>.json` `tasks[].key==task_key → task.model`, port of `model_from_run_state`) go in the same file. Add `store.AllAttempts` to `internal/store/analytics.go` (`SELECT run_id,run_name,... ORDER BY id` → `[]Attempt`).

- [ ] **Step 4: Register the `db` command group**

In `db.go` `init()`, register the parent + subcommands:

```go
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
```

- [ ] **Step 5: Add an export→import round-trip test**

In `cmd/ringer/db_test.go`, add a test that seeds a store, exports via `AllAttempts`+JSON, re-imports the JSONL through `attemptFromJSONL`, and asserts the row count and a representative row survive the round-trip (native field names).

- [ ] **Step 6: Run tests, confirm green**

Run: `./build.sh --test 2>&1 | grep -E "cmd/ringer|internal/store|FAIL|ok"`
Expected: all `ok`.

- [ ] **Step 7: Commit**

```bash
git add cmd/ringer/db.go cmd/ringer/db_test.go internal/store/analytics.go
git commit -m "cmd: db export/import (backfill precedence) + integrity/checkpoint"
```

---

## Task 10: `/hud/models` panel from SQLite

**Files:**
- Modify: `internal/hud/models.go` — real handler
- Modify: `internal/hud/views/models.templ` — `ModelsPanel(rows []scoreboard.Row)` (was a no-arg stub)
- Modify: `internal/hud/views/layout.templ` — the models panel polls (`hx-trigger="load, every 10s"`)
- Test: `internal/hud/models_test.go`, `internal/hud/views/models_golden_test.go`

**Interfaces:**
- Consumes: `scoreboard.Scoreboard`/`LoadRegistry`; `store.Open`.
- Produces: `GET /hud/models` renders the tiered rollup from the SQLite store; empty/missing DB → an empty-state panel (never a 500).

**Layering note:** `internal/hud` already imports `internal/hud/views` and (Plan 4b) `internal/artifact`; it may import `internal/store` + `internal/scoreboard` (read-side, spec §4 "`hud` → state/store/artifact"). `internal/hud/views` imports `internal/scoreboard` for the `Row` type — `views` already imports `state`/`artifact`; adding `scoreboard` (a leaf over `store`/`catalog`) introduces no cycle.

- [ ] **Step 1: Write the failing handler test**

Create `internal/hud/models_test.go` — seed a store at `<stateDir>/ringer.db`, GET `/hud/models`, assert the rendered HTML carries a seeded model:

```go
package hud

import (
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/corruptmemory/ringer/internal/store"
)

func TestModelsPanelRendersFromStore(t *testing.T) {
	dir := t.TempDir()
	s, err := store.Open(filepath.Join(dir, "ringer.db"))
	if err != nil {
		t.Fatal(err)
	}
	for _, a := range []store.Attempt{
		{RunID: "r1", TaskKey: "t1", Engine: "codex", Model: "gpt-5.5", TaskType: "code", Verdict: "PASS", Tokens: 100, CreatedAt: "2026-07-10T00:00:01Z"},
		{RunID: "r1", TaskKey: "t2", Engine: "codex", Model: "gpt-5.5", TaskType: "code", Verdict: "PASS", Tokens: 100, CreatedAt: "2026-07-10T00:00:02Z"},
		{RunID: "r1", TaskKey: "t3", Engine: "codex", Model: "gpt-5.5", TaskType: "docs", Verdict: "PASS", Tokens: 100, CreatedAt: "2026-07-10T00:00:03Z"},
	} {
		if err := s.InsertAttempt(a); err != nil {
			t.Fatal(err)
		}
	}
	s.Close()

	srv := New(dir, nil)
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/hud/models", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("code=%d", rec.Code)
	}
	body := rec.Body.String()
	if !strings.Contains(body, "GPT-5.5") || !strings.Contains(body, "proven") {
		t.Fatalf("models panel missing data:\n%s", body)
	}
}

func TestModelsPanelEmptyStateNoDB(t *testing.T) {
	srv := New(t.TempDir(), nil) // no ringer.db yet
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/hud/models", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("empty-state must be 200, got %d", rec.Code)
	}
}
```

- [ ] **Step 2: Run it, confirm it fails**

Run: `./build.sh --test 2>&1 | grep -E "internal/hud|FAIL"`
Expected: FAIL — the stub renders no model data.

- [ ] **Step 3: Implement the handler**

Replace `internal/hud/models.go`:

```go
// internal/hud/models.go
package hud

import (
	"net/http"
	"path/filepath"

	"github.com/corruptmemory/ringer/internal/hud/views"
	"github.com/corruptmemory/ringer/internal/scoreboard"
	"github.com/corruptmemory/ringer/internal/store"
)

// handleModels renders the tiered per-model scoreboard from the SQLite eval
// store. A missing/empty DB yields an empty-state panel, never a 500.
func (s *Server) handleModels(w http.ResponseWriter, r *http.Request) {
	dbPath := filepath.Join(s.stateDir, "ringer.db")
	st, err := store.Open(dbPath)
	if err != nil {
		s.lg.Warnf("hud: models: open store: %v", err)
		s.renderComponent(w, r, views.ModelsPanel(nil))
		return
	}
	defer st.Close()
	rows, err := scoreboard.Scoreboard(st, scoreboard.Filter{}, scoreboard.LoadRegistry(""))
	if err != nil {
		s.lg.Warnf("hud: models: scoreboard: %v", err)
		rows = nil
	}
	s.renderComponent(w, r, views.ModelsPanel(rows))
}
```

(The HUD uses the embedded identity registry — `LoadRegistry("")`. A config-override registry for the HUD is out of scope; note it.)

- [ ] **Step 4: Implement the templ panel**

Replace `internal/hud/views/models.templ`'s `ModelsPanel` to take `rows []scoreboard.Row` and render a table (model display, tier chip, tasks, pass %, first-try %, median tokens, harness). Empty `rows` → an empty-state line. Reuse existing ringside CSS classes (e.g. `.panel`, `.chip`). In `layout.templ`, change the models panel to poll: `hx-trigger="load, every 10s" hx-swap="morph:innerHTML"`.

- [ ] **Step 5: Run tests + regenerate templ/golden**

Run: `./build.sh --test 2>&1 | grep -E "internal/hud|FAIL|ok"`
Regenerate the panel golden if used: `go test ./internal/hud/views -run ModelsPanel -update`, then `./build.sh --test`.
Expected: all `ok`.

- [ ] **Step 6: Commit**

```bash
git add internal/hud/models.go internal/hud/views/models.templ internal/hud/views/layout.templ internal/hud/views/*_templ.go internal/hud/models_test.go internal/hud/views/testdata/
git commit -m "hud: /hud/models renders the tiered scoreboard from SQLite"
```

---

## Task 11: `run` catalog auto-refresh (background, 24h throttle)

**Files:**
- Modify: `internal/config/config.go` — `CatalogConfig{Source string}`
- Modify: `internal/store/analytics.go` — `NewestCatalogFetchedAt`
- Create: `cmd/ringer/catalog_refresh.go` — throttle check + background trigger
- Modify: `cmd/ringer/run.go` — call the trigger on real runs
- Test: `cmd/ringer/catalog_refresh_test.go`

**Interfaces:**
- Consumes: `store.NewestCatalogFetchedAt() (string, error)`; `catalog.Refresh/DefaultSource/AutoRefreshMaxAge`.
- Produces:
  - `config.AppConfig.Catalog CatalogConfig` with `Source string` (`toml:"catalog"` → `source`); accessor `CatalogSource()` returns the override or `""` (cmd falls back to `catalog.DefaultSource`).
  - `store.NewestCatalogFetchedAt() (string, error)` — `SELECT MAX(fetched_at) FROM catalog_models`.
  - `catalogIsStale(newestFetchedAt, now string, maxAge time.Duration) bool` (pure; parses RFC3339; empty/unparseable → stale) and `maybeRefreshCatalog(s *store.Store, source string, lg logging.Logger)` — checks staleness, and if stale spawns a detached goroutine that fetches+persists best-effort, logging failures (never blocks/aborts the run).

- [ ] **Step 1: Write the failing throttle test**

Create `cmd/ringer/catalog_refresh_test.go`:

```go
package main

import (
	"testing"
	"time"
)

func TestCatalogIsStale(t *testing.T) {
	now := "2026-07-10T12:00:00Z"
	fresh := "2026-07-10T00:00:00Z"  // 12h ago
	stale := "2026-07-08T00:00:00Z"  // 60h ago
	if catalogIsStale(fresh, now, 24*time.Hour) {
		t.Fatal("12h-old catalog should be fresh")
	}
	if !catalogIsStale(stale, now, 24*time.Hour) {
		t.Fatal("60h-old catalog should be stale")
	}
	if !catalogIsStale("", now, 24*time.Hour) {
		t.Fatal("empty (never fetched) should be stale")
	}
}
```

- [ ] **Step 2: Run it, confirm it fails**

Run: `./build.sh --test 2>&1 | grep -E "cmd/ringer|undefined|FAIL"`
Expected: FAIL — `catalogIsStale` undefined.

- [ ] **Step 3: Implement the throttle + background trigger**

Create `cmd/ringer/catalog_refresh.go`:

```go
// cmd/ringer/catalog_refresh.go
package main

import (
	"time"

	"github.com/corruptmemory/ringer/internal/catalog"
	"github.com/corruptmemory/ringer/internal/logging"
	"github.com/corruptmemory/ringer/internal/store"
)

// catalogIsStale reports whether the newest catalog fetch is older than
// maxAge. An empty or unparseable timestamp counts as stale.
func catalogIsStale(newestFetchedAt, now string, maxAge time.Duration) bool {
	if newestFetchedAt == "" {
		return true
	}
	f, err := time.Parse(time.RFC3339, newestFetchedAt)
	if err != nil {
		return true
	}
	n, err := time.Parse(time.RFC3339, now)
	if err != nil {
		return true
	}
	return n.Sub(f) > maxAge
}

// maybeRefreshCatalog triggers a best-effort background catalog refresh if the
// stored catalog is stale. Never blocks the run; failures are logged, not fatal.
func maybeRefreshCatalog(s *store.Store, source string, lg logging.Logger, now string) {
	newest, err := s.NewestCatalogFetchedAt()
	if err != nil {
		lg.Warnf("catalog auto-refresh: freshness check: %v", err)
		return
	}
	if !catalogIsStale(newest, now, catalog.AutoRefreshMaxAge) {
		return
	}
	go func() {
		if _, err := catalog.Refresh(s, source, catalog.FetchTimeout, now); err != nil {
			lg.Warnf("catalog auto-refresh: %v", err)
		} else {
			lg.Infof("catalog auto-refreshed from %s", source)
		}
	}()
}
```

- [ ] **Step 4: Add `NewestCatalogFetchedAt` + config + wire into `run`**

Append to `internal/store/analytics.go`:

```go
func (s *Store) NewestCatalogFetchedAt() (string, error) {
	var v *string
	err := withBusyRetry(func() error {
		return s.db.QueryRow(`SELECT MAX(fetched_at) FROM catalog_models`).Scan(&v)
	})
	if err != nil || v == nil {
		return "", err
	}
	return *v, nil
}
```

Add to `config.go`: `type CatalogConfig struct { Source string \`toml:"source"\` }`, field `Catalog CatalogConfig \`toml:"catalog"\``, and `CatalogSource()` returning `c.Catalog.Source`. In `run.go`'s real-run path (after the store is opened, before/alongside `runner.Run`, guarded by `!dryRun`), call `maybeRefreshCatalog(st, catalogSourceOrDefault(cfg), lg, time.Now().UTC().Format(time.RFC3339))` where `catalogSourceOrDefault` returns `cfg.CatalogSource()` or `catalog.DefaultSource`. The runner already holds a `*store.Store` on the real-run path (Plan 2/4b) — reuse it; a background goroutine sharing the `*sql.DB` is safe (`SetMaxOpenConns(1)` serializes the brief catalog write against attempt inserts; the slow HTTP fetch happens outside any DB lock).

- [ ] **Step 5: Run tests, confirm green**

Run: `./build.sh --test 2>&1 | grep -E "cmd/ringer|internal/store|internal/config|FAIL|ok"`
Expected: all `ok`.

- [ ] **Step 6: Commit**

```bash
git add cmd/ringer/catalog_refresh.go cmd/ringer/catalog_refresh_test.go internal/store/analytics.go internal/config/config.go cmd/ringer/run.go
git commit -m "run: background 24h-throttled catalog auto-refresh"
```

---

## Done criteria

- `./build.sh --test --race` green across all packages.
- `ringer models` prints the scoreboard from `~/.ringer/ringer.db`; `--json`/`--explore`/`--html --open` work; `--task-type`/`--model`/`--engine`/`--since` filter.
- `ringer catalog --refresh` populates `catalog_models`/`catalog_events`; `catalog`/`--free`/`--changes`/`--json` read them.
- `ringer db export` → JSONL; `ringer db import --jsonl <legacy>` seeds `attempts` with the frozen backfill precedence; `db integrity`/`db checkpoint` work.
- The HUD `/hud/models` panel renders the tiered scoreboard from SQLite (browser-verified, per [[use-browser-for-ui-verification]]).
- `run`/`demo` trigger a throttled background catalog refresh.
- **Deferred to Plan 5b/5c:** `install-agent`/`uninstall-agent`/`nudge-hook`/`gen-config` (5b); deleting `ringer.py`/`hud/`/`dashboard/`/`scripts/backfill_model_log.py`, README/SKILL sweep, `config.sample.toml` Python-era key cleanup, `allow_full_access` gating (5c).
