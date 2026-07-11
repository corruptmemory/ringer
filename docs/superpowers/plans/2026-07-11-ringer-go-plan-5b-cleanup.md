# Ringer Go Plan 5b — Cleanup Batch Implementation Plan

> **STATUS — EXECUTED + reviewed 2026-07-11 (branch `go-5b`).** All 5 tasks ran via subagent-driven-development; opus whole-branch review (Tasks 1–4) = **READY TO MERGE**, 0 Critical / 0 Important. **Task 5 (the visible cost column)** was added after Jim's look-see and task-reviewed clean. Shipped-code is authoritative where it diverges from the task text; the deliberate deviations:
> - **Task 2:** the scoreboard cost JOIN strips the `openrouter/` prefix (`cm.id = CASE WHEN s.model LIKE 'openrouter/%' THEN substr(s.model, length('openrouter/')+1) ELSE s.model END`) — an approved break from Python parity so opencode cost resolves; codex/grok stay "in plan".
> - **Task 4:** `config.sample.toml` is now a **generated** artifact (`RenderDocumented(ExampleConfig())`), drift-locked by `TestConfigSampleIsFresh`; regenerating it dropped the stale Python-era keys (`dashboard_port_base`, `hud_app_path`, `[eval] backend`).
> - **Task 1:** a `var ensureHUD = ensureHUDRunning` seam makes the dry-run "no HUD spawn" test real (closes a Plan-4 fork-bomb-adjacent risk).
> - **Fast-follow (`152f5aa`):** the gen-config round-trip test now asserts `config.Load` succeeds (value validation), not just decode.
>
> **Deferred:** 5c = agent-integration (`install-agent`/`uninstall-agent` + `nudge-hook`); 5d = the hard cutover.

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Three focused cleanups on top of the merged Plan 5a analytics: de-fixate the hardcoded HUD port, make the scoreboard's opencode cost actually resolve, and add a reflection-based `gen-config` that generates a self-documenting TOML config from the config structs (retiring the drift-prone hand-maintained `config.sample.toml`).

**Architecture:** Small, independent changes. (1) The HUD port becomes a `[hud] port` config key + `--port` on `run`/`demo`, threaded into the already-port-parameterized `ensureHUDRunning`. (2) The scoreboard cost JOIN strips the `openrouter/` prefix so opencode model slugs match OpenRouter catalog ids. (3) `gen-config` walks the config structs via `reflect`, emitting `# ` comments from `doc:"..."` tags and values from an `ExampleConfig()` source-of-truth — extended beyond the house exemplars to handle Ringer's `slog.Level` (a `TextMarshaler`), `map[string]EngineConfig` (Engines), and `*bool` fields.

**Tech Stack:** Go 1.26, `CGO_ENABLED=0`; `github.com/BurntSushi/toml`; `github.com/jessevdk/go-flags`; `reflect`; table-driven + golden tests via `./build.sh`.

## Global Constraints

Every task's requirements implicitly include this section.

- **Build/test ONLY via `./build.sh` and `./build.sh --test [--race]`.** Never `go build`/`go test`/`templ generate` directly. The one sanctioned exception is regenerating a golden fixture with `go test ./<pkg> -run <Name> -update`. Editor-LSP "undefined"/"unused"/"too many arguments" reports are known false positives in this repo — a passing `./build.sh --test` is the source of truth.
- **No silent failures, ever.** New flags/config must do what they say or error loudly; the `gen-config` no-overwrite guard errors rather than clobbering; a stray positional errors (match the `models`/`catalog` guard precedent).
- **CLI expressive-equivalence + no port fixation** (house rules): the port is just a port — a `--port`/config default, and generated pages stay port-agnostic (they already use relative links; do not add absolute `:8700` URLs).
- **Layering (unchanged):** `internal/config` is a leaf (stdlib + BurntSushi/toml only — the `gen-config` generator lives here and must NOT import other `internal/...` packages). `cmd/ringer` wires the subcommand + flags. `internal/store` owns the scoreboard SQL.
- **Frozen contracts still hold:** the config TOML schema (keys, `isolation`/`logging.format` validation) is unchanged except the additive `[hud] port` key; `catalog_models`/`attempts` schemas unchanged; the cost formula (`median_tokens * (prompt+completion)/2 / 1e6`) unchanged — only the JOIN key is normalized.
- **`config.sample.toml` becomes a generated artifact** produced by `gen-config`, with a test asserting it stays fresh (so it can never drift again). Regenerating it incidentally drops the stale Python-era keys.

---

## File Structure

**New files:**
- `internal/config/gencfg.go` — `RenderDocumented(AppConfig) (string, error)` + the reflect helpers (`writeSections`, `writeCommentBlock`, `tomlName`, `tomlLiteral`) with the map/pointer/TextMarshaler extensions.
- `internal/config/example.go` — `ExampleConfig() AppConfig` (the documented-example source of truth, incl. example `[engines.codex]`/`[engines.opencode]` entries).
- `internal/config/gencfg_test.go`, `internal/config/example_test.go`.
- `cmd/ringer/genconfig.go` + `cmd/ringer/genconfig_test.go` — the `gen-config` subcommand.

**Modified files:**
- `internal/config/config.go` — add `HudConfig{Port int}` + `Hud` field + `HudPort()` accessor; add `doc:"..."` tags to every config struct field.
- `cmd/ringer/run.go` — `--port` on `runCmd`/`demoCmd` threaded through `runManifestFile` into `ensureHUDRunning`.
- `cmd/ringer/demo.go` — `--port` flag (mirrors run).
- `cmd/ringer/hud.go` — `hud --port` default falls back to `[hud] port` config then 8700.
- `internal/store/analytics.go` — the cost JOIN (line ~239) strips the `openrouter/` prefix.
- `config.sample.toml` — regenerated from `gen-config` (drift-locked by a test).

## Task Overview

1. Port de-fixation (`[hud] port` + `--port` on run/demo → `ensureHUDRunning`)
2. opencode cost resolution (strip `openrouter/` prefix in the scoreboard cost JOIN)
3. `gen-config` reflect generator (config package: `doc` tags + `ExampleConfig()` + generator with map/pointer/TextMarshaler support)
4. `gen-config` subcommand + regenerate `config.sample.toml` (drift-lock test)

---

## Task 1: Port de-fixation

**Files:**
- Modify: `internal/config/config.go` (add `HudConfig` + `HudPort()`)
- Modify: `cmd/ringer/run.go` (`--port` on `runCmd`, thread through `runManifestFile`)
- Modify: `cmd/ringer/demo.go` (`--port` on `demoCmd`)
- Modify: `cmd/ringer/hud.go` (`hud --port` default ← `[hud] port`)
- Test: `internal/config/config_test.go`, `cmd/ringer/run_test.go`

**Interfaces:**
- Consumes: existing `ensureHUDRunning(stateDir string, port int, lg, openBrowser bool)` and `spawnDetachedHUD` (already port-parameterized — they spawn `ringer hud --no-open --port <port>`); `hud.DefaultPort` (8700).
- Produces: `config.HudConfig{Port int}` (toml `[hud] port`), `AppConfig.Hud HudConfig`, `(*AppConfig).HudPort() int` (returns `c.Hud.Port` if > 0 else `hud`-package default 8700 — but config is a leaf and must not import `internal/hud`, so hardcode the literal `8700` in config with a doc comment cross-referencing `hud.DefaultPort`). `runManifestFile` gains a trailing `hudPortOverride int` param (0 = use config).

**Background:** the only real fixation is `run.go:82` passing `hud.DefaultPort` literally; `ensureHUDRunning`/`spawnDetachedHUD` already take a `port`. Precedence: `--port` flag > `[hud] port` config > 8700.

- [ ] **Step 1: Write the failing config test**

Add to `internal/config/config_test.go`:

```go
func TestHudPortDefaultAndOverride(t *testing.T) {
	var c AppConfig
	if got := c.HudPort(); got != 8700 {
		t.Fatalf("default HudPort = %d, want 8700", got)
	}
	c.Hud.Port = 9100
	if got := c.HudPort(); got != 9100 {
		t.Fatalf("configured HudPort = %d, want 9100", got)
	}
}
```

- [ ] **Step 2: Run it, confirm it fails**

Run: `./build.sh --test 2>&1 | grep -E "internal/config|undefined|FAIL"`
Expected: FAIL — `HudPort` / `Hud` undefined.

- [ ] **Step 3: Add `HudConfig` + accessor to config.go**

In `internal/config/config.go`:

```go
// HudConfig configures the Ringside HUD. Port is the fixed port the HUD binds
// (127.0.0.1 only, fails if taken); run/demo probe + auto-spawn on it.
type HudConfig struct {
	Port int `toml:"port" doc:"HUD port (127.0.0.1). run/demo auto-spawn + probe here. Default 8700 (matches hud.DefaultPort)."`
}
```

Add `Hud HudConfig \`toml:"hud"\`` to `AppConfig` (after `Catalog`), and:

```go
// HudPort resolves the HUD port: the configured [hud] port, or 8700.
// (config is a leaf; the literal mirrors internal/hud.DefaultPort.)
func (c *AppConfig) HudPort() int {
	if c.Hud.Port > 0 {
		return c.Hud.Port
	}
	return 8700
}
```

- [ ] **Step 4: Thread `--port` through run/demo**

In `cmd/ringer/run.go`, add to `runCmd`: `Port int \`long:"port" description:"HUD port for the auto-started Ringside (default: [hud] port or 8700)"\``. Change `runCmd.Execute` to pass `c.Port` and update `runManifestFile`'s signature to accept `hudPortOverride int`, then at line 82:

```go
if !dryRun && !noDashboard {
	port := cfg.HudPort()
	if hudPortOverride > 0 {
		port = hudPortOverride
	}
	ensureHUDRunning(cfg.StateDirPath(), port, lg, true)
}
```

Update both callers of `runManifestFile` (run's `Execute`, demo's `Execute`) to pass their `--port` flag (0 when unset). Add the same `Port int \`long:"port"\`` flag to `demoCmd` in `cmd/ringer/demo.go`.

- [ ] **Step 5: Make `hud --port` honor the config default**

In `cmd/ringer/hud.go` `Execute`, when `c.Port == 0`, use `cfg.HudPort()` instead of the bare `hud.DefaultPort`:

```go
port := c.Port
if port == 0 {
	port = cfg.HudPort()
}
```

- [ ] **Step 6: Test the wiring**

Add a `cmd/ringer/run_test.go` case asserting `runManifestFile(..., dryRun=true, ...)` still returns nil and does not spawn a HUD (dry-run short-circuits before the port logic), and that the `runCmd`/`demoCmd` structs carry a `Port` field parsed by go-flags (drive the parser like `TestModelsFlagParsing`). Keep it focused — the port *resolution* is unit-tested in config; here just prove the flag is wired and dry-run is unaffected.

- [ ] **Step 7: Run tests + commit**

Run: `./build.sh --test 2>&1 | grep -E "internal/config|cmd/ringer|FAIL|ok"`

```bash
git add internal/config/config.go internal/config/config_test.go cmd/ringer/run.go cmd/ringer/demo.go cmd/ringer/hud.go cmd/ringer/run_test.go
git commit -m "hud: configurable port for run/demo auto-HUD ([hud] port + --port)"
```

---

## Task 2: opencode cost resolution

**Files:**
- Modify: `internal/store/analytics.go` (the `ScoreboardModelRows` cost JOIN, ~line 239)
- Test: `internal/store/scoreboard_query_test.go`

**Interfaces:**
- Consumes: the existing `ScoreboardModelRows` query + `catalog_models` table.
- Produces: cost now resolves for opencode-style model slugs (`openrouter/z-ai/glm-5.2`) by matching the catalog id with the `openrouter/` prefix stripped (`z-ai/glm-5.2`).

**Background — deliberate improvement over Python:** Python's cost lookup keys `catalog_models.id = model` exactly, so opencode's `openrouter/<slug>` never matches the catalog's `<slug>` and cost is always blank. Cost is only economically meaningful for opencode anyway (OpenRouter per-token); codex/grok are OAuth-plan flat-rate ("in plan" → correctly blank). Stripping the prefix is the smallest change that makes the column work for the one engine it applies to.

- [ ] **Step 1: Write the failing test**

Extend `internal/store/scoreboard_query_test.go` with a test that seeds an opencode-style attempt + a matching catalog row and asserts cost resolves:

```go
func TestScoreboardCostStripsOpenrouterPrefix(t *testing.T) {
	s, err := Open(filepath.Join(t.TempDir(), "cost.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	// opencode model slug carries the openrouter/ prefix; catalog id does not.
	for _, a := range []Attempt{
		{RunID: "r1", TaskKey: "t1", Engine: "opencode", Model: "openrouter/z-ai/glm-5.2", TaskType: "code", Verdict: "PASS", Tokens: 1000, CreatedAt: "2026-07-11T00:00:01Z"},
	} {
		if err := s.InsertAttempt(a); err != nil {
			t.Fatal(err)
		}
	}
	p := 2.0
	if err := s.ReplaceCatalog([]catalog.Model{{ID: "z-ai/glm-5.2", PromptPerM: &p, CompletionPerM: &p}}); err != nil {
		t.Fatal(err)
	}
	rows, err := s.ScoreboardModelRows(ScoreFilter{})
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 || rows[0].Cost == nil {
		t.Fatalf("opencode cost did not resolve after prefix strip: %+v", rows)
	}
	// median_tokens = 1000, (2+2)/2 = 2 per-M, cost = 1000*2/1e6 = 0.002
	if *rows[0].Cost != 0.002 {
		t.Fatalf("cost = %v, want 0.002", *rows[0].Cost)
	}
}
```

- [ ] **Step 2: Run it, confirm it fails**

Run: `./build.sh --test 2>&1 | grep -E "internal/store|FAIL"`
Expected: FAIL — `rows[0].Cost` is nil (no prefix strip yet).

- [ ] **Step 3: Strip the prefix in the JOIN**

In `internal/store/analytics.go`, change the cost JOIN in `ScoreboardModelRows` from:

```sql
LEFT JOIN catalog_models cm ON cm.id = s.model
```

to:

```sql
LEFT JOIN catalog_models cm ON cm.id = CASE
  WHEN s.model LIKE 'openrouter/%' THEN substr(s.model, length('openrouter/') + 1)
  ELSE s.model
END
```

Leave the cost `CASE` expression itself unchanged. Add a one-line comment noting this is a deliberate divergence from Python (which does not strip the prefix), keyed to opencode being the only per-token engine.

- [ ] **Step 4: Run tests, confirm green + no regression**

Run: `./build.sh --test 2>&1 | grep -E "internal/store|FAIL|ok"`
Expected: `ok` — the new test passes and `TestScoreboardModelRows` (the existing cost lock, model "M" with an exact-match catalog id) still passes (exact matches are unaffected by the strip).

- [ ] **Step 5: Commit**

```bash
git add internal/store/analytics.go internal/store/scoreboard_query_test.go
git commit -m "scoreboard: resolve opencode cost by stripping the openrouter/ prefix in the catalog join"
```

---

## Task 3: `gen-config` reflect generator

**Files:**
- Create: `internal/config/example.go` (`ExampleConfig()`)
- Create: `internal/config/gencfg.go` (`RenderDocumented` + reflect helpers)
- Modify: `internal/config/config.go` (add `doc:"..."` tags to every field)
- Test: `internal/config/gencfg_test.go`

**Interfaces:**
- Consumes: the `AppConfig` structs (with new `doc` tags), `github.com/BurntSushi/toml` (for the round-trip test), `reflect`, `encoding` (TextMarshaler), stdlib.
- Produces:
  - `config.ExampleConfig() AppConfig` — a fully-populated documented example (sensible scalar defaults + example `[engines.codex]` and `[engines.opencode]` entries mirroring the current `config.sample.toml`).
  - `config.RenderDocumented(cfg AppConfig) (string, error)` — the generated TOML (header banner + `# `-commented sections from `doc` tags).
  - Internal helpers `writeSections` / `writeCommentBlock` / `tomlName` / `tomlLiteral` handling: scalars, slices, `map[string]struct` (→ `[parent.<key>]` tables, keys sorted), pointer leaves (deref; nil → the elem's zero-value literal), and `encoding.TextMarshaler`/`fmt.Stringer` leaves (e.g. `slog.Level` → `"INFO"`).

**Design decisions (banner for review):** (1) `ExampleConfig()` is a *documented example*, not a claim about runtime zero-value defaults — the schema + `doc` comments come from the structs (drift-proof), the example values are curated (necessarily so for the Engines map, which has no static fields to reflect). (2) The generator lives in `internal/config` (leaf) so it can reflect the structs without an import cycle. (3) `slog.Level` must render as its text form (`"INFO"`), never its int value (`0`) — a bare int would fail to decode via `UnmarshalText`; hence the TextMarshaler/Stringer branch runs *before* the integer branch.

- [ ] **Step 1: Add `doc:"..."` tags to every config field**

In `internal/config/config.go`, add a `doc:"..."` tag beside each `toml:"..."` tag on `LoggingConfig`, `EngineConfig`, `ArtifactConfig`, `EvalConfig`, `ScoreboardConfig`, `CatalogConfig`, `HudConfig` (Task 1), and the top-level `AppConfig` fields. Each `doc` is a one- or two-line human explanation (the `AppConfig` struct fields that are sub-sections get a section-level doc, e.g. `Engines map[string]EngineConfig \`toml:"engines" doc:"Per-engine spawn config. One [engines.<name>] table per engine."\``). Keep them terse and accurate; these become the generated file's comments.

- [ ] **Step 2: Write the failing generator test**

Create `internal/config/gencfg_test.go`:

```go
package config

import (
	"strings"
	"testing"

	"github.com/BurntSushi/toml"
)

func TestRenderDocumentedRoundTrips(t *testing.T) {
	out, err := RenderDocumented(ExampleConfig())
	if err != nil {
		t.Fatal(err)
	}
	// It must contain comments (from doc tags) and section headers.
	for _, want := range []string{"# ", "[engines.codex]", "[engines.opencode]", "[logging]", "level = \"INFO\"", "allow_full_access ="} {
		if !strings.Contains(out, want) {
			t.Errorf("generated config missing %q:\n%s", want, out)
		}
	}
	// And it must decode back into an AppConfig with the strict loader's rules
	// (no unknown keys), proving the generated keys are all real.
	var c AppConfig
	md, err := toml.Decode(out, &c)
	if err != nil {
		t.Fatalf("generated config did not decode: %v", err)
	}
	if u := md.Undecoded(); len(u) != 0 {
		t.Fatalf("generated config has keys the struct doesn't accept: %v", u)
	}
	if _, ok := c.Engines["codex"]; !ok {
		t.Fatalf("round-tripped config lost the codex engine")
	}
	// slog.Level rendered as text, not int (a bare int wouldn't UnmarshalText).
	if strings.Contains(out, "level = 0") {
		t.Fatalf("slog.Level rendered as int, not text")
	}
	// *bool (artifact.enabled) rendered as a bool literal, not a pointer/nil.
	if !strings.Contains(out, "enabled = true") {
		t.Fatalf("*bool did not render as a bool literal:\n%s", out)
	}
	// []string (args_template) rendered as a TOML array.
	if !strings.Contains(out, "args_template = [") {
		t.Fatalf("[]string did not render as an array:\n%s", out)
	}
}
```

This one round-trip test exercises every hard case (the `slog.Level` TextMarshaler/Stringer branch, the `*bool` pointer deref, the `[]string` slice, and the `map[string]EngineConfig` → `[engines.<key>]` sections), so no separate per-kind test is needed.

- [ ] **Step 3: Run it, confirm it fails**

Run: `./build.sh --test 2>&1 | grep -E "internal/config|undefined|FAIL"`
Expected: FAIL — `RenderDocumented` / `ExampleConfig` undefined.

- [ ] **Step 4: Implement `example.go`**

Create `internal/config/example.go` — `ExampleConfig()` returns an `AppConfig` populated with the current `config.sample.toml` values: `IdentityDefault` example, `Hud.Port = 8700`, `Logging{Level: slog.LevelInfo, Format: "text"}`, an `Artifact{Enabled: ptr(true)}`, `Engines` with a `codex` entry (bin/args_template/sandbox_args/full_access_args/token_regex per the existing sample) and an `opencode` entry (bin, `Isolation: "jail"`, `JailStateDirs`, `JailRoBinds`). Include a small `func ptr[T any](v T) *T { return &v }` helper if needed. This is the curated example source of truth.

- [ ] **Step 5: Implement `gencfg.go`**

Create `internal/config/gencfg.go`:

```go
// internal/config/gencfg.go
package config

import (
	"encoding"
	"fmt"
	"reflect"
	"sort"
	"strconv"
	"strings"
)

// RenderDocumented generates a documented TOML config from cfg: `doc:"..."`
// tags become `# ` comments, `toml:"..."` tags become keys, values come from
// cfg (ExampleConfig() is the curated source of truth). Schema + comments are
// reflected from the structs, so they can never drift from the code.
func RenderDocumented(cfg AppConfig) (string, error) {
	var b strings.Builder
	b.WriteString("# Generated by `ringer gen-config`.\n")
	b.WriteString("# Schema + docs come from the config structs (internal/config); copy to your\n")
	b.WriteString("# config path (default ~/.config/ringer/config.toml, override with --config).\n\n")
	if err := writeSections(&b, reflect.TypeOf(cfg), reflect.ValueOf(cfg), "", ""); err != nil {
		return "", err
	}
	return b.String(), nil
}

// writeSections walks a struct two-pass — TOML requires bare keys before any
// [table], so leaves are emitted first, then struct / map[string]struct
// sections as [path] / [path.<key>] tables (map keys sorted).
func writeSections(b *strings.Builder, t reflect.Type, v reflect.Value, path, sectionDoc string) error {
	if sectionDoc != "" {
		writeCommentBlock(b, sectionDoc)
	}
	if path != "" {
		fmt.Fprintf(b, "[%s]\n", path)
	}
	for i := 0; i < t.NumField(); i++ { // pass 1: leaves
		f := t.Field(i)
		name := tomlName(f)
		if !f.IsExported() || name == "" || isSection(f.Type) {
			continue
		}
		lit, err := tomlLiteral(v.Field(i))
		if err != nil {
			return fmt.Errorf("field %s: %w", f.Name, err)
		}
		if doc := f.Tag.Get("doc"); doc != "" {
			writeCommentBlock(b, doc)
		}
		fmt.Fprintf(b, "%s = %s\n", name, lit)
	}
	for i := 0; i < t.NumField(); i++ { // pass 2: sections
		f := t.Field(i)
		name := tomlName(f)
		if !f.IsExported() || name == "" || !isSection(f.Type) {
			continue
		}
		child := name
		if path != "" {
			child = path + "." + name
		}
		fv := v.Field(i)
		if f.Type.Kind() == reflect.Map {
			keys := fv.MapKeys()
			sort.Slice(keys, func(a, c int) bool { return keys[a].String() < keys[c].String() })
			for _, k := range keys {
				b.WriteString("\n")
				ev := fv.MapIndex(k)
				if err := writeSections(b, ev.Type(), ev, child+"."+k.String(), f.Tag.Get("doc")); err != nil {
					return err
				}
			}
			continue
		}
		b.WriteString("\n")
		if err := writeSections(b, f.Type, fv, child, f.Tag.Get("doc")); err != nil {
			return err
		}
	}
	return nil
}

// isSection reports whether a field renders as a [table]: a struct, or a map
// whose element is a struct.
func isSection(t reflect.Type) bool {
	switch t.Kind() {
	case reflect.Struct:
		return true
	case reflect.Map:
		return t.Elem().Kind() == reflect.Struct
	}
	return false
}

func writeCommentBlock(b *strings.Builder, doc string) {
	for _, line := range strings.Split(doc, "\n") {
		fmt.Fprintf(b, "# %s\n", strings.TrimRight(line, " "))
	}
}

func tomlName(f reflect.StructField) string {
	name, _, _ := strings.Cut(f.Tag.Get("toml"), ",")
	if name == "-" {
		return ""
	}
	if name == "" {
		return f.Name
	}
	return name
}

// tomlLiteral renders a scalar/slice/pointer/TextMarshaler value as a TOML
// literal. Order matters: pointer-deref and TextMarshaler/Stringer run BEFORE
// the integer branch, so slog.Level renders as "INFO" (via its Stringer/
// TextMarshaler), never as its int value 0 (which would fail UnmarshalText).
func tomlLiteral(v reflect.Value) (string, error) {
	if v.Kind() == reflect.Ptr {
		if v.IsNil() {
			return tomlLiteral(reflect.Zero(v.Type().Elem()))
		}
		return tomlLiteral(v.Elem())
	}
	if v.CanInterface() {
		switch m := v.Interface().(type) {
		case encoding.TextMarshaler:
			txt, err := m.MarshalText()
			if err != nil {
				return "", err
			}
			return strconv.Quote(string(txt)), nil
		case fmt.Stringer:
			return strconv.Quote(m.String()), nil
		}
	}
	switch v.Kind() {
	case reflect.String:
		return strconv.Quote(v.String()), nil
	case reflect.Bool:
		return strconv.FormatBool(v.Bool()), nil
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return strconv.FormatInt(v.Int(), 10), nil
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		return strconv.FormatUint(v.Uint(), 10), nil
	case reflect.Float32, reflect.Float64:
		return strconv.FormatFloat(v.Float(), 'g', -1, 64), nil
	case reflect.Slice, reflect.Array:
		parts := make([]string, v.Len())
		for i := 0; i < v.Len(); i++ {
			p, err := tomlLiteral(v.Index(i))
			if err != nil {
				return "", err
			}
			parts[i] = p
		}
		return "[" + strings.Join(parts, ", ") + "]", nil
	}
	return "", fmt.Errorf("gen-config: unsupported kind %s", v.Kind())
}
```

Note on `slog.Level`: it satisfies `fmt.Stringer` (`String()` → `"INFO"`) and BurntSushi decodes `level = "INFO"` via its `UnmarshalText` (the reason `TestRenderDocumentedRoundTrips` asserts `level = "INFO"` and *not* `level = 0`).

- [ ] **Step 6: Run tests, confirm green**

Run: `./build.sh --test 2>&1 | grep -E "internal/config|FAIL|ok"`
Expected: `ok ... internal/config`.

- [ ] **Step 7: Commit**

```bash
git add internal/config/config.go internal/config/example.go internal/config/gencfg.go internal/config/gencfg_test.go
git commit -m "config: reflection-based documented-config generator (doc tags + ExampleConfig)"
```

---

## Task 4: `gen-config` subcommand + regenerate `config.sample.toml`

**Files:**
- Create: `cmd/ringer/genconfig.go`
- Create: `cmd/ringer/genconfig_test.go`
- Modify: `config.sample.toml` (regenerated)
- Test: a drift-lock test (`cmd/ringer/genconfig_test.go` or `internal/config/gencfg_test.go`)

**Interfaces:**
- Consumes: `config.RenderDocumented(config.ExampleConfig())`.
- Produces: `ringer gen-config [--output PATH|-o PATH]` — writes the documented config to stdout (default, or when `--output -`) or to a file (refusing to overwrite an existing file unless `--force`). Registered via `parser.AddCommand("gen-config", ...)`.

- [ ] **Step 1: Write the failing subcommand + drift test**

Create `cmd/ringer/genconfig_test.go`:

```go
package main

import (
	"os"
	"strings"
	"testing"

	"github.com/corruptmemory/ringer/internal/config"
)

// The committed config.sample.toml must be exactly what gen-config emits, so it
// can never drift from the config structs again.
func TestConfigSampleIsFresh(t *testing.T) {
	want, err := config.RenderDocumented(config.ExampleConfig())
	if err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile("../../config.sample.toml")
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != want {
		t.Fatalf("config.sample.toml is stale — regenerate with `ringer gen-config -o config.sample.toml`.\nDiff first lines:\n got: %q\nwant: %q", first(string(got)), first(want))
	}
}

func first(s string) string { if i := strings.IndexByte(s, '\n'); i >= 0 { return s[:i] }; return s }
```

- [ ] **Step 2: Run it, confirm it fails**

Run: `./build.sh --test 2>&1 | grep -E "cmd/ringer|FAIL"`
Expected: FAIL — the current hand-written `config.sample.toml` differs from the generated output (and it still has stale Python-era keys).

- [ ] **Step 3: Implement `genconfig.go`**

```go
// cmd/ringer/genconfig.go
package main

import (
	"fmt"
	"os"

	"github.com/corruptmemory/ringer/internal/config"
)

type genConfigCmd struct {
	Output string `short:"o" long:"output" description:"output path, or - for stdout (default stdout)"`
	Force  bool   `long:"force" description:"overwrite an existing output file"`
}

func (c *genConfigCmd) Execute(args []string) error {
	if len(args) > 0 {
		return fmt.Errorf("gen-config: unexpected argument %q", args[0])
	}
	out, err := config.RenderDocumented(config.ExampleConfig())
	if err != nil {
		return err
	}
	if c.Output == "" || c.Output == "-" {
		fmt.Print(out)
		return nil
	}
	if !c.Force {
		if _, err := os.Stat(c.Output); err == nil {
			return fmt.Errorf("gen-config: %s exists (use --force to overwrite)", c.Output)
		}
	}
	if err := os.WriteFile(c.Output, []byte(out), 0o644); err != nil {
		return err
	}
	fmt.Printf("wrote %s\n", c.Output)
	return nil
}

func init() {
	parser.AddCommand("gen-config", "Generate a documented sample config",
		"Generate a documented TOML config from the config structs (self-documenting; won't drift).",
		&genConfigCmd{})
}
```

- [ ] **Step 4: Regenerate `config.sample.toml`**

Build the binary and regenerate the sample from the generator (this drops the stale Python-era keys automatically):

```bash
./build.sh && ./ringer gen-config --force -o config.sample.toml
```

- [ ] **Step 5: Run tests, confirm green**

Run: `./build.sh --test 2>&1 | grep -E "cmd/ringer|internal/config|FAIL|ok"`
Expected: `ok` — `TestConfigSampleIsFresh` passes (committed sample == generated), and any earlier test that read `config.sample.toml` still passes against the regenerated file. If an existing test asserted a specific key/value in the sample, update it to the regenerated content.

- [ ] **Step 6: Commit**

```bash
git add cmd/ringer/genconfig.go cmd/ringer/genconfig_test.go config.sample.toml
git commit -m "cmd: gen-config subcommand; regenerate config.sample.toml (drift-locked)"
```

---

## Task 5: Cost column in the Models display

**Files:**
- Modify: `internal/scoreboard/scoreboard.go` — `FormatShortCost(cost *float64) string`
- Modify: `internal/hud/views/models.templ` — add a Cost column to the HUD Models panel
- Modify: `internal/hud/views/models_scoreboard.templ` — add a Cost column to the `models --html` page
- Test: `internal/scoreboard/scoreboard_test.go` (FormatShortCost table); regenerate the HUD/`--html` goldens

**Interfaces:**
- Consumes: `scoreboard.Row.Cost *float64` (resolved by Task 2 + the 5a scoreboard).
- Produces: `scoreboard.FormatShortCost(cost *float64) string` — port of Python `fmt_short_task_cost`: `nil → "in plan"` (OAuth engines / no catalog match), `0 → "free"`, `< $0.10 → "<1¢"` or `"~N¢"` (rounded cents), else `"$X.XX"`.

**Background:** Task 2 made `Row.Cost` resolve for opencode, but it's only in `--json` + drives `ORDER BY`. This surfaces it as a visible column in the two rollup views (the HUD Models panel and the `--html` scoreboard page — both render `scoreboard.Row`, which carries `Cost`). The CLI `models` table renders per-`(model,task_type)` groups (no `Cost`) and stays as-is (Python parity — cost was never in that table).

- [ ] **Step 1: Write the failing formatter test**

Add to `internal/scoreboard/scoreboard_test.go`:

```go
func TestFormatShortCost(t *testing.T) {
	f := func(v float64) *float64 { return &v }
	cases := []struct {
		cost *float64
		want string
	}{
		{nil, "in plan"},
		{f(0), "free"},
		{f(0.0435), "~4¢"},
		{f(0.005), "<1¢"},
		{f(0.5), "$0.50"},
		{f(1.25), "$1.25"},
	}
	for _, c := range cases {
		if got := FormatShortCost(c.cost); got != c.want {
			t.Errorf("FormatShortCost(%v) = %q, want %q", c.cost, got, c.want)
		}
	}
}
```

- [ ] **Step 2: Run it, confirm it fails**

Run: `./build.sh --test 2>&1 | grep -E "internal/scoreboard|undefined|FAIL"`
Expected: FAIL — `FormatShortCost` undefined.

- [ ] **Step 3: Implement `FormatShortCost`**

Add to `internal/scoreboard/scoreboard.go` (import `math`, `fmt` if not already):

```go
// FormatShortCost renders a per-task estimated cost for display (port of
// Python fmt_short_task_cost): nil -> "in plan" (OAuth-plan engine or no
// catalog match), 0 -> "free", under $0.10 -> cents, else "$X.XX".
func FormatShortCost(cost *float64) string {
	if cost == nil {
		return "in plan"
	}
	v := *cost
	if v == 0 {
		return "free"
	}
	if v < 0.10 {
		cents := v * 100
		if cents < 1 {
			return "<1¢"
		}
		return fmt.Sprintf("~%d¢", int(math.Round(cents)))
	}
	return fmt.Sprintf("$%.2f", v)
}
```

- [ ] **Step 4: Run it, confirm green**

Run: `./build.sh --test 2>&1 | grep -E "internal/scoreboard|FAIL|ok"`

- [ ] **Step 5: Add the Cost column to the two rollup views**

In `internal/hud/views/models.templ` (`ModelsPanel`), add a `<th>Cost</th>` header and a `<td>{ scoreboard.FormatShortCost(r.Cost) }</td>` cell (place it after Tokens, before or after Harness — pick a sensible column order and keep it consistent between the two views). Do the same in `internal/hud/views/models_scoreboard.templ` (the `--html` page). `views` already imports `scoreboard`, so `scoreboard.FormatShortCost` is directly callable.

- [ ] **Step 6: Regenerate goldens + verify**

Run: `./build.sh --test` — the HUD panel / `--html` golden tests will fail on the new column. Regenerate with the sanctioned `go test ./internal/hud/views -run <ModelsPanelGolden|ModelScoreboard> -update`, then re-run `./build.sh --test` and confirm green. (The mock-data goldens will show `in plan` for the cost cell — mock/slowsh have no catalog match — which is the correct display.)

- [ ] **Step 7: Commit**

```bash
git add internal/scoreboard/scoreboard.go internal/scoreboard/scoreboard_test.go internal/hud/views/models.templ internal/hud/views/models_scoreboard.templ internal/hud/views/*_templ.go internal/hud/views/testdata/
git commit -m "hud: cost column in the Models panel + --html scoreboard (FormatShortCost)"
```

---

## Done criteria

- `./build.sh --test --race` green across all packages.
- `ringer run`/`demo` honor `[hud] port` + `--port` (auto-HUD spawns on the configured port); `ringer hud --port` unchanged; generated pages remain port-agnostic.
- The Models scoreboard shows a **cost** for opencode models (`openrouter/<slug>` matched to catalog `<slug>`); codex/grok stay "in plan" (blank).
- `ringer gen-config` prints a documented config; `-o PATH` writes it (no-overwrite unless `--force`); `config.sample.toml` is the generated output and a test keeps it fresh.
- **Deferred to 5c:** agent-integration (`install-agent`/`uninstall-agent` + `nudge-hook`). **5d:** the hard cutover (delete `ringer.py`/`hud/`/`dashboard/`/`scripts/`; README/SKILL sweep; `allow_full_access` gating).
