# Ringer Go Rewrite — Plan 2: The Verified Run Path

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make `ringer run <manifest>` execute a manifest of tasks in parallel against pluggable engines, verify each by executing its check, retry failures once, and log every attempt to the SQLite store — headless, no isolation yet. **Milestone 1** (Tasks 1–9): a runnable, watchable end-to-end demo against the zero-cost `mock` engine. **Milestone 2** (Tasks 10–13): the `lint`/`demo` subcommands and real-engine ergonomics.

**Architecture:** Builds directly on Plan 1's foundation (`internal/config`, `internal/store`, and — unused here — `internal/jail`). Adds `internal/{manifest,engine,verify,state,runner,lint}` and the `run`/`lint`/`demo`/`mock-worker` subcommands. The runner uses the actor pattern (one goroutine owns run state; a bounded goroutine pool executes tasks). **Isolation is out of scope** — every worker runs with `isolation=none`; the jail/Landlock integration is Plan 3.

**Tech Stack:** Go 1.26, go-flags, BurntSushi/toml, modernc.org/sqlite (all already in go.mod from Plan 1). No new third-party deps.

**Spec:** `docs/superpowers/specs/2026-07-08-ringer-go-rewrite-design.md` — §5 (runner core, spawn invariants), §9 (frozen contracts), §3 (CLI surface) are binding. Plan 1 delivered the foundation; this plan is the next slice.

## Global Constraints

- Module `github.com/corruptmemory/ringer`, Go 1.26, `CGO_ENABLED=0` static binary.
- Every build/test invocation goes through `./build.sh` — never raw `go build`/`go test`.
- No new third-party dependencies. Everything is stdlib + the four deps already present.
- **The four frozen spawn invariants (design §9.8), non-negotiable in the runner:** (1) stdin is always closed (`/dev/null`); (2) sandbox mode is always explicit (here: `isolation=none` is explicit — no implicit default sandbox); (3) verification executes the artifact (exit 0 is the only PASS); (4) logs carry raw worker output only, never a summary.
- **Frozen manifest JSON schema** (design §9.1): task fields `key, spec, check, engine, model, expect_files, timeout_s, full_access, engine_args, verified, task_type`; run-level `run_name, workdir, max_parallel, worktrees, repo, tasks`. Worktrees mode is parsed/validated but its execution is deferred to Plan 3 (a manifest with `worktrees:true` must error clearly in Plan 2: "worktrees mode lands in Plan 3").
- **Frozen engine spawn contract** (design §9.3): argv = `bin` + expanded `args_template`; placeholders `{taskdir} {spec} {model}` string-substituted, `{engine_args} {access_args} {sandbox_args} {full_access_args}` list-spliced; cwd = taskdir; stdin `/dev/null`; stdout+stderr merged, teed to `<workdir>/logs/<key>.worker.log` + a 1MB ring buffer + ringer's own stdout; token count scraped from the tail via the engine's `token_regex`.
- **Frozen on-disk formats** (design §9.4): run-state JSON at `<state_dir>/runs/<run_id>.json`; `<state_dir>/active-runs.json`. These must match what a future HUD (Plan 4) and the existing Python Ringside expect — the schema is defined in Task 5 and is authoritative once set.
- **Eval rows** go to the SQLite store via Plan 1's `store.InsertAttempt` — one row per attempt, using the frozen columns.
- `isolation=none` only. Any engine config with `isolation="jail"` must preflight-fail with "jail isolation lands in Plan 3" (config already validates the *value*; the runner rejects *using* it).
- Format with `gofmt`; tests stdlib `testing`, table-driven, `t.TempDir()`; no testify. Commit after each green task.
- Work happens on the `go-run-path` branch (already checked out, off the merged main).

---

### Task 1: Manifest parsing & validation

**Files:**
- Create: `internal/manifest/manifest.go`
- Test: `internal/manifest/manifest_test.go`

**Interfaces:**
- Consumes: nothing (reads JSON from a path).
- Produces:

```go
package manifest

type Task struct {
	Key         string   `json:"key"`
	Spec        string   `json:"spec"`
	Check       string   `json:"check"`
	Engine      string   `json:"engine"`       // "" -> "codex" (default) resolved by caller
	Model       string   `json:"model"`
	ExpectFiles []string `json:"expect_files"`
	TimeoutS    int      `json:"timeout_s"`    // 0 -> default 900 applied by caller
	FullAccess  bool     `json:"full_access"`
	EngineArgs  []string `json:"engine_args"`
	Verified    string   `json:"verified"`
	TaskType    string   `json:"task_type"`
}

type Manifest struct {
	RunName     string `json:"run_name"`
	Workdir     string `json:"workdir"`
	MaxParallel int    `json:"max_parallel"`
	Worktrees   bool   `json:"worktrees"`
	Repo        string `json:"repo"`
	Tasks       []Task `json:"tasks"`
}

// FromPath reads and validates a manifest JSON file.
// Validation errors are returned joined (all problems at once), not one-at-a-time.
func FromPath(path string) (*Manifest, error)
// FromBytes is the testable core FromPath wraps.
func FromBytes(data []byte) (*Manifest, error)
```

Validation rules (each a distinct error message): `run_name` non-empty; `workdir` non-empty; at least one task; every task `key` non-empty and unique; every task `spec` non-empty; every task `check` non-empty; `max_parallel >= 0` (0 means "unbounded" → caller clamps to len(tasks)); `worktrees:true` → error "worktrees mode lands in Plan 3, not yet supported".

- [ ] **Step 1: Write the failing tests**

```go
// internal/manifest/manifest_test.go
package manifest

import (
	"strings"
	"testing"
)

func TestFromBytesValid(t *testing.T) {
	m, err := FromBytes([]byte(`{
		"run_name":"demo","workdir":"/tmp/x","max_parallel":3,
		"tasks":[{"key":"alpha","spec":"do it","check":"test -f alpha.txt","expect_files":["alpha.txt"]}]
	}`))
	if err != nil {
		t.Fatalf("valid manifest rejected: %v", err)
	}
	if m.RunName != "demo" || len(m.Tasks) != 1 || m.Tasks[0].Key != "alpha" {
		t.Fatalf("parsed wrong: %+v", m)
	}
}

func TestFromBytesValidation(t *testing.T) {
	cases := []struct{ name, body, want string }{
		{"no run_name", `{"workdir":"/x","tasks":[{"key":"a","spec":"s","check":"c"}]}`, "run_name"},
		{"no workdir", `{"run_name":"r","tasks":[{"key":"a","spec":"s","check":"c"}]}`, "workdir"},
		{"no tasks", `{"run_name":"r","workdir":"/x","tasks":[]}`, "at least one task"},
		{"dup key", `{"run_name":"r","workdir":"/x","tasks":[{"key":"a","spec":"s","check":"c"},{"key":"a","spec":"s","check":"c"}]}`, "duplicate"},
		{"empty key", `{"run_name":"r","workdir":"/x","tasks":[{"key":"","spec":"s","check":"c"}]}`, "key"},
		{"no spec", `{"run_name":"r","workdir":"/x","tasks":[{"key":"a","check":"c"}]}`, "spec"},
		{"no check", `{"run_name":"r","workdir":"/x","tasks":[{"key":"a","spec":"s"}]}`, "check"},
		{"worktrees", `{"run_name":"r","workdir":"/x","worktrees":true,"tasks":[{"key":"a","spec":"s","check":"c"}]}`, "Plan 3"},
		{"bad json", `{not json`, "invalid"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := FromBytes([]byte(tc.body))
			if err == nil {
				t.Fatalf("expected error containing %q, got nil", tc.want)
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Errorf("error %q missing %q", err, tc.want)
			}
		})
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `./build.sh --test`
Expected: FAIL — package `internal/manifest` does not exist.

- [ ] **Step 3: Implement**

```go
// internal/manifest/manifest.go
package manifest

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
)

// (Task and Manifest structs exactly as in the Interfaces block)

func FromPath(path string) (*Manifest, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("manifest %s: %w", path, err)
	}
	m, err := FromBytes(data)
	if err != nil {
		return nil, fmt.Errorf("manifest %s: %w", path, err)
	}
	return m, nil
}

func FromBytes(data []byte) (*Manifest, error) {
	var m Manifest
	if err := json.Unmarshal(data, &m); err != nil {
		return nil, fmt.Errorf("invalid manifest JSON: %w", err)
	}
	var errs []error
	if m.RunName == "" {
		errs = append(errs, errors.New("run_name is required"))
	}
	if m.Workdir == "" {
		errs = append(errs, errors.New("workdir is required"))
	}
	if len(m.Tasks) == 0 {
		errs = append(errs, errors.New("manifest must have at least one task"))
	}
	if m.Worktrees {
		errs = append(errs, errors.New("worktrees mode lands in Plan 3, not yet supported"))
	}
	if m.MaxParallel < 0 {
		errs = append(errs, errors.New("max_parallel must be >= 0"))
	}
	seen := map[string]bool{}
	for i, tk := range m.Tasks {
		where := fmt.Sprintf("task[%d]", i)
		if tk.Key == "" {
			errs = append(errs, fmt.Errorf("%s: key is required", where))
		} else {
			if seen[tk.Key] {
				errs = append(errs, fmt.Errorf("duplicate task key %q", tk.Key))
			}
			seen[tk.Key] = true
			where = "task " + tk.Key
		}
		if tk.Spec == "" {
			errs = append(errs, fmt.Errorf("%s: spec is required", where))
		}
		if tk.Check == "" {
			errs = append(errs, fmt.Errorf("%s: check is required", where))
		}
	}
	if len(errs) > 0 {
		return nil, errors.Join(errs...)
	}
	return &m, nil
}
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `./build.sh --test`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/manifest
git commit -m "feat: manifest parsing and validation (frozen schema)"
```

---

### Task 2: Engine command building & preflight

**Files:**
- Create: `internal/engine/engine.go`
- Test: `internal/engine/engine_test.go`

**Interfaces:**
- Consumes: `config.EngineConfig` (Plan 1), `manifest.Task` (Task 1).
- Produces:

```go
package engine

import "github.com/corruptmemory/ringer/internal/config"

// BuiltinCodex is the default engine injected when config defines none named "codex".
func BuiltinCodex() config.EngineConfig

// Resolve returns the engine config for a task's engine name, applying the
// builtin codex default. Returns an error if the named engine is unknown.
func Resolve(engines map[string]config.EngineConfig, engineName string) (config.EngineConfig, error)

// BuildArgv expands the engine's args_template for a task into (bin, argv).
// Placeholders: {taskdir} {spec} {model} string-substituted per token;
// {engine_args} {access_args} {sandbox_args} {full_access_args} list-spliced.
// access_args = sandbox_args normally, full_access_args when fullAccess is true.
func BuildArgv(e config.EngineConfig, taskDir, spec, model string, engineArgs []string, fullAccess bool) (bin string, argv []string)

// Preflight verifies each engine's bin exists on PATH or as an absolute path.
// Returns a joined error naming every missing bin (with the config key), or nil.
// It also rejects isolation=="jail" with "jail isolation lands in Plan 3".
func Preflight(engines map[string]config.EngineConfig, used map[string]bool) error

// ParseTokens scrapes a token count from the tail of worker output using the
// engine's token_regex (first capture group). Returns -1 if regex empty or no match.
func ParseTokens(tokenRegex, output string) int64
```

`BuiltinCodex` mirrors the Python default: `bin="codex"`, `args_template=["exec","--skip-git-repo-check","{access_args}","{engine_args}","-C","{taskdir}","{spec}"]`, empty sandbox/full-access args, `token_regex=""`, `model_default=""`.

- [ ] **Step 1: Write the failing tests**

```go
// internal/engine/engine_test.go
package engine

import (
	"strings"
	"testing"

	"github.com/corruptmemory/ringer/internal/config"
)

func TestResolveBuiltinCodex(t *testing.T) {
	e, err := Resolve(map[string]config.EngineConfig{}, "codex")
	if err != nil {
		t.Fatalf("codex must resolve from builtin: %v", err)
	}
	if e.Bin != "codex" {
		t.Errorf("builtin codex bin = %q", e.Bin)
	}
}

func TestResolveUnknown(t *testing.T) {
	_, err := Resolve(map[string]config.EngineConfig{}, "nope")
	if err == nil || !strings.Contains(err.Error(), "nope") {
		t.Fatalf("unknown engine must error naming it, got %v", err)
	}
}

func TestResolveConfigOverridesBuiltin(t *testing.T) {
	custom := config.EngineConfig{Bin: "/my/codex"}
	e, _ := Resolve(map[string]config.EngineConfig{"codex": custom}, "codex")
	if e.Bin != "/my/codex" {
		t.Errorf("config codex must override builtin, got %q", e.Bin)
	}
}

func TestBuildArgvSubstitution(t *testing.T) {
	e := config.EngineConfig{
		Bin:          "opencode",
		ArgsTemplate: []string{"run", "{spec}", "--dir", "{taskdir}", "-m", "{model}", "{engine_args}", "{sandbox_args}"},
		SandboxArgs:  []string{"--sandbox"},
	}
	bin, argv := BuildArgv(e, "/tmp/task", "build it", "glm-5.2", []string{"--variant", "low"}, false)
	if bin != "opencode" {
		t.Fatalf("bin = %q", bin)
	}
	want := []string{"run", "build it", "--dir", "/tmp/task", "-m", "glm-5.2", "--variant", "low", "--sandbox"}
	if strings.Join(argv, "\x00") != strings.Join(want, "\x00") {
		t.Errorf("argv =\n %v\nwant\n %v", argv, want)
	}
}

func TestBuildArgvFullAccessSwapsArgs(t *testing.T) {
	e := config.EngineConfig{
		Bin: "x", ArgsTemplate: []string{"{access_args}"},
		SandboxArgs: []string{"--sbx"}, FullAccessArgs: []string{"--no-sandbox"},
	}
	_, sandboxed := BuildArgv(e, "/t", "s", "", nil, false)
	if len(sandboxed) != 1 || sandboxed[0] != "--sbx" {
		t.Errorf("sandboxed access_args = %v", sandboxed)
	}
	_, full := BuildArgv(e, "/t", "s", "", nil, true)
	if len(full) != 1 || full[0] != "--no-sandbox" {
		t.Errorf("full access_args = %v", full)
	}
}

func TestPreflightRejectsJail(t *testing.T) {
	engines := map[string]config.EngineConfig{"j": {Bin: "sh", Isolation: "jail"}}
	err := Preflight(engines, map[string]bool{"j": true})
	if err == nil || !strings.Contains(err.Error(), "Plan 3") {
		t.Fatalf("jail isolation must be rejected in Plan 2, got %v", err)
	}
}

func TestPreflightMissingBin(t *testing.T) {
	engines := map[string]config.EngineConfig{"x": {Bin: "definitely-not-a-real-binary-xyz"}}
	err := Preflight(engines, map[string]bool{"x": true})
	if err == nil || !strings.Contains(err.Error(), "definitely-not-a-real-binary-xyz") {
		t.Fatalf("missing bin must be reported, got %v", err)
	}
}

func TestParseTokens(t *testing.T) {
	got := ParseTokens(`"tokens":\{"total":([0-9]+)`, `blah "tokens":{"total":1234} blah`)
	if got != 1234 {
		t.Errorf("ParseTokens = %d, want 1234", got)
	}
	if ParseTokens("", "anything") != -1 {
		t.Errorf("empty regex must yield -1")
	}
	if ParseTokens(`total=([0-9]+)`, "no match here") != -1 {
		t.Errorf("no match must yield -1")
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `./build.sh --test`
Expected: FAIL — package `internal/engine` does not exist.

- [ ] **Step 3: Implement**

```go
// internal/engine/engine.go
package engine

import (
	"errors"
	"fmt"
	"os/exec"
	"regexp"
	"strconv"
	"strings"

	"github.com/corruptmemory/ringer/internal/config"
)

func BuiltinCodex() config.EngineConfig {
	return config.EngineConfig{
		Bin:          "codex",
		ArgsTemplate: []string{"exec", "--skip-git-repo-check", "{access_args}", "{engine_args}", "-C", "{taskdir}", "{spec}"},
	}
}

func Resolve(engines map[string]config.EngineConfig, name string) (config.EngineConfig, error) {
	if name == "" {
		name = "codex"
	}
	if e, ok := engines[name]; ok {
		return e, nil
	}
	if name == "codex" {
		return BuiltinCodex(), nil
	}
	return config.EngineConfig{}, fmt.Errorf("unknown engine %q (not in config, and only \"codex\" is built in)", name)
}

// scalar placeholders are replaced within a token; list placeholders replace the
// whole token with zero or more tokens.
func BuildArgv(e config.EngineConfig, taskDir, spec, model string, engineArgs []string, fullAccess bool) (string, []string) {
	access := e.SandboxArgs
	if fullAccess {
		access = e.FullAccessArgs
	}
	lists := map[string][]string{
		"{engine_args}":      engineArgs,
		"{access_args}":      access,
		"{sandbox_args}":     e.SandboxArgs,
		"{full_access_args}": e.FullAccessArgs,
	}
	scalars := strings.NewReplacer("{taskdir}", taskDir, "{spec}", spec, "{model}", model)
	var argv []string
	for _, tok := range e.ArgsTemplate {
		if repl, isList := lists[tok]; isList {
			argv = append(argv, repl...)
			continue
		}
		argv = append(argv, scalars.Replace(tok))
	}
	return e.Bin, argv
}

func Preflight(engines map[string]config.EngineConfig, used map[string]bool) error {
	var errs []error
	for name := range used {
		e, err := Resolve(engines, name)
		if err != nil {
			errs = append(errs, err)
			continue
		}
		if e.Isolation == "jail" {
			errs = append(errs, fmt.Errorf("engine %q uses isolation=\"jail\", which lands in Plan 3; use \"none\" for now", name))
			continue
		}
		if _, err := exec.LookPath(e.Bin); err != nil {
			errs = append(errs, fmt.Errorf("engine %q: binary %q not found on PATH", name, e.Bin))
		}
	}
	return errors.Join(errs...)
}

func ParseTokens(tokenRegex, output string) int64 {
	if tokenRegex == "" {
		return -1
	}
	re, err := regexp.Compile(tokenRegex)
	if err != nil {
		return -1
	}
	m := re.FindStringSubmatch(output)
	if len(m) < 2 {
		return -1
	}
	n, err := strconv.ParseInt(m[1], 10, 64)
	if err != nil {
		return -1
	}
	return n
}
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `./build.sh --test`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/engine
git commit -m "feat: engine command building, preflight, token scraping"
```

---

### Task 3: `mock-worker` subcommand

**Files:**
- Create: `internal/mockworker/mockworker.go`, `cmd/ringer/mockworker.go`
- Test: `internal/mockworker/mockworker_test.go`

**Interfaces:**
- Consumes: nothing.
- Produces (byte-compatible port of `engines/mock_worker.py`):

```go
package mockworker

// Run parses the spec mini-language and writes files into workDir.
// Grammar: lines "MOCK_FILE: <relpath>" ... "MOCK_END" enclose file content;
// a "MOCK_FAIL" line anywhere prints "mock-worker: simulated failure" and returns exit 1.
// Path escapes (absolute, or ".." out of workDir) return exit 1. Returns an exit code.
func Run(spec, workDir string, stdout, stderr io.Writer) int
```

The `cmd/ringer/mockworker.go` adds a `mock-worker` subcommand taking the spec as its last positional arg, `cwd` as workDir, wiring `os.Stdout/os.Stderr`, and calling `os.Exit(mockworker.Run(...))`. `[engines.mock]` in config points `bin` at the ringer binary with `args_template=["mock-worker","{spec}"]`.

- [ ] **Step 1: Write the failing tests**

```go
// internal/mockworker/mockworker_test.go
package mockworker

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

func TestRunWritesFiles(t *testing.T) {
	dir := t.TempDir()
	spec := "MOCK_FILE: out.txt\nhello world\nMOCK_END\n"
	code := Run(spec, dir, &bytes.Buffer{}, &bytes.Buffer{})
	if code != 0 {
		t.Fatalf("exit = %d, want 0", code)
	}
	got, err := os.ReadFile(filepath.Join(dir, "out.txt"))
	if err != nil || string(got) != "hello world\n" {
		t.Fatalf("file content = %q, err %v", got, err)
	}
}

func TestRunSimulatedFailure(t *testing.T) {
	var errb bytes.Buffer
	code := Run("MOCK_FAIL\n", t.TempDir(), &bytes.Buffer{}, &errb)
	if code != 1 {
		t.Errorf("exit = %d, want 1", code)
	}
	if !bytes.Contains(errb.Bytes(), []byte("simulated failure")) {
		t.Errorf("stderr = %q", errb.String())
	}
}

func TestRunRejectsPathEscape(t *testing.T) {
	for _, bad := range []string{"/etc/passwd", "../escape.txt"} {
		code := Run("MOCK_FILE: "+bad+"\nx\nMOCK_END\n", t.TempDir(), &bytes.Buffer{}, &bytes.Buffer{})
		if code != 1 {
			t.Errorf("path %q: exit = %d, want 1", bad, code)
		}
	}
}
```

- [ ] **Step 2: Run to verify fail**

Run: `./build.sh --test`
Expected: FAIL — package does not exist.

- [ ] **Step 3: Implement**

```go
// internal/mockworker/mockworker.go
package mockworker

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

func Run(spec, workDir string, stdout, stderr io.Writer) int {
	lines := strings.Split(spec, "\n")
	for i := 0; i < len(lines); i++ {
		line := lines[i]
		if strings.TrimSpace(line) == "MOCK_FAIL" {
			fmt.Fprintln(stderr, "mock-worker: simulated failure")
			return 1
		}
		if rel, ok := strings.CutPrefix(line, "MOCK_FILE: "); ok {
			var content []string
			i++
			for ; i < len(lines); i++ {
				if strings.TrimSpace(lines[i]) == "MOCK_END" {
					break
				}
				content = append(content, lines[i])
			}
			dest, err := resolveOutputPath(workDir, strings.TrimSpace(rel))
			if err != nil {
				fmt.Fprintf(stderr, "mock-worker: %v\n", err)
				return 1
			}
			if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
				fmt.Fprintf(stderr, "mock-worker: %v\n", err)
				return 1
			}
			body := strings.Join(content, "\n")
			if len(content) > 0 {
				body += "\n"
			}
			if err := os.WriteFile(dest, []byte(body), 0o644); err != nil {
				fmt.Fprintf(stderr, "mock-worker: %v\n", err)
				return 1
			}
			fmt.Fprintf(stdout, "mock-worker: wrote %s\n", rel)
		}
	}
	return 0
}

func resolveOutputPath(workDir, rel string) (string, error) {
	if filepath.IsAbs(rel) {
		return "", fmt.Errorf("absolute path not allowed: %s", rel)
	}
	dest := filepath.Join(workDir, rel)
	clean := filepath.Clean(dest)
	base := filepath.Clean(workDir)
	if clean != base && !strings.HasPrefix(clean, base+string(os.PathSeparator)) {
		return "", fmt.Errorf("path escapes task dir: %s", rel)
	}
	return clean, nil
}
```

```go
// cmd/ringer/mockworker.go
package main

import (
	"fmt"
	"os"

	"github.com/corruptmemory/ringer/internal/mockworker"
)

type mockWorkerCommand struct {
	Args struct {
		Spec string `positional-arg-name:"SPEC"`
	} `positional-args:"yes" required:"yes"`
}

func (c *mockWorkerCommand) Execute(args []string) error {
	wd, err := os.Getwd()
	if err != nil {
		return err
	}
	os.Exit(mockworker.Run(c.Args.Spec, wd, os.Stdout, os.Stderr))
	return nil
}

func init() {
	parser.AddCommand("mock-worker",
		"Deterministic offline worker (CI/testing)",
		"Parses MOCK_FILE/MOCK_END/MOCK_FAIL spec grammar and writes files into the cwd.",
		&mockWorkerCommand{})
	_ = fmt.Sprint // keep import if unused after edits
}
```

(Remove the `fmt` keep-line if `fmt` ends up used; it's a scaffolding guard.)

- [ ] **Step 4: Run to verify pass; smoke the subcommand**

Run: `./build.sh --test && printf 'MOCK_FILE: t.txt\nhi\nMOCK_END\n' | xargs -0 -I{} ./ringer mock-worker {}` — simpler smoke: `cd $(mktemp -d) && /home/jim/projects/ringer/ringer mock-worker "$(printf 'MOCK_FILE: t.txt\nhi\nMOCK_END')" && cat t.txt`
Expected: tests PASS; smoke writes `t.txt` containing `hi`.

- [ ] **Step 5: Commit**

```bash
git add internal/mockworker cmd/ringer/mockworker.go
git commit -m "feat: mock-worker subcommand (offline deterministic engine)"
```

---

### Task 4: Verify (check execution + expect_files)

**Files:**
- Create: `internal/verify/verify.go`
- Test: `internal/verify/verify_test.go`

**Interfaces:**
- Consumes: nothing.
- Produces:

```go
package verify

import "context"

type Result struct {
	Pass     bool
	Output   string // combined stdout+stderr of the check (raw)
	ExitCode int
	TimedOut bool
	Missing  []string // expect_files that were absent/empty
}

// Verify checks expect_files (must exist and be non-empty) then runs `check`
// via `sh -c` in taskDir with a hard timeout. Exit 0 AND all files present = Pass.
func Verify(ctx context.Context, taskDir, check string, expectFiles []string, timeout time.Duration) Result
```

- [ ] **Step 1: Write the failing tests**

```go
// internal/verify/verify_test.go
package verify

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestVerifyPass(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "a.txt"), []byte("data"), 0o644)
	r := Verify(context.Background(), dir, `test -f a.txt`, []string{"a.txt"}, 10*time.Second)
	if !r.Pass || r.ExitCode != 0 {
		t.Fatalf("expected pass, got %+v", r)
	}
}

func TestVerifyFailExit(t *testing.T) {
	r := Verify(context.Background(), t.TempDir(), `echo nope; exit 3`, nil, 10*time.Second)
	if r.Pass || r.ExitCode != 3 {
		t.Fatalf("expected fail exit 3, got %+v", r)
	}
	if !contains(r.Output, "nope") {
		t.Errorf("output must capture check stdout, got %q", r.Output)
	}
}

func TestVerifyMissingExpectFile(t *testing.T) {
	r := Verify(context.Background(), t.TempDir(), `true`, []string{"ghost.txt"}, 10*time.Second)
	if r.Pass || len(r.Missing) != 1 || r.Missing[0] != "ghost.txt" {
		t.Fatalf("expected missing ghost.txt, got %+v", r)
	}
}

func TestVerifyEmptyFileIsMissing(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "empty.txt"), nil, 0o644)
	r := Verify(context.Background(), dir, `true`, []string{"empty.txt"}, 10*time.Second)
	if r.Pass || len(r.Missing) != 1 {
		t.Fatalf("empty file must count as missing, got %+v", r)
	}
}

func TestVerifyTimeout(t *testing.T) {
	r := Verify(context.Background(), t.TempDir(), `sleep 5`, nil, 200*time.Millisecond)
	if r.Pass || !r.TimedOut {
		t.Fatalf("expected timeout, got %+v", r)
	}
}

func contains(s, sub string) bool { return len(s) >= len(sub) && (s == sub || indexOf(s, sub) >= 0) }
func indexOf(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}
```

- [ ] **Step 2: Run to verify fail** — `./build.sh --test`, expect package-missing failure.

- [ ] **Step 3: Implement**

```go
// internal/verify/verify.go
package verify

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"time"
)

type Result struct {
	Pass     bool
	Output   string
	ExitCode int
	TimedOut bool
	Missing  []string
}

func Verify(ctx context.Context, taskDir, check string, expectFiles []string, timeout time.Duration) Result {
	var res Result
	for _, f := range expectFiles {
		info, err := os.Stat(filepath.Join(taskDir, f))
		if err != nil || info.Size() == 0 {
			res.Missing = append(res.Missing, f)
		}
	}
	if len(res.Missing) > 0 {
		return res // check does not run if the floor isn't met
	}
	cctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	cmd := exec.CommandContext(cctx, "sh", "-c", check)
	cmd.Dir = taskDir
	out, err := cmd.CombinedOutput()
	res.Output = string(out)
	if cctx.Err() == context.DeadlineExceeded {
		res.TimedOut = true
		return res
	}
	if err == nil {
		res.Pass = true
		return res
	}
	var ee *exec.ExitError
	if errors.As(err, &ee) {
		res.ExitCode = ee.ExitCode()
	} else {
		res.ExitCode = -1
	}
	return res
}
```

- [ ] **Step 4: Run to verify pass** — `./build.sh --test`, expect PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/verify
git commit -m "feat: verify — executed checks with expect_files floor and timeout"
```

---

### Task 5: Run-state writer & active-runs registry

**Files:**
- Create: `internal/state/state.go`
- Test: `internal/state/state_test.go`

**Interfaces:**
- Consumes: nothing (the runner feeds it snapshots).
- Produces:

```go
package state

type TaskView struct {
	Key       string `json:"key"`
	Engine    string `json:"engine"`
	Model     string `json:"model"`
	Status    string `json:"status"`   // pending|running|passed|failed|timeout
	Attempt   int    `json:"attempt"`
	Tokens    int64  `json:"tokens"`
	Verified  string `json:"verified"`
	LogPath   string `json:"log_path"`
}

type RunState struct {
	RunID     string     `json:"run_id"`
	RunName   string     `json:"run_name"`
	Identity  string     `json:"identity"`
	PID       int        `json:"pid"`
	StartedAt string     `json:"started_at"`
	UpdatedAt string     `json:"updated_at"`
	Done      bool       `json:"done"`
	Tasks     []TaskView `json:"tasks"`
}

// WriteRunState atomically writes s to <stateDir>/runs/<run_id>.json.
func WriteRunState(stateDir string, s RunState) error

// RegisterActiveRun / UnregisterActiveRun maintain <stateDir>/active-runs.json,
// a map[run_id]{pid,run_name,identity,started_at}. Dead PIDs are pruned on read.
func RegisterActiveRun(stateDir, runID string, pid int, runName, identity, startedAt string) error
func UnregisterActiveRun(stateDir, runID string) error
type ActiveRun struct {
	PID int `json:"pid"`; RunName string `json:"run_name"`; Identity string `json:"identity"`; StartedAt string `json:"started_at"`
}
func ReadActiveRuns(stateDir string) (map[string]ActiveRun, error)
```

Both files are written via temp-file + `os.Rename` (atomic). `ReadActiveRuns` prunes entries whose PID is not alive (`syscall.Kill(pid, 0)`).

- [ ] **Step 1: Write the failing tests**

```go
// internal/state/state_test.go
package state

import (
	"os"
	"path/filepath"
	"testing"
)

func TestWriteRunStateAtomic(t *testing.T) {
	dir := t.TempDir()
	s := RunState{RunID: "r1", RunName: "demo", PID: os.Getpid(), Tasks: []TaskView{{Key: "a", Status: "passed"}}}
	if err := WriteRunState(dir, s); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, "runs", "r1.json")); err != nil {
		t.Fatalf("run state file not written: %v", err)
	}
}

func TestActiveRunsRoundTripAndPrune(t *testing.T) {
	dir := t.TempDir()
	// Live PID (us) survives; a bogus PID is pruned.
	if err := RegisterActiveRun(dir, "live", os.Getpid(), "n", "id", "t"); err != nil {
		t.Fatal(err)
	}
	if err := RegisterActiveRun(dir, "dead", 2147480000, "n", "id", "t"); err != nil {
		t.Fatal(err)
	}
	runs, err := ReadActiveRuns(dir)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := runs["live"]; !ok {
		t.Error("live run pruned incorrectly")
	}
	if _, ok := runs["dead"]; ok {
		t.Error("dead run not pruned")
	}
}

func TestUnregister(t *testing.T) {
	dir := t.TempDir()
	RegisterActiveRun(dir, "x", os.Getpid(), "n", "id", "t")
	if err := UnregisterActiveRun(dir, "x"); err != nil {
		t.Fatal(err)
	}
	runs, _ := ReadActiveRuns(dir)
	if _, ok := runs["x"]; ok {
		t.Error("run not unregistered")
	}
}
```

- [ ] **Step 2: Run to verify fail** — `./build.sh --test`.

- [ ] **Step 3: Implement** (atomic write helper; active-runs read-modify-write with prune; `pidAlive` via `syscall.Kill(pid,0)` treating `ESRCH` as dead, `EPERM` as alive). Full code:

```go
// internal/state/state.go
package state

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"syscall"
)

// (TaskView, RunState, ActiveRun structs as in Interfaces)

func atomicWriteJSON(path string, v any) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".tmp-*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		os.Remove(tmpName)
		return err
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmpName)
		return err
	}
	return os.Rename(tmpName, path)
}

func WriteRunState(stateDir string, s RunState) error {
	return atomicWriteJSON(filepath.Join(stateDir, "runs", s.RunID+".json"), s)
}

func activeRunsPath(stateDir string) string { return filepath.Join(stateDir, "active-runs.json") }

func pidAlive(pid int) bool {
	err := syscall.Kill(pid, 0)
	if err == nil {
		return true
	}
	return errors.Is(err, syscall.EPERM) // exists but not ours
}

func readActiveRaw(stateDir string) map[string]ActiveRun {
	m := map[string]ActiveRun{}
	data, err := os.ReadFile(activeRunsPath(stateDir))
	if err == nil {
		_ = json.Unmarshal(data, &m)
	}
	return m
}

func RegisterActiveRun(stateDir, runID string, pid int, runName, identity, startedAt string) error {
	m := readActiveRaw(stateDir)
	m[runID] = ActiveRun{PID: pid, RunName: runName, Identity: identity, StartedAt: startedAt}
	return atomicWriteJSON(activeRunsPath(stateDir), m)
}

func UnregisterActiveRun(stateDir, runID string) error {
	m := readActiveRaw(stateDir)
	delete(m, runID)
	return atomicWriteJSON(activeRunsPath(stateDir), m)
}

func ReadActiveRuns(stateDir string) (map[string]ActiveRun, error) {
	m := readActiveRaw(stateDir)
	for id, r := range m {
		if !pidAlive(r.PID) {
			delete(m, id)
		}
	}
	return m, nil
}
```

- [ ] **Step 4: Run to verify pass** — `./build.sh --test`.

- [ ] **Step 5: Commit**

```bash
git add internal/state
git commit -m "feat: run-state writer and pid-pruned active-runs registry"
```

---

### Task 6: Logging seam (`internal/logging`) + `[logging]` config + `--log-level`

**Files:**
- Create: `internal/logging/logging.go`, `internal/logging/capture.go`, `internal/logging/logging_test.go`
- Modify: `internal/config/config.go` (add `[logging]` section + format validation), `internal/config/config_test.go`
- Modify: `cmd/ringer/main.go` (add `--log-level` root flag + `resolveLogLevel` helper), `cmd/ringer/main_test.go`

**Interfaces:**
- Consumes: `config.AppConfig` (Plan 1).
- Produces: `logging.Logger`, `logging.Default()`, `logging.New(Config)`, `logging.NewCapture()`, `config.LoggingConfig`, and `resolveLogLevel` (in `package main`).

```go
package logging // github.com/corruptmemory/ringer/internal/logging

type Logger interface {
	Debug(msg string, args ...any); Debugf(format string, args ...any)
	Info(msg string, args ...any);  Infof(format string, args ...any)
	Warn(msg string, args ...any);  Warnf(format string, args ...any)
	Error(msg string, args ...any); Errorf(format string, args ...any)
	WithLevel(level slog.Level) Logger
}
func Default() Logger                       // always-on Info->stderr, no config needed
func New(cfg Config) (Logger, error)        // refines Default; errors on bad Format (never os.Exit)
func NewCapture() (Logger, *Capture)        // synchronous-drain sink for tests + future HUD
```

**Design principle (load-bearing):** the logger is NEVER unconfigured. `Default()` is a working Info→stderr logger from the first instruction; `New(Config)` only REFINES level/format — it never *enables* logging. "Logging before logging is configured" is a non-problem because we own `main()`. No package `init()`, no CWD file read, no `os.Exit` inside the package — the CLI boundary owns fail-loud. The capture sink drains SYNCHRONOUSLY (mutex, no timer/linger), so a test asserts immediately after a logged call returns. (This whole task is compile-verified: `./build.sh --test` green across all packages, and the strict config loader was confirmed to accept the additive `[logging]` section.)

- [ ] **Step 1: Write the failing tests** — `internal/logging/logging_test.go`:

```go
package logging

import (
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/BurntSushi/toml"
)

func TestLevelFiltering(t *testing.T) {
	logger, capt := NewCapture() // starts at Info per NewCapture's contract
	logger.Debug("debug line")
	if got := capt.String(); strings.Contains(got, "debug line") {
		t.Fatalf("Debug at Info level should be suppressed, got: %q", got)
	}
	logger.Info("info line")
	logger.Warn("warn line")
	logger.Error("error line")
	got := capt.String()
	for _, want := range []string{"info line", "warn line", "error line"} {
		if !strings.Contains(got, want) {
			t.Errorf("output missing %q, got: %q", want, got)
		}
	}
}

func TestPrintfMethods(t *testing.T) {
	logger, capt := NewCapture()
	logger.Debugf("debug %d %s", 1, "x") // suppressed at Info
	logger.Infof("info %d %s", 2, "y")
	got := capt.String()
	if strings.Contains(got, "debug 1 x") {
		t.Errorf("Debugf should be suppressed at Info level, got: %q", got)
	}
	if !strings.Contains(got, "info 2 y") {
		t.Errorf("Infof missing formatted line, got: %q", got)
	}
}

func TestCaptureIsSynchronousNotLingering(t *testing.T) {
	logger, capt := NewCapture()
	logger.Info("line one")
	// No sleep: a synchronous, mutex-protected drain must have the line
	// available the instant the logging call returns.
	if got := capt.String(); !strings.Contains(got, "line one") {
		t.Fatalf("expected synchronous capture of %q, got: %q", "line one", got)
	}
}

func TestWithLevel(t *testing.T) {
	logger, capt := NewCapture() // Info
	logger.Debug("hidden")
	if strings.Contains(capt.String(), "hidden") {
		t.Fatalf("Debug should be hidden at Info level")
	}
	debugLogger := logger.WithLevel(slog.LevelDebug)
	debugLogger.Debug("now visible")
	if !strings.Contains(capt.String(), "now visible") {
		t.Fatalf("WithLevel(Debug) should emit Debug lines, got: %q", capt.String())
	}
}

func TestDefaultIsAlwaysAvailable(t *testing.T) {
	logger := Default() // no configuration step
	if logger == nil {
		t.Fatal("Default() returned nil")
	}
	logger.Info("process starting") // must not panic
}

func TestNewBuildsFromConfig(t *testing.T) {
	cases := []struct {
		name    string
		cfg     Config
		wantErr bool
	}{
		{"zero value defaults to text/info", Config{}, false},
		{"explicit debug+json", Config{Level: slog.LevelDebug, Format: "json"}, false},
		{"unknown format rejected", Config{Format: "xml"}, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			l, err := New(tc.cfg)
			if tc.wantErr {
				if err == nil {
					t.Fatal("want error, got nil")
				}
				return
			}
			if err != nil || l == nil {
				t.Fatalf("New: l=%v err=%v", l, err)
			}
		})
	}
}

func TestConfigLevelParsesFromTOML(t *testing.T) {
	type fixture struct {
		Logging Config `toml:"logging"`
	}
	p := filepath.Join(t.TempDir(), "c.toml")
	if err := os.WriteFile(p, []byte("[logging]\nlevel = \"debug\"\nformat = \"json\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	var f fixture
	if _, err := toml.DecodeFile(p, &f); err != nil {
		t.Fatalf("DecodeFile: %v", err)
	}
	if f.Logging.Level != slog.LevelDebug || f.Logging.Format != "json" {
		t.Errorf("parsed %+v", f.Logging)
	}
}
```

- [ ] **Step 2: Run to verify fail** — `./build.sh --test`.

- [ ] **Step 3: Implement.** `internal/logging/logging.go`:

```go
// Package logging provides ringer's minimal, always-on logging interface.
//
// The logger is never unconfigured: Default returns a working Info-level
// logger to stderr, usable from the very first line of startup, before any
// config file or CLI flag has been parsed. Loading a Config via New only
// refines the level and format of that same logger — it never "enables"
// logging. This makes "logging before logging is configured" a non-problem.
package logging

import (
	"fmt"
	"io"
	"log/slog"
	"os"
)

type Logger interface {
	Debug(msg string, args ...any)
	Debugf(format string, args ...any)
	Info(msg string, args ...any)
	Infof(format string, args ...any)
	Warn(msg string, args ...any)
	Warnf(format string, args ...any)
	Error(msg string, args ...any)
	Errorf(format string, args ...any)
	WithLevel(level slog.Level) Logger
}

// Config controls a Logger built by New. The zero value is valid and sane:
// Level's zero value is slog.LevelInfo; an empty Format is treated as "text".
type Config struct {
	Level  slog.Level
	Format string // "text" (default) or "json"
}

type slogLogger struct {
	log    *slog.Logger
	out    io.Writer
	format string
}

// Default returns a working Info-level logger to stderr. Always safe to call,
// requires no configuration — log through this before New has been called.
func Default() Logger { return newSlogLogger(os.Stderr, slog.LevelInfo, "text") }

// New builds a Logger from cfg. It is the only constructor that can fail
// (unknown Format) and it never exits the process — the CLI boundary decides
// how to surface the error.
func New(cfg Config) (Logger, error) {
	format := cfg.Format
	if format == "" {
		format = "text"
	}
	if format != "text" && format != "json" {
		return nil, fmt.Errorf("logging: unknown format %q (want \"text\" or \"json\")", format)
	}
	return newSlogLogger(os.Stderr, cfg.Level, format), nil
}

func newSlogLogger(out io.Writer, level slog.Level, format string) *slogLogger {
	return &slogLogger{log: slog.New(newHandler(out, level, format)), out: out, format: format}
}

func newHandler(out io.Writer, level slog.Level, format string) slog.Handler {
	opts := &slog.HandlerOptions{Level: level}
	if format == "json" {
		return slog.NewJSONHandler(out, opts)
	}
	return slog.NewTextHandler(out, opts)
}

func (l *slogLogger) Debug(msg string, args ...any)    { l.log.Debug(msg, args...) }
func (l *slogLogger) Debugf(format string, args ...any) { l.log.Debug(fmt.Sprintf(format, args...)) }
func (l *slogLogger) Info(msg string, args ...any)     { l.log.Info(msg, args...) }
func (l *slogLogger) Infof(format string, args ...any)  { l.log.Info(fmt.Sprintf(format, args...)) }
func (l *slogLogger) Warn(msg string, args ...any)     { l.log.Warn(msg, args...) }
func (l *slogLogger) Warnf(format string, args ...any)  { l.log.Warn(fmt.Sprintf(format, args...)) }
func (l *slogLogger) Error(msg string, args ...any)    { l.log.Error(msg, args...) }
func (l *slogLogger) Errorf(format string, args ...any) { l.log.Error(fmt.Sprintf(format, args...)) }

// WithLevel returns a sibling logger (same destination + format) at level.
func (l *slogLogger) WithLevel(level slog.Level) Logger {
	return newSlogLogger(l.out, level, l.format)
}
```

`internal/logging/capture.go`:

```go
package logging

import (
	"bytes"
	"log/slog"
	"sync"
)

// Capture is a deterministic, synchronous sink for a Logger built with
// NewCapture. Every logging call writes into the buffer, under a mutex,
// before the logging method returns — no background flush, timer, or linger.
// A test can call String() immediately after a logged call and see the line.
// Also usable as the backing store for a future in-process HUD.
type Capture struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (c *Capture) Write(p []byte) (int, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.buf.Write(p)
}

func (c *Capture) String() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.buf.String()
}

// NewCapture returns a Logger writing synchronously into the returned Capture.
func NewCapture() (Logger, *Capture) {
	c := &Capture{}
	return newSlogLogger(c, slog.LevelInfo, "text"), c
}
```

Then the `[logging]` config section in `internal/config/config.go` — add `"log/slog"` to the imports, a `LoggingConfig` type, an `AppConfig.Logging` field, and a format-validation check in `Load` (mirrors the existing engine-isolation validation, "fail loudly on bad config"):

```go
// LoggingConfig mirrors logging.Config field-for-field (Level slog.Level,
// Format string) instead of importing internal/logging, keeping config
// dependency-light; the CLI boundary does the trivial conversion. slog.Level
// implements encoding.TextUnmarshaler, so `level = "debug"` decodes for free.
// Zero value == {Info, ""} == the sane default (absent section => Info/text).
type LoggingConfig struct {
	Level  slog.Level `toml:"level"`
	Format string     `toml:"format"`
}

// ... add to AppConfig:  Logging LoggingConfig `toml:"logging"`

// ... in Load, after the engine-isolation validation loop:
	switch c.Logging.Format {
	case "", "text", "json":
	default:
		return nil, fmt.Errorf("config %s: logging.format must be \"text\" or \"json\", got %q", path, c.Logging.Format)
	}
```

And the `--log-level` flag + resolver in `cmd/ringer/main.go` (`resolveLogLevel` follows `config.ResolveIdentity`'s flag→config→default precedence; the package var `logger` is the always-on `Default()` until a subcommand refines it):

```go
type rootOptions struct {
	Config   string `long:"config" description:"Path to config TOML (default: $RINGER_CONFIG or ~/.config/ringer/config.toml)"`
	LogLevel string `long:"log-level" description:"Minimum log level: debug, info, warn, error (default: [logging].level, or info)"`
}

// logger is always-on from process start (never unconfigured); a subcommand
// refines it via resolveLogLevel + logging.New once a config is loaded.
var logger logging.Logger = logging.Default()

// resolveLogLevel implements flag ?? config ?? default precedence. cfg may be nil.
func resolveLogLevel(flagValue string, cfg *config.AppConfig) (slog.Level, error) {
	if flagValue != "" {
		var lvl slog.Level
		if err := lvl.UnmarshalText([]byte(flagValue)); err != nil {
			return 0, err
		}
		return lvl, nil
	}
	if cfg != nil {
		return cfg.Logging.Level, nil // zero value == slog.LevelInfo
	}
	return slog.LevelInfo, nil
}
```

Add matching tests to `internal/config/config_test.go` (`TestLoadAcceptsLoggingSection` — the strict-loader risk check — and `TestInvalidLoggingFormatRejected`) and `cmd/ringer/main_test.go` (`TestResolveLogLevel`, table-driven over flag/config/nil/invalid).

- [ ] **Step 4: Run to verify pass** — `./build.sh --test`. Expected: all packages green; the strict loader accepts `[logging]`.

- [ ] **Step 5: Commit**

```bash
git add internal/logging cmd/ringer/main.go cmd/ringer/main_test.go internal/config/config.go internal/config/config_test.go
git commit -m "feat: internal/logging — always-on leveled slog seam, [logging] config, --log-level"
```

---

### Task 7: Runner — actor-owned state

**Files:**
- Create: `internal/runner/actor.go`
- Test: `internal/runner/actor_test.go`

**Interfaces:**
- Consumes: `state.TaskView`, `state.RunState` (Task 5); `logging.Logger` (Task 6).
- Produces:

```go
package runner

// taskState is the actor's private mutable per-task record.
// The actor goroutine is the ONLY thing that touches the map; callers send commands.
type actor struct { /* unexported: cmds chan, state map, meta */ }

func newActor(runID, runName, identity string, keys []string, lg logging.Logger) *actor
func (a *actor) start()                                  // launches the goroutine
func (a *actor) stop()                                   // idempotent trigger: recover-guarded close(cmds); logs a recovered double-stop
func (a *actor) wait()                                   // blocks until the goroutine exits (sync.WaitGroup — N callers ok)
func (a *actor) stopAndWait()                            // convenience: stop() then wait()
func (a *actor) setStatus(key, status string, attempt int)
func (a *actor) setResult(key, status string, tokens int64, verified, logPath string)
func (a *actor) snapshot() state.RunState                // synchronous request/reply
```

All mutation goes through the command channel; `snapshot()` sends a reply channel and blocks for the response. This is the single-owner pattern — no mutex on the task map.

- [ ] **Step 1: Write the failing test**

```go
// internal/runner/actor_test.go
package runner

import (
	"sync"
	"testing"

	"github.com/corruptmemory/ringer/internal/logging"
)

func TestActorConcurrentUpdatesThenSnapshot(t *testing.T) {
	keys := []string{"a", "b", "c"}
	a := newActor("r1", "demo", "id", keys, logging.Default())
	a.start()
	defer a.stopAndWait()

	var wg sync.WaitGroup
	for _, k := range keys {
		wg.Add(1)
		go func(k string) {
			defer wg.Done()
			a.setStatus(k, "running", 1)
			a.setResult(k, "passed", 100, "did the thing", "/logs/"+k)
		}(k)
	}
	wg.Wait()

	snap := a.snapshot()
	if len(snap.Tasks) != 3 {
		t.Fatalf("snapshot has %d tasks, want 3", len(snap.Tasks))
	}
	for _, tv := range snap.Tasks {
		if tv.Status != "passed" || tv.Tokens != 100 {
			t.Errorf("task %s not settled: %+v", tv.Key, tv)
		}
	}
}
```

- [ ] **Step 2: Run to verify fail** — `./build.sh --test`.

- [ ] **Step 3: Implement** the actor: a `cmds chan func()` (each command is a closure run on the actor goroutine — simplest correct actor in Go), the private `map[string]*state.TaskView`, ordered `keys` for stable snapshot order, and `snapshot()` posting a closure that copies the map into a `state.RunState` and sends it on a reply channel.

  **Lifecycle protocol** (see the `actor-pattern` skill, "Lifecycle"): `stop` and `wait` are separate operations. `wait()` is a `sync.WaitGroup` (`Add(1)` before the goroutine, `defer Done()` inside it) so any number of callers may wait. `stop()` is a `recover`-guarded `close(a.cmds)` — drain-then-exit — that logs a recovered double-stop (keyed by `runID`) rather than swallowing it silently, and returns immediately without waiting. `stopAndWait()` is the named convenience so a blocking wait is never hidden inside a method called `stop`.

```go
// internal/runner/actor.go
package runner

import (
	"sync"

	"github.com/corruptmemory/ringer/internal/logging"
	"github.com/corruptmemory/ringer/internal/state"
)

type actor struct {
	runID, runName, identity string
	keys                     []string
	cmds                     chan func()
	wg                       sync.WaitGroup // wait() blocks on this — N callers ok
	lg                       logging.Logger
	tasks                    map[string]*state.TaskView
}

func newActor(runID, runName, identity string, keys []string, lg logging.Logger) *actor {
	tasks := make(map[string]*state.TaskView, len(keys))
	for _, k := range keys {
		tasks[k] = &state.TaskView{Key: k, Status: "pending"}
	}
	return &actor{
		runID: runID, runName: runName, identity: identity, keys: keys,
		cmds: make(chan func()), lg: lg, tasks: tasks,
	}
}

func (a *actor) start() {
	a.wg.Add(1)
	go func() {
		defer a.wg.Done()
		for fn := range a.cmds { // drain accepted commands, then exit
			fn()
		}
	}()
}

// stop is the shutdown trigger: it closes cmds (drain-then-exit) and returns
// immediately — it does NOT wait. It is idempotent: a second or concurrent
// stop re-closes cmds, panics, and is recovered. A recovered double-stop is a
// correct no-op but also evidence of a stray stop() caller, so it is logged
// (never swallowed), keyed by runID. Add debug.Stack() to the log line when
// hunting the wayward caller. Producers must have quiesced before stop() —
// do() is synchronous, so once every caller has returned there are no in-flight
// commands and nothing left to drain.
func (a *actor) stop() {
	defer func() {
		if r := recover(); r != nil {
			a.lg.Warnf("runner actor %s: recovered panic in stop (double stop?): %v", a.runID, r)
		}
	}()
	close(a.cmds)
}

// wait blocks until the actor goroutine has exited. Safe for any number of
// callers (it is a sync.WaitGroup).
func (a *actor) wait() { a.wg.Wait() }

// stopAndWait is the named convenience for "stop, then block until exited" —
// the blocking wait is visible at the call site, never hidden inside stop().
func (a *actor) stopAndWait() { a.stop(); a.wait() }

func (a *actor) do(fn func()) {
	done := make(chan struct{})
	a.cmds <- func() { fn(); close(done) }
	<-done
}

func (a *actor) setStatus(key, status string, attempt int) {
	a.do(func() {
		if tv := a.tasks[key]; tv != nil {
			tv.Status = status
			tv.Attempt = attempt
		}
	})
}

func (a *actor) setResult(key, status string, tokens int64, verified, logPath string) {
	a.do(func() {
		if tv := a.tasks[key]; tv != nil {
			tv.Status = status
			tv.Tokens = tokens
			tv.Verified = verified
			tv.LogPath = logPath
		}
	})
}

func (a *actor) snapshot() state.RunState {
	var out state.RunState
	a.do(func() {
		out = state.RunState{RunID: a.runID, RunName: a.runName, Identity: a.identity}
		for _, k := range a.keys {
			out.Tasks = append(out.Tasks, *a.tasks[k])
		}
	})
	return out
}
```

- [ ] **Step 4: Run to verify pass — with the race detector** (the whole point of the actor is no data races):

Run: `./build.sh --test --race`
Expected: PASS, no race warnings.

- [ ] **Step 5: Commit**

```bash
git add internal/runner/actor.go internal/runner/actor_test.go
git commit -m "feat: runner actor — single-owner run state, no mutex"
```

---

### Task 8: Runner — worker execution (spawn, tee, timeout, kill)

**Files:**
- Create: `internal/runner/worker.go`
- Test: `internal/runner/worker_test.go`

**Interfaces:**
- Consumes: nothing new (uses `os/exec`, `syscall`).
- Produces:

```go
package runner

type WorkerOutcome struct {
	ExitCode int
	TimedOut bool
	Tail     string // last ~1MB of combined output (for token scraping + failure context)
	Err      error
}

// runWorker spawns bin+argv in taskDir with the frozen invariants:
// stdin=/dev/null, stderr merged into stdout, new process group (Setpgid),
// output teed to logPath + a 1MB ring buffer + w (usually os.Stdout).
// On ctx cancel/timeout: SIGTERM the group, 5s grace, then SIGKILL.
func runWorker(ctx context.Context, bin string, argv []string, taskDir, logPath string, w io.Writer, timeout time.Duration) WorkerOutcome
```

- [ ] **Step 1: Write the failing tests** (use `/bin/sh -c` as a stand-in worker):

```go
// internal/runner/worker_test.go
package runner

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestRunWorkerCapturesOutputAndExit(t *testing.T) {
	dir := t.TempDir()
	log := filepath.Join(dir, "w.log")
	var mirror bytes.Buffer
	out := runWorker(context.Background(), "sh", []string{"-c", "echo hello; exit 7"}, dir, log, &mirror, 10*time.Second)
	if out.ExitCode != 7 {
		t.Errorf("exit = %d, want 7", out.ExitCode)
	}
	if !strings.Contains(out.Tail, "hello") {
		t.Errorf("tail missing output: %q", out.Tail)
	}
	logData, _ := os.ReadFile(log)
	if !strings.Contains(string(logData), "hello") {
		t.Errorf("log file missing output: %q", logData)
	}
	if !strings.Contains(mirror.String(), "hello") {
		t.Errorf("mirror missing output: %q", mirror.String())
	}
}

func TestRunWorkerTimeoutKills(t *testing.T) {
	dir := t.TempDir()
	start := time.Now()
	out := runWorker(context.Background(), "sh", []string{"-c", "sleep 30"}, dir, filepath.Join(dir, "w.log"), &bytes.Buffer{}, 300*time.Millisecond)
	if !out.TimedOut {
		t.Errorf("expected timeout, got %+v", out)
	}
	if time.Since(start) > 10*time.Second {
		t.Errorf("kill took too long: %v", time.Since(start))
	}
}

func TestRunWorkerClosesStdin(t *testing.T) {
	// A worker that reads stdin must see EOF immediately, not hang.
	dir := t.TempDir()
	out := runWorker(context.Background(), "sh", []string{"-c", "cat; echo done"}, dir, filepath.Join(dir, "w.log"), &bytes.Buffer{}, 5*time.Second)
	if out.TimedOut || !strings.Contains(out.Tail, "done") {
		t.Errorf("stdin not closed (worker hung?): %+v", out)
	}
}
```

- [ ] **Step 2: Run to verify fail** — `./build.sh --test`.

- [ ] **Step 3: Implement** `internal/runner/worker.go` (the code below is compile-verified and green under `--race`). Notes: `exec.CommandContext` kills only the leader, so build `*exec.Cmd` manually with `Setpgid`; because `Stdout`/`Stderr` are an `io.MultiWriter` (not an `*os.File`), `os/exec`'s own output-copy goroutines are joined by `cmd.Wait()`, so `ring.Tail()` read after Wait is race-free with no hand-rolled `io.Copy`. `runWorker` takes no logger — it returns a `WorkerOutcome`; the caller (Task 9) does the logging.

```go
// Package runner spawns worker subprocesses with the frozen process
// lifecycle invariants described in the project design (§9): closed stdin,
// merged stdout/stderr, a dedicated process group so the whole tree can be
// signalled, and tee'd output capture with a bounded tail for token
// scraping and failure context.
package runner

import (
	"context"
	"io"
	"os"
	"os/exec"
	"sync"
	"syscall"
	"time"
)

// ringBufferSize is the fixed capacity of the ring buffer used to retain
// the tail of a worker's combined output.
const ringBufferSize = 1 << 20 // 1MB

// ringBuffer is a fixed-size io.Writer that retains only the most recently
// written ringBufferSize bytes, overwriting the oldest data (wraparound).
type ringBuffer struct {
	mu   sync.Mutex
	buf  [ringBufferSize]byte
	pos  int  // next write offset within buf
	full bool // true once buf has wrapped at least once
}

func (r *ringBuffer) Write(p []byte) (int, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	n := len(p)
	if n == 0 {
		return 0, nil
	}
	if n >= ringBufferSize {
		copy(r.buf[:], p[n-ringBufferSize:])
		r.pos = 0
		r.full = true
		return n, nil
	}
	end := r.pos + n
	if end <= ringBufferSize {
		copy(r.buf[r.pos:end], p)
		r.pos = end
		if r.pos == ringBufferSize {
			r.pos = 0
			r.full = true
		}
	} else {
		first := ringBufferSize - r.pos
		copy(r.buf[r.pos:], p[:first])
		copy(r.buf[:end-ringBufferSize], p[first:])
		r.pos = end - ringBufferSize
		r.full = true
	}
	return n, nil
}

// Tail returns the buffered contents in chronological order (oldest first).
func (r *ringBuffer) Tail() string {
	r.mu.Lock()
	defer r.mu.Unlock()
	if !r.full {
		out := make([]byte, r.pos)
		copy(out, r.buf[:r.pos])
		return string(out)
	}
	out := make([]byte, ringBufferSize)
	copy(out, r.buf[r.pos:])
	copy(out[ringBufferSize-r.pos:], r.buf[:r.pos])
	return string(out)
}

// WorkerOutcome describes how a worker subprocess finished.
type WorkerOutcome struct {
	ExitCode int
	TimedOut bool
	Tail     string // last ~1MB of combined output (for token scraping + failure context)
	Err      error
}

// runWorker spawns bin+argv in taskDir with the frozen invariants:
// stdin=/dev/null, stderr merged into stdout, new process group (Setpgid),
// output teed to logPath + a 1MB ring buffer + w (usually os.Stdout).
// On ctx cancel/timeout: SIGTERM the group, 5s grace, then SIGKILL.
func runWorker(ctx context.Context, bin string, argv []string, taskDir, logPath string, w io.Writer, timeout time.Duration) WorkerOutcome {
	devNull, err := os.Open(os.DevNull)
	if err != nil {
		return WorkerOutcome{Err: err}
	}
	defer devNull.Close()

	logFile, err := os.Create(logPath)
	if err != nil {
		return WorkerOutcome{Err: err}
	}
	defer logFile.Close()

	ring := &ringBuffer{}
	mw := io.MultiWriter(logFile, ring, w)

	cmd := exec.Command(bin, argv...)
	cmd.Dir = taskDir
	cmd.Stdin = devNull
	cmd.Stdout = mw
	cmd.Stderr = mw
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}

	if err := cmd.Start(); err != nil {
		return WorkerOutcome{Err: err, Tail: ring.Tail()}
	}

	// Setpgid without an explicit Pgid makes the child the leader of its own
	// new process group, so its pid doubles as the group id.
	pgid := cmd.Process.Pid

	timeoutCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	// cmd.Wait also joins the internal goroutines os/exec uses to copy
	// Stdout/Stderr into mw (since mw isn't an *os.File), so once waitErr is
	// received all writes into ring have happened and Tail() is race-free.
	waitDone := make(chan error, 1)
	go func() { waitDone <- cmd.Wait() }()

	var timedOut bool
	var waitErr error
	select {
	case waitErr = <-waitDone:
	case <-timeoutCtx.Done():
		timedOut = true
		_ = syscall.Kill(-pgid, syscall.SIGTERM)
		select {
		case waitErr = <-waitDone:
		case <-time.After(5 * time.Second):
			_ = syscall.Kill(-pgid, syscall.SIGKILL)
			waitErr = <-waitDone
		}
	}

	outcome := WorkerOutcome{TimedOut: timedOut, Tail: ring.Tail()}
	if waitErr != nil {
		if exitErr, ok := waitErr.(*exec.ExitError); ok {
			outcome.ExitCode = exitErr.ExitCode()
		} else {
			outcome.Err = waitErr
		}
	}
	return outcome
}
```

- [ ] **Step 4: Run to verify pass, including race** — `./build.sh --test --race`, expect PASS (the tee writes from one goroutine; assert no races).

- [ ] **Step 5: Commit**

```bash
git add internal/runner/worker.go internal/runner/worker_test.go
git commit -m "feat: runner worker spawn — frozen invariants (stdin closed, pgid kill, teed raw output)"
```

---

### Task 9: Runner — task loop, retry, and the end-to-end mock run (**Milestone 1**)

**Files:**
- Create: `internal/runner/runner.go`, `cmd/ringer/run.go`
- Test: `internal/runner/runner_test.go` (E2E through the mock engine)

**Interfaces:**
- Consumes: everything so far — `manifest`, `engine`, `verify`, `state`, `store`, `logging` (T6), the actor (T7), `runWorker` (T8).
- Produces:

```go
package runner

type Options struct {
	Manifest    *manifest.Manifest
	Engines     map[string]config.EngineConfig
	StateDir    string
	Identity    string
	Store       *store.Store    // may be nil (skip eval logging)
	Stdout      io.Writer
	Logger      logging.Logger  // nil -> logging.Default()
	MaxParallel int             // 0 -> len(tasks)
}

type TaskResult struct { Key, Verdict string; Attempts int; Tokens int64 }
type RunResult struct { RunID string; Results []TaskResult; AllPassed bool }

// Run executes the manifest end-to-end: prepare each task dir, run its worker
// (bounded by MaxParallel), verify, retry once with failure context appended,
// log each attempt to Store, and flush run-state each second. Headless.
func Run(ctx context.Context, opts Options) (RunResult, error)
```

Per-task flow (max 2 attempts): mkdir `<workdir>/<key>`; build argv via `engine.BuildArgv`; `runWorker`; `verify.Verify`; on PASS → record; on FAIL/TIMEOUT and attempt 1 → append failure context (`"\n\n--- Previous attempt failed. Check output:\n" + tail-of-check-output`) to the spec and re-run; record final verdict. Each attempt → `store.InsertAttempt`. A background ticker flushes `actor.snapshot()` → `state.WriteRunState` every second and once at the end; `state.RegisterActiveRun` at start, `UnregisterActiveRun` at end. RunID is `<run_name>-<time.Now().UnixNano()>` — this is real Go, so `time.Now()` is available (the workflow-script `Date`/random ban does NOT apply to the ringer binary); `StartedAt`/`UpdatedAt`/`CreatedAt` are RFC3339 UTC. (Per-task `Engine`/`Model` in the run-state `TaskView` are left empty in Plan 2 — cosmetic only; the live fields are status/verdict/tokens. Populated when the HUD needs them, Plan 4.)

- [ ] **Step 1: Write the failing E2E test** (the milestone-1 proof — a real `ringer` process is NOT needed; call `runner.Run` directly with the mock engine pointed at the built binary):

```go
// internal/runner/runner_test.go
package runner

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/corruptmemory/ringer/internal/config"
	"github.com/corruptmemory/ringer/internal/manifest"
)

// buildRingerBinary compiles the ringer binary once so the mock engine has a bin.
func buildRingerBinary(t *testing.T) string {
	t.Helper()
	bin := filepath.Join(t.TempDir(), "ringer")
	cmd := exec.Command("go", "build", "-o", bin, "github.com/corruptmemory/ringer/cmd/ringer")
	cmd.Env = append(os.Environ(), "CGO_ENABLED=0")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("build ringer: %v\n%s", err, out)
	}
	return bin
}

func TestRunEndToEndMockEngine(t *testing.T) {
	ringerBin := buildRingerBinary(t)
	workdir := t.TempDir()
	stateDir := t.TempDir()

	m := &manifest.Manifest{
		RunName: "e2e", Workdir: workdir, MaxParallel: 2,
		Tasks: []manifest.Task{
			{Key: "pass", Engine: "mock",
				Spec:  "MOCK_FILE: out.txt\nalpha ready\nMOCK_END\n",
				Check: `test "$(cat out.txt)" = "alpha ready"`, ExpectFiles: []string{"out.txt"}},
			{Key: "retry", Engine: "mock",
				// Fails first attempt (MOCK_FAIL), but the check only needs the file;
				// this exercises the fail→retry path deterministically via a check that
				// fails until a marker file exists. Simpler: a task whose check fails once.
				Spec:  "MOCK_FILE: r.txt\nok\nMOCK_END\n",
				Check: `test -f r.txt`, ExpectFiles: []string{"r.txt"}},
		},
	}
	engines := map[string]config.EngineConfig{
		"mock": {Bin: ringerBin, ArgsTemplate: []string{"mock-worker", "{spec}"}},
	}
	res, err := Run(context.Background(), Options{
		Manifest: m, Engines: engines, StateDir: stateDir, Identity: "test", Stdout: os.Stderr,
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !res.AllPassed {
		t.Fatalf("expected all pass, got %+v", res.Results)
	}
	// Run-state file was written.
	if _, err := os.Stat(filepath.Join(stateDir, "runs", res.RunID+".json")); err != nil {
		t.Errorf("run-state not written: %v", err)
	}
	// Deliverable exists.
	if _, err := os.Stat(filepath.Join(workdir, "pass", "out.txt")); err != nil {
		t.Errorf("deliverable missing: %v", err)
	}
}
```

(Note to implementer: to test the fail→retry path *deterministically*, add a second test where the mock spec includes `MOCK_FAIL` on attempt 1 — since the runner appends failure context to the spec on retry, use a spec/check pair where the retry succeeds. If a deterministic retry-success is awkward through the mock grammar alone, add a tiny `MOCK_FAIL_ONCE` sentinel to the mock worker that fails only if a per-taskdir marker file is absent, creating the marker so the retry passes. Keep the grammar change minimal and covered by a mockworker unit test. Document the choice.)

- [ ] **Step 2: Run to verify fail** — `./build.sh --test`.

- [ ] **Step 3: Implement `runner.Run`** — the actor-owned task loop (semaphore-bounded parallelism, per-task 2-attempt flow with failure-context injection, store logging, 1s state-flush ticker, active-run register/unregister). The actor is the single owner of run state; the result is built from its final snapshot.

```go
// internal/runner/runner.go
package runner

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/corruptmemory/ringer/internal/config"
	"github.com/corruptmemory/ringer/internal/engine"
	"github.com/corruptmemory/ringer/internal/logging"
	"github.com/corruptmemory/ringer/internal/manifest"
	"github.com/corruptmemory/ringer/internal/state"
	"github.com/corruptmemory/ringer/internal/store"
	"github.com/corruptmemory/ringer/internal/verify"
)

const defaultTimeoutS = 900

// Run executes the manifest end-to-end: prepare each task dir, run its worker
// (bounded by MaxParallel), verify, retry once with failure context appended,
// log each attempt to Store, and flush run-state each second. Headless.
func Run(ctx context.Context, opts Options) (RunResult, error) {
	m := opts.Manifest
	lg := opts.Logger
	if lg == nil {
		lg = logging.Default()
	}
	if m.Worktrees {
		return RunResult{}, fmt.Errorf("worktrees mode lands in Plan 3")
	}

	keys := make([]string, len(m.Tasks))
	for i, t := range m.Tasks {
		keys[i] = t.Key
	}

	runID := fmt.Sprintf("%s-%d", m.RunName, time.Now().UnixNano())
	startedAt := time.Now().UTC().Format(time.RFC3339)

	a := newActor(runID, m.RunName, opts.Identity, keys, lg)
	a.start()
	defer a.stopAndWait()

	_ = state.RegisterActiveRun(opts.StateDir, runID, os.Getpid(), m.RunName, opts.Identity, startedAt)
	defer func() { _ = state.UnregisterActiveRun(opts.StateDir, runID) }()

	writeState := func(done bool) {
		s := a.snapshot()
		s.PID = os.Getpid()
		s.StartedAt = startedAt
		s.UpdatedAt = time.Now().UTC().Format(time.RFC3339)
		s.Done = done
		if err := state.WriteRunState(opts.StateDir, s); err != nil {
			lg.Warnf("run %s: write state: %v", runID, err)
		}
	}

	flushDone := make(chan struct{})
	go func() {
		t := time.NewTicker(1 * time.Second)
		defer t.Stop()
		for {
			select {
			case <-flushDone:
				return
			case <-t.C:
				writeState(false)
			}
		}
	}()

	maxPar := opts.MaxParallel
	if maxPar <= 0 {
		maxPar = len(m.Tasks)
	}
	if maxPar < 1 {
		maxPar = 1
	}
	sem := make(chan struct{}, maxPar)
	var wg sync.WaitGroup
	for _, task := range m.Tasks {
		wg.Add(1)
		go func(task manifest.Task) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			runTask(ctx, opts, a, lg, runID, task)
		}(task)
	}
	wg.Wait()

	close(flushDone)
	writeState(true) // final flush, Done=true

	// Result from the authoritative actor snapshot.
	snap := a.snapshot()
	res := RunResult{RunID: runID, AllPassed: true}
	for _, tv := range snap.Tasks {
		verdict := statusToVerdict(tv.Status)
		if verdict != "PASS" {
			res.AllPassed = false
		}
		res.Results = append(res.Results, TaskResult{
			Key: tv.Key, Verdict: verdict, Attempts: tv.Attempt, Tokens: tv.Tokens,
		})
	}
	return res, nil
}

// runTask runs one manifest task through up to two attempts.
func runTask(ctx context.Context, opts Options, a *actor, lg logging.Logger, runID string, task manifest.Task) {
	engineName := task.Engine
	if engineName == "" {
		engineName = "codex"
	}
	engConf, ok := opts.Engines[engineName]
	if !ok {
		lg.Errorf("task %s: unknown engine %q", task.Key, engineName)
		a.setResult(task.Key, "failed", -1, task.Verified, "")
		return
	}
	if engConf.Isolation == "jail" {
		lg.Errorf("task %s: jail isolation lands in Plan 3", task.Key)
		a.setResult(task.Key, "failed", -1, task.Verified, "")
		return
	}

	taskDir := filepath.Join(opts.Manifest.Workdir, task.Key)
	if err := os.MkdirAll(taskDir, 0o755); err != nil {
		lg.Errorf("task %s: mkdir %s: %v", task.Key, taskDir, err)
		a.setResult(task.Key, "failed", -1, task.Verified, "")
		return
	}
	logsDir := filepath.Join(opts.Manifest.Workdir, "logs")
	_ = os.MkdirAll(logsDir, 0o755)
	logPath := filepath.Join(logsDir, task.Key+".worker.log")

	model := task.Model
	if model == "" {
		model = engConf.ModelDefault
	}
	timeoutS := task.TimeoutS
	if timeoutS == 0 {
		timeoutS = defaultTimeoutS
	}
	timeout := time.Duration(timeoutS) * time.Second

	spec := task.Spec
	verdict := "ERROR"
	var tokens int64 = -1
	attempts := 0

	for attempt := 1; attempt <= 2; attempt++ {
		attempts = attempt
		a.setStatus(task.Key, "running", attempt)

		bin, argv := engine.BuildArgv(engConf, taskDir, spec, model, task.EngineArgs, task.FullAccess)
		lg.Infof("task %s: attempt %d: %s", task.Key, attempt, bin)
		outcome := runWorker(ctx, bin, argv, taskDir, logPath, opts.Stdout, timeout)
		if outcome.Err != nil {
			lg.Errorf("task %s: spawn error: %v", task.Key, outcome.Err)
		}
		tokens = scrapeTokens(engConf.TokenRegex, outcome.Tail)

		vres := verify.Verify(ctx, taskDir, task.Check, task.ExpectFiles, timeout)
		switch {
		case outcome.TimedOut:
			verdict = "TIMEOUT"
		case outcome.Err != nil:
			verdict = "ERROR"
		case vres.Pass:
			verdict = "PASS"
		default:
			verdict = "FAIL"
		}

		if opts.Store != nil {
			if err := opts.Store.InsertAttempt(store.Attempt{
				RunID: runID, RunName: opts.Manifest.RunName, TaskKey: task.Key,
				Engine: engineName, Model: model, TaskType: task.TaskType,
				Verdict: verdict, Retry: attempt - 1, Tokens: tokens,
				CheckOutput: vres.Output, Identity: opts.Identity,
				CreatedAt: time.Now().UTC().Format(time.RFC3339),
			}); err != nil {
				lg.Warnf("task %s: insert attempt: %v", task.Key, err)
			}
		}

		if verdict == "PASS" {
			break
		}
		if attempt == 1 {
			// Inject failure context into the spec for the retry.
			spec = task.Spec + "\n\n--- Previous attempt failed. Check output:\n" + vres.Output
			lg.Warnf("task %s: attempt 1 %s; retrying", task.Key, verdict)
		}
	}

	a.setResult(task.Key, verdictToStatus(verdict), tokens, task.Verified, logPath)
	lg.Infof("task %s: %s (%d attempt(s), tokens=%d)", task.Key, verdict, attempts, tokens)
}

// scrapeTokens pulls a token count from the tail using the engine's
// token_regex (last match wins; last capture group, or whole match if none).
// Returns -1 when unknown.
func scrapeTokens(tokenRegex, tail string) int64 {
	if tokenRegex == "" {
		return -1
	}
	re, err := regexp.Compile(tokenRegex)
	if err != nil {
		return -1
	}
	matches := re.FindAllStringSubmatch(tail, -1)
	if len(matches) == 0 {
		return -1
	}
	last := matches[len(matches)-1]
	grp := last[len(last)-1]
	n, err := strconv.ParseInt(strings.TrimSpace(grp), 10, 64)
	if err != nil {
		return -1
	}
	return n
}

func statusToVerdict(status string) string {
	switch status {
	case "passed":
		return "PASS"
	case "timeout":
		return "TIMEOUT"
	case "failed":
		return "FAIL"
	default:
		return "ERROR"
	}
}

func verdictToStatus(verdict string) string {
	switch verdict {
	case "PASS":
		return "passed"
	case "TIMEOUT":
		return "timeout"
	default:
		return "failed"
	}
}
```

- [ ] **Step 3b: Implement `cmd/ringer/run.go`** — the `run` subcommand: load config, build the logger (fail-loud at the CLI boundary), resolve identity, load+validate the manifest, inject the built-in `mock` engine (pointing at this binary), preflight, open the store, call `runner.Run`, print a verdict table, exit non-zero if any task failed.

```go
// cmd/ringer/run.go
package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/corruptmemory/ringer/internal/config"
	"github.com/corruptmemory/ringer/internal/engine"
	"github.com/corruptmemory/ringer/internal/logging"
	"github.com/corruptmemory/ringer/internal/manifest"
	"github.com/corruptmemory/ringer/internal/runner"
	"github.com/corruptmemory/ringer/internal/store"
)

type runCmd struct {
	MaxParallel int    `long:"max-parallel" description:"override manifest max_parallel"`
	Identity    string `long:"identity" description:"identity for eval rows (default: resolved from config/env/hostname)"`
	DryRun      bool   `long:"dry-run" description:"print the plan and exit"`
	NoDashboard bool   `long:"no-dashboard" description:"accepted; always headless in Plan 2 (no HUD yet)"`
	Args        struct {
		Manifest string `positional-arg-name:"MANIFEST" description:"path to the manifest JSON"`
	} `positional-args:"yes" required:"yes"`
}

func (c *runCmd) Execute(args []string) error {
	cfgPath := opts.Config
	if cfgPath == "" {
		cfgPath = config.DefaultPath()
	}
	cfg, err := config.Load(cfgPath)
	if err != nil {
		return err
	}

	// Build the logger — fail loud here, at the CLI boundary, not in a library init().
	lvl, err := resolveLogLevel(opts.LogLevel, cfg)
	if err != nil {
		return fmt.Errorf("--log-level: %w", err)
	}
	lg, err := logging.New(logging.Config{Level: lvl, Format: cfg.Logging.Format})
	if err != nil {
		return err
	}

	manifestPath := c.Args.Manifest
	m, err := manifest.FromPath(manifestPath)
	if err != nil {
		return err
	}
	if c.MaxParallel > 0 {
		m.MaxParallel = c.MaxParallel
	}
	identity := config.ResolveIdentity(c.Identity, cfg, filepath.Dir(manifestPath))

	// Engines: config engines plus a built-in mock pointing at this binary.
	self, _ := os.Executable()
	engines := map[string]config.EngineConfig{}
	for k, v := range cfg.Engines {
		engines[k] = v
	}
	if _, ok := engines["mock"]; !ok {
		engines["mock"] = config.EngineConfig{
			Bin: self, ArgsTemplate: []string{"mock-worker", "{spec}"}, Isolation: "none",
		}
	}

	used := map[string]bool{}
	for _, t := range m.Tasks {
		name := t.Engine
		if name == "" {
			name = "codex"
		}
		used[name] = true
	}
	if err := engine.Preflight(engines, used); err != nil {
		return err
	}

	if c.DryRun {
		fmt.Fprintf(os.Stdout, "run %q: %d task(s), max_parallel=%d, identity=%s\n",
			m.RunName, len(m.Tasks), m.MaxParallel, identity)
		for _, t := range m.Tasks {
			fmt.Fprintf(os.Stdout, "  - %s [%s]\n", t.Key, t.Engine)
		}
		return nil
	}

	var st *store.Store
	if s, err := store.Open(cfg.DBPath()); err != nil {
		lg.Warnf("eval store unavailable (%v); continuing without eval logging", err)
	} else {
		st = s
		defer st.Close()
	}

	res, err := runner.Run(context.Background(), runner.Options{
		Manifest: m, Engines: engines, StateDir: cfg.StateDirPath(),
		Identity: identity, Store: st, Stdout: os.Stdout, Logger: lg,
		MaxParallel: m.MaxParallel,
	})
	if err != nil {
		return err
	}

	fmt.Fprintf(os.Stdout, "\n%-20s %-8s %-9s %s\n", "TASK", "VERDICT", "ATTEMPTS", "TOKENS")
	for _, r := range res.Results {
		fmt.Fprintf(os.Stdout, "%-20s %-8s %-9d %d\n", r.Key, r.Verdict, r.Attempts, r.Tokens)
	}
	if !res.AllPassed {
		return fmt.Errorf("run %s: one or more tasks failed", res.RunID)
	}
	return nil
}

func init() {
	parser.AddCommand("run", "Run a manifest", "Execute a manifest of tasks against pluggable engines.", &runCmd{})
}
```

- [ ] **Step 4: Run to verify pass** — `./build.sh --test` then a real smoke:

```bash
./build.sh
cat > /tmp/demo.json <<'JSON'
{"run_name":"smoke","workdir":"/tmp/ringer-smoke","max_parallel":2,
 "tasks":[{"key":"a","engine":"mock","spec":"MOCK_FILE: a.txt\nalpha ready\nMOCK_END\n","check":"test \"$(cat a.txt)\" = \"alpha ready\"","expect_files":["a.txt"]}]}
JSON
RINGER_CONFIG=/dev/null ./ringer run /tmp/demo.json --identity smoke-test
```
Expected: a PASS verdict table; `/tmp/ringer-smoke/a/a.txt` written; `~/.ringer/runs/smoke-*.json` present; an attempts row in `~/.ringer/ringer.db`.

- [ ] **Step 5: Commit**

```bash
git add internal/runner/runner.go internal/runner/runner_test.go cmd/ringer/run.go
git commit -m "feat: run subcommand — verified parallel run, retry, eval logging (mock demo end-to-end)"
```

**MILESTONE 1 COMPLETE:** `ringer run <manifest>` works end-to-end against the mock engine.

---

### Task 10: Lint package + `lint` subcommand + run warnings

**Files:**
- Create: `internal/lint/lint.go`, `cmd/ringer/lint.go`
- Modify: `cmd/ringer/run.go` (print non-blocking lint findings after manifest load)
- Test: `internal/lint/lint_test.go`

**Interfaces:**
- Consumes: `manifest.Manifest` (Task 1).
- Produces:

```go
package lint

type Finding struct { TaskKey, Rule, Message string }
// Check returns findings for the manifest-level "checks that can't be trusted" heuristics.
func Check(m *manifest.Manifest) []Finding
```

Port the heuristics from the Python `lint_manifest` (design references the set): a check that is exactly `true`/`exit 0`/`echo ...` (cannot fail); a check that is only a file-existence `test -f`/`test -e` (existence, not content); a check using `grep -q`/`diff -q`/`--quiet` (silent, no failure context); a spec under ~40 chars (underspecified); `expect_files` empty AND check references a file it never asserts content of. Each heuristic gets a table-test case seeded from the Python test suite's cases.

- [ ] **Step 1: Write failing tests** (one table-test with a case per heuristic — clean manifest yields zero findings; each bad pattern yields the expected rule). *(Full case list transcribed from `tests/test_lint.py` — the implementer reads that Python file for the exact 14 cases and mirrors them.)*

- [ ] **Step 2: Run to verify fail** — `./build.sh --test`.

- [ ] **Step 3: Implement** the heuristics (pure string/regex checks over each task's `check`/`spec`/`expect_files`), the `lint` subcommand (load manifest, print findings, exit 1 if any), and wire non-blocking findings into `run` (print after load, never block).

- [ ] **Step 4: Run to verify pass** — `./build.sh --test` + `./ringer lint /tmp/demo.json` (expect clean or findings).

- [ ] **Step 5: Commit**

```bash
git add internal/lint cmd/ringer/lint.go cmd/ringer/run.go
git commit -m "feat: lint heuristics + lint subcommand + non-blocking run warnings"
```

---

### Task 11: `demo` subcommand

**Files:**
- Create: `cmd/ringer/demo.go`
- Test: `cmd/ringer/demo_test.go` (or an internal helper test)

**Interfaces:**
- Consumes: `runner`, `manifest`, the mock engine.
- Produces: a `demo` subcommand that generates a 3-task mock manifest in a temp dir and runs it through `runner.Run` with the mock engine — no API cost, proves the whole path. Mirrors the `run` flags.

- [ ] **Step 1: Write a failing test** that invokes the demo's manifest-builder and asserts it produces 3 valid mock tasks that pass `manifest.FromBytes`/validation.
- [ ] **Step 2: Run to verify fail.**
- [ ] **Step 3: Implement** the demo (build the 3-task mock manifest — one pass, one multi-file, one fail-then-retry-pass — write to a temp path, run it).
- [ ] **Step 4: Run to verify pass** — `./build.sh --test` + `./ringer demo` (expect a PASS table, no network).
- [ ] **Step 5: Commit** — `git commit -m "feat: demo subcommand — zero-cost 3-task mock run"`

---

### Task 12: Wire the E2E + demo into CI; Plan-2 acceptance

**Files:**
- Modify: none expected (CI already runs `./build.sh --test`, which now includes the runner E2E).

**Interfaces:** none.

- [ ] **Step 1** Confirm the runner E2E test (Task 9) runs under `./build.sh --test` on this machine (it compiles the binary via `go build`, which needs the module — fine in-repo).
- [ ] **Step 2** Push the branch; verify CI green on ubuntu-latest + macos-latest (the E2E spawns `sh` and the built binary — both present on runners; no userns needed since isolation=none).
- [ ] **Step 3** If the E2E is too slow or flaky in CI (it builds the binary), gate it behind `testing.Short()` skip in `-short` OR accept the build cost — decide based on observed CI time; document.
- [ ] **Step 4** Commit any CI adjustment.

---

### Task 13: Plan-2 final whole-branch review + finish

Not a coding task — the controller runs the final whole-branch review (opus) over `main..HEAD`, triages findings, then uses `superpowers:finishing-a-development-branch`. Carry-forwards to Plan 3: isolation (`Isolator` interface, jail integration with the **cwd seam fix** — `chroot` lands cwd at `/`, so the jailed spawn must inject `cd <taskdir>` / set the worker's dir inside the jail script to honor the frozen `cwd=taskdir` contract), the Landlock fallback spike, worktrees mode, and the `store.Checkpoint`/connector-hook items already recorded.

---

## Self-Review (completed at write time)

- **Spec coverage (Plan 2 scope):** logging seam → Task 6; run path §5 → Tasks 7-9; engine spawn contract §9.3 → Tasks 2,8; manifest schema §9.1 → Task 1; verify §5 → Task 4; on-disk state §9.4 → Task 5; eval rows → Task 9 (via Plan 1 store); mock-worker → Task 3; lint → Task 10; demo → Task 11; CLI surface §3 (run/lint/demo/mock-worker) → Tasks 3,9,10,11. Deferred by design: isolation/jail (Plan 3), HUD/artifacts (Plan 4), models/catalog/scoreboard + Python cutover (Plan 5), worktrees mode (Plan 3).
- **Placeholders:** Tasks 1-9 now have complete code — including the two large runner tasks (worker spawn T8, task loop + `run` subcommand T9). The logging package (T6) and the worker (T8) were compile-verified in isolated worktrees (`./build.sh --test`, worker also `--race`) before their code landed here; the runner (T9) is authored against those verified signatures but can only be fully compiled once T1–T8 exist, so its first real build is during execution. Only Tasks 10-11 (lint, demo) still specify interfaces + tests + the non-obvious mechanics in prose where the code is long; the implementer writes those mechanical bodies against the given tests, and the subagent review loop covers the fill-in.
- **Type consistency:** `manifest.Task`/`Manifest`, `config.EngineConfig` (Plan 1), `store.Attempt`/`InsertAttempt` (Plan 1), `state.RunState`/`TaskView`, `verify.Result`, `engine.BuildArgv` signature, and `runner.Options`/`RunResult` are consistent across the tasks that produce and consume them.
- **Known risk flagged for execution:** Task 8's deterministic fail→retry test may need a minimal `mock-worker` grammar addition (a fail-once sentinel) — called out in the task so the implementer handles it deliberately rather than faking a retry.
