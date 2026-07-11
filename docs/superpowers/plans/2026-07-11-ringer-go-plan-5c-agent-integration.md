# Ringer Go Plan 5c — Agent Integration Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Port the two remaining Claude Code integration surfaces from Python to Go — the routing-nudge hook (`hooks/ringer_nudge.py` → `ringer nudge-hook pre-bash|post-edit`) and the skill/hook installer (`ringer.py`'s `install-agent`/`uninstall-agent`) — so the 5d cutover can delete the Python.

**Architecture:** Two new leaf packages. `internal/nudge` is the hook runtime: it reads a Claude Code hook payload on stdin, applies the frozen gating rules (provider/harness regexes, edit-spiral counter, live-run suppression, per-session dedupe markers), and prints a `hookSpecificOutput.additionalContext` JSON object on a trigger — always exiting 0. `internal/agent` is the installer: idempotent `settings.json` hook merge (over `map[string]any` so unknown user keys survive) + backup + embedded SKILL.md copy. Two thin `cmd/ringer` subcommand files wire them to go-flags. The nudge runtime reuses `internal/state.ReadActiveRuns` for live-run detection; the installer embeds a committed copy of the skill, drift-locked to the canonical file the way `config.sample.toml` is drift-locked to `gen-config`.

**Tech Stack:** Go 1.26, module `github.com/corruptmemory/ringer`, `CGO_ENABLED=0`, go-flags subcommands, stdlib `regexp` (RE2), `crypto/sha256`, `encoding/json`, `//go:embed`. Build/test ONLY via `./build.sh` and `./build.sh --test [--race]`.

## Global Constraints

These are frozen external contracts (spec §3, §4, §9.9) — copy the exact values; do not paraphrase.

- **Module path:** `github.com/corruptmemory/ringer`. New packages: `internal/nudge`, `internal/agent`.
- **nudge-hook contract (spec §9.9 item 9):** stdin hook JSON → stdout `hookSpecificOutput.additionalContext`; **ALWAYS exit 0** (a hook must never break the agent's tool call). Modes: `pre-bash`, `post-edit`. Unknown/missing mode → exit 0, no output. Malformed/empty stdin → exit 0, no output.
- **Nudge text (byte-identical to `ringer_nudge.py` `NUDGE_TEXT`):**
  ```
  Ringer routing check: this looks like swarm-shaped work happening inline (model call/harness/edit loop outside a live Ringer run). Load the ringer skill and route it as a manifest — a single task is a one-task manifest. If the user explicitly asked for inline work, proceed.
  ```
- **PROVIDER regex (case-insensitive):** `(api\.anthropic\.com|api\.openai\.com|openrouter\.ai|generativelanguage\.googleapis|/v1/chat/completions|/v1/messages)`
- **HARNESS regex (case-insensitive):** `\b(?:node|python3?|bun|deno)\s+\S*(?:simulat|probe|smoke|harness|persona|grader|eval)\S*\.(?:mjs|js|ts|py)\b`
- **Hook event names emitted:** pre-bash → `PreToolUse`; post-edit → `PostToolUse`.
- **Post-edit trigger threshold:** nudge only when `count >= 8` AND `distinct_files >= 3` (and no live run). The counter increments on every post-edit event regardless.
- **Dedupe marker:** `sha256(session_id + "\x00" + event)` hex + `.<event>.nudged`, under `<home>/nudge-state/`; claimed via `O_CREATE|O_EXCL` (a second claim in the same session/event returns "already nudged").
- **Ringer home resolution (nudge-hook):** `$RINGER_HOME` (expanded) if non-empty, else `~/.ringer`. This mirrors the Python hook's external env contract; it is NOT the config's `state_dir`.
- **install-agent hook registration (spec §3, §4):** two hooks, **binary path not python3** —
  - `PreToolUse` / matcher `Bash` / command `<ringer-binary> nudge-hook pre-bash`
  - `PostToolUse` / matcher `Edit|Write` / command `<ringer-binary> nudge-hook post-edit`
- **install-agent must be idempotent + preserve unknown settings keys + back up an existing `settings.json`.** Model `settings.json` as `map[string]any` (a typed struct would silently drop the user's other keys).
- **`settings.json` write format (match Python `json.dumps(indent=2, sort_keys=True) + "\n"`):** 2-space indent, sorted keys (encoding/json sorts `map[string]any` keys), `SetEscapeHTML(false)`, trailing newline, atomic tmp+rename.
- **Scopes:** `--project` → `./.claude`; default (user) → `~/.claude`. Skill target: `<root>/skills/ringer/SKILL.md`. Settings: `<root>/settings.json`.
- **install/uninstall failures are loud** (non-zero exit) — mirror Python, whose `main()` prints `error:` to stderr and returns 2. In Go, return the error from `Execute` (go-flags prints it, exits 1).

## Source references (Python being ported)

- `hooks/ringer_nudge.py` (243 lines) — the whole nudge runtime.
- `ringer.py:7844-7900` — `claude_root`, `ringer_skill_source`, `ringer_hook_command`, `backup_file`, `load_settings`, `write_settings`, `hook_command_contains`, `event_has_ringer_hook`.
- `ringer.py:7902-8019` — `merge_ringer_hook`, `remove_ringer_hooks`, `install_agent`, `uninstall_agent`.
- `ringer.py:8208-8226` — argparse wiring + dispatch.
- `internal/state/state.go:104-158` — `ReadActiveRuns` (read + prune-dead-pids + write-back), reused for live-run gating.
- `cmd/ringer/genconfig.go` — the go-flags subcommand idiom (struct + `Execute` + `init()`+`parser.AddCommand`).

## Design decisions (deviations from a literal transcription — banner these for Jim)

1. **Two packages, not one.** Spec §4 lists only `internal/agent`. This plan adds `internal/nudge` for the hook runtime (a distinct concern from install/uninstall). Both are small, focused leaf packages.
2. **SKILL.md is embedded as a committed copy** at `internal/agent/SKILL.md` (`//go:embed` cannot reach `.claude/…` — a dot-directory). A drift-lock test (`TestEmbeddedSkillMatchesCanonical`) asserts it equals `.claude/skills/ringer/SKILL.md` — the exact pattern `config.sample.toml`/`TestConfigSampleIsFresh` uses. **Alternative Jim may prefer later:** make `.claude/skills/ringer/SKILL.md` a symlink to a single embeddable source (zero duplication, matches how `registry/model-identity.toml` is embedded once). Chose the copy for this unobserved run because it never touches the live skill file. **The embedded SKILL.md still contains Python-era `ringer.py` references — that content sweep is 5d's job (spec §11); 5c ports the install *mechanism*, not the skill *prose*. When 5d edits the canonical file, the drift-lock test forces the embed to be regenerated.**
3. **Hook marker = `nudge-hook`** (substring of `<binary> nudge-hook <action>`, path-independent). Analog of Python's `ringer_nudge.py` needle. Legacy `python3 …/ringer_nudge.py` hooks are intentionally not matched (idempotency + removal) — cleaning those belongs to 5d / a README migration note.
4. **Ringer-invocation guard is tokenized.** Python's pre-bash gate skips commands containing the literal `"ringer.py"`. The Go analog skips a command whose whitespace tokens include `ringer`, `ringer.py`, `*/ringer`, or `*/ringer.py` (so `ringer run x` / `/usr/bin/ringer demo` are skipped, but `python3 ringer_probe.py` — legitimately swarm-shaped — is NOT). A blunt `Contains(cmd,"ringer")` would wrongly suppress that probe nudge.
5. **Dead code dropped.** Python's `command_references_active_workdir` is unreachable (the `if active_runs: return False` above it already returns). Omitted, with a comment.
6. **Benign divergence on corrupt `active-runs.json`.** `state.ReadActiveRuns` treats unparseable JSON as "no live runs" (returns empty); Python raised → caught → exit 0 with no nudge. Net: on a corrupt state file Go may emit an advisory nudge where Python stayed silent. Harmless, arguably more correct, both exit 0.
7. **Nudge stdout is compact JSON** (`json.Marshal`), Python used `json.dump` (spaces after `:`/`,`). Whitespace-only difference; Claude Code parses JSON. Structure + exit-0 are the frozen contract.

## File Structure

- `internal/nudge/nudge.go` (create) — regexes, `NudgeText`, home resolution, gating predicates, dedupe markers, post-edit state, `Run(mode, stdin, stdout, home)`. Imports `internal/config` (for `ExpandUser`) and `internal/state` (for `ReadActiveRuns`).
- `internal/nudge/nudge_test.go` (create) — table tests for every predicate + `Run` end-to-end with `t.TempDir()` homes.
- `cmd/ringer/nudgehook.go` (create) — `nudge-hook` go-flags subcommand (positional `MODE`; always exit 0).
- `cmd/ringer/nudgehook_test.go` (create).
- `internal/agent/settings.go` (create) — `map[string]any` settings merge/remove/load/write+backup + marker helpers.
- `internal/agent/settings_test.go` (create).
- `internal/agent/agent.go` (create) — `HookCommand`, `Install`, `Uninstall`, result structs, frozen hook constants.
- `internal/agent/skill.go` (create) — `//go:embed SKILL.md` → `SkillMarkdown`.
- `internal/agent/SKILL.md` (create) — committed copy of `.claude/skills/ringer/SKILL.md`.
- `internal/agent/agent_test.go` (create) — Install/Uninstall against a temp root + drift-lock test.
- `cmd/ringer/agent.go` (create) — `install-agent`/`uninstall-agent` subcommands (`--project`).
- `cmd/ringer/agent_test.go` (create).

Dependency direction (no cycles): `nudge → config, state`; `agent → (stdlib + embed)`; `cmd/ringer → nudge, agent`.

---

## Task 1: `internal/nudge` — the nudge engine

Ports all of `hooks/ringer_nudge.py`: regexes, gating, dedupe markers, post-edit state, and the `Run` driver. One cohesive package/file; heavily table-tested.

**Files:**
- Create: `internal/nudge/nudge.go`
- Test: `internal/nudge/nudge_test.go`

**Interfaces:**
- Consumes: `config.ExpandUser(string) string`; `state.ReadActiveRuns(stateDir string) (map[string]state.ActiveRun, error)`.
- Produces: `nudge.Run(mode string, stdin io.Reader, stdout io.Writer, home string) error`; `nudge.RingerHome() string`; `nudge.NudgeText` (string const). Task 2 consumes exactly these three.

- [ ] **Step 1: Write `internal/nudge/nudge.go`**

```go
// Package nudge ports hooks/ringer_nudge.py: the Claude Code hook that nudges
// the agent to route swarm-shaped work through a Ringer manifest instead of
// running model calls / harness scripts / edit-spirals inline. It is invoked
// as `ringer nudge-hook pre-bash|post-edit` with the hook payload JSON on
// stdin; on a trigger it prints a hookSpecificOutput.additionalContext JSON
// object to stdout. It ALWAYS results in exit 0 (a hook must never break the
// agent's tool call — spec §9.9).
package nudge

import (
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/corruptmemory/ringer/internal/config"
	"github.com/corruptmemory/ringer/internal/state"
)

// NudgeText is the advisory injected as additionalContext. Frozen —
// byte-identical to ringer_nudge.py NUDGE_TEXT.
const NudgeText = "Ringer routing check: this looks like swarm-shaped work happening inline " +
	"(model call/harness/edit loop outside a live Ringer run). Load the ringer " +
	"skill and route it as a manifest — a single task is a one-task manifest. " +
	"If the user explicitly asked for inline work, proceed."

// Frozen provider/harness detectors. RE2-safe (no lookaround); (?i) = the
// re.IGNORECASE the Python patterns carry.
var (
	providerRE = regexp.MustCompile(`(?i)(api\.anthropic\.com|api\.openai\.com|openrouter\.ai|generativelanguage\.googleapis|/v1/chat/completions|/v1/messages)`)
	harnessRE  = regexp.MustCompile(`(?i)\b(?:node|python3?|bun|deno)\s+\S*(?:simulat|probe|smoke|harness|persona|grader|eval)\S*\.(?:mjs|js|ts|py)\b`)
	safeSessionRE = regexp.MustCompile(`[^A-Za-z0-9_.-]+`)
)

// RingerHome mirrors ringer_nudge.py ringer_home(): $RINGER_HOME (expanded) if
// non-empty, else ~/.ringer. This is the hook's own env contract, distinct
// from the config's state_dir.
func RingerHome() string {
	if v := strings.TrimSpace(os.Getenv("RINGER_HOME")); v != "" {
		return config.ExpandUser(v)
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ".ringer"
	}
	return filepath.Join(home, ".ringer")
}

func stateDir(home string) string { return filepath.Join(home, "nudge-state") }

// hasLiveRuns reuses state.ReadActiveRuns (read + prune-dead-pids + write-back).
func hasLiveRuns(home string) (bool, error) {
	runs, err := state.ReadActiveRuns(home)
	if err != nil {
		return false, err
	}
	return len(runs) > 0, nil
}

// commandInvokesRinger is the Go analog of Python's `"ringer.py" in command`
// guard: skip nudging a command that itself invokes the ringer orchestrator.
// Tokenized so `python3 ringer_probe.py` (legitimately swarm-shaped) is NOT
// matched, but `ringer run x` / `/usr/bin/ringer demo` are.
func commandInvokesRinger(command string) bool {
	for _, tok := range strings.Fields(command) {
		if tok == "ringer" || tok == "ringer.py" ||
			strings.HasSuffix(tok, "/ringer") || strings.HasSuffix(tok, "/ringer.py") {
			return true
		}
	}
	return false
}

func shouldNudgePreBash(payload map[string]any, home string) (bool, error) {
	ti, ok := payload["tool_input"].(map[string]any)
	if !ok {
		return false, nil
	}
	command, ok := ti["command"].(string)
	if !ok || strings.TrimSpace(command) == "" {
		return false, nil
	}
	if commandInvokesRinger(command) {
		return false, nil
	}
	if !(providerRE.MatchString(command) || harnessRE.MatchString(command)) {
		return false, nil
	}
	// Python then checks command_references_active_workdir, but that branch is
	// unreachable — the active_runs truthiness check below already returns for
	// any non-empty map. Any live run suppresses the nudge.
	live, err := hasLiveRuns(home)
	if err != nil {
		return false, err
	}
	return !live, nil
}

type postEditState struct {
	Count     int      `json:"count"`
	FilePaths []string `json:"file_paths"`
}

// safeSessionID mirrors ringer_nudge.py safe_session_id: session_id is a string
// in the hook JSON; empty/absent → "unknown-session"; then non-[A-Za-z0-9_.-]
// runs collapse to "_" and leading/trailing "._" are trimmed.
func safeSessionID(v any) string {
	s, _ := v.(string)
	if s == "" {
		s = "unknown-session"
	}
	s = safeSessionRE.ReplaceAllString(s, "_")
	s = strings.Trim(s, "._")
	if s == "" {
		return "unknown-session"
	}
	return s
}

func postEditStatePath(home string, sessionID any) string {
	return filepath.Join(stateDir(home), safeSessionID(sessionID)+".json")
}

func loadPostEditState(path string) postEditState {
	st := postEditState{Count: 0, FilePaths: []string{}}
	data, err := os.ReadFile(path)
	if err != nil {
		return st
	}
	var raw postEditState
	if err := json.Unmarshal(data, &raw); err != nil {
		return st // corrupt self-written state → start fresh (Python is more granular; benign)
	}
	if raw.FilePaths == nil {
		raw.FilePaths = []string{}
	}
	return raw
}

func writeJSONAtomic(path string, v any) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	data, err := json.Marshal(v)
	if err != nil {
		return err
	}
	tmp := filepath.Join(filepath.Dir(path), fmt.Sprintf(".%s.%d.tmp", filepath.Base(path), os.Getpid()))
	if err := os.WriteFile(tmp, append(data, '\n'), 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// recordPostEdit increments the per-session edit counter and unions the edited
// file path, returning (count, distinctFiles). Increments on every call.
func recordPostEdit(payload map[string]any, home string) (int, int, error) {
	path := postEditStatePath(home, payload["session_id"])
	st := loadPostEditState(path)
	count := st.Count + 1
	files := make(map[string]struct{}, len(st.FilePaths)+1)
	for _, f := range st.FilePaths {
		files[f] = struct{}{}
	}
	if ti, ok := payload["tool_input"].(map[string]any); ok {
		if fp, ok := ti["file_path"].(string); ok && strings.TrimSpace(fp) != "" {
			files[fp] = struct{}{}
		}
	}
	sorted := make([]string, 0, len(files))
	for f := range files {
		sorted = append(sorted, f)
	}
	sort.Strings(sorted)
	if err := writeJSONAtomic(path, postEditState{Count: count, FilePaths: sorted}); err != nil {
		return count, len(sorted), err
	}
	return count, len(sorted), nil
}

func shouldNudgePostEdit(payload map[string]any, home string) (bool, error) {
	count, distinct, err := recordPostEdit(payload, home)
	if err != nil {
		return false, err
	}
	if count < 8 || distinct < 3 {
		return false, nil
	}
	live, err := hasLiveRuns(home)
	if err != nil {
		return false, err
	}
	return !live, nil
}

func markerPath(home string, sessionID any, event string) string {
	key := fmt.Sprintf("%v\x00%s", sessionID, event)
	sum := sha256.Sum256([]byte(key))
	return filepath.Join(stateDir(home), fmt.Sprintf("%x.%s.nudged", sum, event))
}

// claimDedupeMarker atomically claims the (session,event) marker. Returns
// (true,nil) on first claim, (false,nil) if already claimed, (false,err) on a
// real filesystem error.
func claimDedupeMarker(home string, sessionID any, event string) (bool, error) {
	dir := stateDir(home)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return false, err
	}
	f, err := os.OpenFile(markerPath(home, sessionID, event), os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o644)
	if err != nil {
		if errors.Is(err, os.ErrExist) {
			return false, nil
		}
		return false, err
	}
	defer f.Close()
	_, werr := f.WriteString(time.Now().UTC().Format(time.RFC3339Nano) + "\n")
	return true, werr
}

type hookOutput struct {
	HookSpecificOutput hookSpecificOutput `json:"hookSpecificOutput"`
}

type hookSpecificOutput struct {
	HookEventName     string `json:"hookEventName"`
	AdditionalContext string `json:"additionalContext"`
}

func emitNudge(w io.Writer, eventName string) error {
	data, err := json.Marshal(hookOutput{HookSpecificOutput: hookSpecificOutput{
		HookEventName:     eventName,
		AdditionalContext: NudgeText,
	}})
	if err != nil {
		return err
	}
	_, err = w.Write(append(data, '\n'))
	return err
}

// Run executes one nudge-hook invocation: read the hook payload from stdin,
// apply the gating + dedupe rules for mode, and on a trigger write the nudge
// JSON to stdout. Returns nil in the normal (no-nudge or nudged) case; a
// non-nil error is only a real filesystem failure. The CLI wrapper turns any
// outcome into exit 0. An unknown mode or malformed/empty stdin is a silent
// no-op (nil), matching ringer_nudge.py.
func Run(mode string, stdin io.Reader, stdout io.Writer, home string) error {
	if mode != "pre-bash" && mode != "post-edit" {
		return nil
	}
	data, err := io.ReadAll(stdin)
	if err != nil {
		return err
	}
	var payload map[string]any
	if err := json.Unmarshal(data, &payload); err != nil || payload == nil {
		return nil // malformed or non-object payload → no-op (Python parity)
	}
	sessionID := payload["session_id"]

	var trigger bool
	var event string
	if mode == "pre-bash" {
		trigger, err = shouldNudgePreBash(payload, home)
		event = "PreToolUse"
	} else {
		trigger, err = shouldNudgePostEdit(payload, home)
		event = "PostToolUse"
	}
	if err != nil || !trigger {
		return err
	}
	claimed, err := claimDedupeMarker(home, sessionID, mode)
	if err != nil || !claimed {
		return err
	}
	return emitNudge(stdout, event)
}
```

- [ ] **Step 2: Verify it builds**

Run: `./build.sh`
Expected: builds clean (templ generate → vet → build).

- [ ] **Step 3: Write `internal/nudge/nudge_test.go`**

Table-drive the predicates and drive `Run` end-to-end. Use `t.TempDir()` as `home`. A helper builds a payload map. Assert exit-0 semantics by asserting `Run` returns nil and stdout is empty/non-empty as expected.

```go
package nudge

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func payloadJSON(t *testing.T, m map[string]any) string {
	t.Helper()
	b, err := json.Marshal(m)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func TestProviderHarnessRegexes(t *testing.T) {
	hits := []string{
		"curl https://api.anthropic.com/v1/messages",
		"python3 run_probe.py --n 5",
		"node persona_sim.mjs",
		"bun smoke_grader.ts",
	}
	for _, c := range hits {
		if !(providerRE.MatchString(c) || harnessRE.MatchString(c)) {
			t.Errorf("expected match for %q", c)
		}
	}
	misses := []string{"ls -la", "git status", "go test ./...", "python3 app.py"}
	for _, c := range misses {
		if providerRE.MatchString(c) || harnessRE.MatchString(c) {
			t.Errorf("unexpected match for %q", c)
		}
	}
}

func TestCommandInvokesRinger(t *testing.T) {
	yes := []string{"ringer run m.toml", "/usr/bin/ringer demo", "./ringer nudge-hook pre-bash", "python3 ringer.py run"}
	for _, c := range yes {
		if !commandInvokesRinger(c) {
			t.Errorf("expected ringer-invocation for %q", c)
		}
	}
	no := []string{"python3 ringer_probe.py", "grep ringer notes.txt", "node harness.mjs"}
	for _, c := range no {
		if commandInvokesRinger(c) {
			t.Errorf("did not expect ringer-invocation for %q", c)
		}
	}
}

func TestSafeSessionID(t *testing.T) {
	cases := map[any]string{
		"abc-123":     "abc-123",
		"a/b c":       "a_b_c",
		"":            "unknown-session",
		nil:           "unknown-session",
		"__weird__":   "weird",
	}
	for in, want := range cases {
		if got := safeSessionID(in); got != want {
			t.Errorf("safeSessionID(%v) = %q, want %q", in, got, want)
		}
	}
}

func TestRunPreBashTriggersAndDedupes(t *testing.T) {
	home := t.TempDir()
	pl := payloadJSON(t, map[string]any{
		"session_id": "s1",
		"tool_input": map[string]any{"command": "curl https://api.anthropic.com/v1/messages"},
	})

	var out1 bytes.Buffer
	if err := Run("pre-bash", strings.NewReader(pl), &out1, home); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out1.String(), NudgeText) {
		t.Fatalf("expected nudge, got %q", out1.String())
	}
	if !strings.Contains(out1.String(), `"hookEventName":"PreToolUse"`) {
		t.Fatalf("expected PreToolUse event, got %q", out1.String())
	}

	// Second identical invocation is deduped by the marker.
	var out2 bytes.Buffer
	if err := Run("pre-bash", strings.NewReader(pl), &out2, home); err != nil {
		t.Fatal(err)
	}
	if out2.Len() != 0 {
		t.Fatalf("expected deduped (empty) output, got %q", out2.String())
	}
}

func TestRunPreBashSuppressedByLiveRun(t *testing.T) {
	home := t.TempDir()
	// A live run: this test's own PID is alive, so the entry survives pruning.
	writeActiveRunsForTest(t, home, os.Getpid())
	pl := payloadJSON(t, map[string]any{
		"session_id": "s2",
		"tool_input": map[string]any{"command": "curl https://api.openai.com/v1/chat/completions"},
	})
	var out bytes.Buffer
	if err := Run("pre-bash", strings.NewReader(pl), &out, home); err != nil {
		t.Fatal(err)
	}
	if out.Len() != 0 {
		t.Fatalf("expected suppression during a live run, got %q", out.String())
	}
}

func TestRunPostEditThreshold(t *testing.T) {
	home := t.TempDir()
	mk := func(i int) string {
		return payloadJSON(t, map[string]any{
			"session_id": "sedit",
			"tool_input": map[string]any{"file_path": filepathN(i)},
		})
	}
	// 7 edits across 3 files: below the count>=8 threshold → no nudge yet.
	for i := 0; i < 7; i++ {
		var out bytes.Buffer
		if err := Run("post-edit", strings.NewReader(mk(i%3)), &out, home); err != nil {
			t.Fatal(err)
		}
		if out.Len() != 0 {
			t.Fatalf("edit %d: unexpected early nudge %q", i, out.String())
		}
	}
	// 8th edit (still 3 distinct files) crosses count>=8 AND distinct>=3.
	var out bytes.Buffer
	if err := Run("post-edit", strings.NewReader(mk(0)), &out, home); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), `"hookEventName":"PostToolUse"`) {
		t.Fatalf("expected PostToolUse nudge at the 8th edit, got %q", out.String())
	}
}

func TestRunUnknownModeAndMalformed(t *testing.T) {
	home := t.TempDir()
	var out bytes.Buffer
	if err := Run("bogus", strings.NewReader(`{}`), &out, home); err != nil || out.Len() != 0 {
		t.Fatalf("unknown mode should no-op: err=%v out=%q", err, out.String())
	}
	out.Reset()
	if err := Run("pre-bash", strings.NewReader("not json"), &out, home); err != nil || out.Len() != 0 {
		t.Fatalf("malformed stdin should no-op: err=%v out=%q", err, out.String())
	}
	out.Reset()
	if err := Run("pre-bash", strings.NewReader(""), &out, home); err != nil || out.Len() != 0 {
		t.Fatalf("empty stdin should no-op: err=%v out=%q", err, out.String())
	}
}

// writeActiveRunsForTest writes an active-runs.json with one entry at pid.
func writeActiveRunsForTest(t *testing.T, home string, pid int) {
	t.Helper()
	entry := map[string]any{
		"r1": map[string]any{"pid": pid, "run_name": "x", "identity": "y", "workdir": "/tmp/x", "started_at": "t"},
	}
	b, err := json.Marshal(entry)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(home, "active-runs.json"), b, 0o644); err != nil {
		t.Fatal(err)
	}
}

func filepathN(i int) string {
	return filepath.Join("/work", "file"+string(rune('a'+i))+".go")
}
```

- [ ] **Step 4: Run the tests**

Run: `./build.sh --test`
Expected: PASS (nudge package + full suite green).

- [ ] **Step 5: Commit**

```bash
git add internal/nudge/
git commit -m "nudge: port ringer_nudge.py to internal/nudge (gating + dedupe + post-edit + Run)"
```

---

## Task 2: `cmd/ringer nudge-hook` subcommand

Wires `nudge.Run` to a go-flags command, resolves the ringer home, and guarantees exit 0.

**Files:**
- Create: `cmd/ringer/nudgehook.go`
- Test: `cmd/ringer/nudgehook_test.go`

**Interfaces:**
- Consumes: `nudge.Run`, `nudge.RingerHome`, `nudge.NudgeText` from Task 1.
- Produces: registered `nudge-hook` command; internal seam `runNudgeHook(mode string, stdin io.Reader, stdout io.Writer) error` (always nil).

- [ ] **Step 1: Write `cmd/ringer/nudgehook.go`**

```go
// cmd/ringer/nudgehook.go
package main

import (
	"io"
	"os"

	"github.com/corruptmemory/ringer/internal/nudge"
)

type nudgeHookCmd struct {
	Args struct {
		Mode string `positional-arg-name:"MODE" description:"pre-bash or post-edit"`
	} `positional-args:"yes"`
}

// runNudgeHook resolves the ringer home and runs the nudge engine, swallowing
// every error and panic: a Claude Code hook must ALWAYS exit 0 so it can never
// break the agent's tool call (frozen contract, spec §9.9).
func runNudgeHook(mode string, stdin io.Reader, stdout io.Writer) (err error) {
	defer func() { _ = recover(); err = nil }()
	_ = nudge.Run(mode, stdin, stdout, nudge.RingerHome())
	return nil
}

func (c *nudgeHookCmd) Execute(args []string) error {
	return runNudgeHook(c.Args.Mode, os.Stdin, os.Stdout)
}

func init() {
	parser.AddCommand("nudge-hook",
		"Claude Code routing nudge hook (pre-bash|post-edit)",
		"Read a Claude Code hook payload on stdin and, for swarm-shaped inline work, print a routing nudge. Always exits 0.",
		&nudgeHookCmd{})
}
```

- [ ] **Step 2: Write `cmd/ringer/nudgehook_test.go`**

Drive the seam directly (avoids coupling to `os.Stdin`). Set `RINGER_HOME` via `t.Setenv` so `RingerHome()` points at a temp dir.

```go
// cmd/ringer/nudgehook_test.go
package main

import (
	"bytes"
	"strings"
	"testing"

	"github.com/corruptmemory/ringer/internal/nudge"
)

func TestRunNudgeHookPreBashEmits(t *testing.T) {
	t.Setenv("RINGER_HOME", t.TempDir())
	in := strings.NewReader(`{"session_id":"cli1","tool_input":{"command":"curl https://api.anthropic.com/v1/messages"}}`)
	var out bytes.Buffer
	if err := runNudgeHook("pre-bash", in, &out); err != nil {
		t.Fatalf("nudge-hook must never error, got %v", err)
	}
	if !strings.Contains(out.String(), nudge.NudgeText) {
		t.Fatalf("expected nudge output, got %q", out.String())
	}
}

func TestRunNudgeHookBogusModeIsSilentExitZero(t *testing.T) {
	t.Setenv("RINGER_HOME", t.TempDir())
	var out bytes.Buffer
	if err := runNudgeHook("", strings.NewReader(`{}`), &out); err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}
	if out.Len() != 0 {
		t.Fatalf("expected no output, got %q", out.String())
	}
}
```

- [ ] **Step 3: Run the tests**

Run: `./build.sh --test`
Expected: PASS.

- [ ] **Step 4: Commit**

```bash
git add cmd/ringer/nudgehook.go cmd/ringer/nudgehook_test.go
git commit -m "cmd: nudge-hook subcommand wiring internal/nudge (always exit 0)"
```

---

## Task 3: `internal/agent` settings merge/remove (pure `map[string]any` logic)

The idempotent `settings.json` manipulation, over `map[string]any` so arbitrary user keys survive round-trips.

**Files:**
- Create: `internal/agent/settings.go`
- Test: `internal/agent/settings_test.go`

**Interfaces:**
- Produces (package-internal, consumed by Task 4): `hookMarker` const; `hookCommandContains(any) bool`; `eventHasRingerHook([]any) bool`; `mergeRingerHook(settings map[string]any, event, matcher, command string) (bool, error)`; `removeRingerHooks(settings map[string]any) int`; `loadSettings(path string) (map[string]any, error)`; `writeSettings(path string, settings map[string]any) error`; `backupFile(path string) (string, error)`.

- [ ] **Step 1: Write `internal/agent/settings.go`**

```go
package agent

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// hookMarker identifies a ringer-installed hook by a substring of its command.
// The Go install registers `<binary> nudge-hook <action>`, so "nudge-hook" is
// the stable, binary-path-independent marker (the analog of ringer_nudge.py's
// "ringer_nudge.py"). Legacy `python3 …/ringer_nudge.py` hooks are intentionally
// NOT matched — cleaning those up belongs to the 5d cutover / a README
// migration note, not this port.
const hookMarker = "nudge-hook"

func hookCommandContains(handler any) bool {
	m, ok := handler.(map[string]any)
	if !ok {
		return false
	}
	cmd, _ := m["command"].(string)
	return strings.Contains(cmd, hookMarker)
}

func eventHasRingerHook(groups []any) bool {
	for _, g := range groups {
		gm, ok := g.(map[string]any)
		if !ok {
			continue
		}
		handlers, ok := gm["hooks"].([]any)
		if !ok {
			continue
		}
		for _, h := range handlers {
			if hookCommandContains(h) {
				return true
			}
		}
	}
	return false
}

// mergeRingerHook adds a ringer hook group for (event, matcher, command) unless
// one is already present (idempotent). Returns true if it modified settings.
// Type mismatches on the hooks tree are loud errors (Python raised ValueError).
func mergeRingerHook(settings map[string]any, event, matcher, command string) (bool, error) {
	hooksAny, ok := settings["hooks"]
	if !ok {
		hooksAny = map[string]any{}
		settings["hooks"] = hooksAny
	}
	hooks, ok := hooksAny.(map[string]any)
	if !ok {
		return false, fmt.Errorf("settings hooks field must be a JSON object")
	}
	groups, _ := hooks[event].([]any) // absent or wrong-type → treat as empty, then re-validate
	if raw, present := hooks[event]; present {
		if _, ok := raw.([]any); !ok {
			return false, fmt.Errorf("settings hooks.%s field must be a JSON array", event)
		}
	}
	if eventHasRingerHook(groups) {
		hooks[event] = groups
		return false, nil
	}
	groups = append(groups, map[string]any{
		"matcher": matcher,
		"hooks": []any{
			map[string]any{"type": "command", "command": command},
		},
	})
	hooks[event] = groups
	return true, nil
}

// removeRingerHooks strips every handler whose command contains hookMarker,
// pruning emptied groups and emptied events. Returns the count removed.
func removeRingerHooks(settings map[string]any) int {
	hooksAny, ok := settings["hooks"]
	if !ok {
		return 0
	}
	hooks, ok := hooksAny.(map[string]any)
	if !ok {
		return 0
	}
	removed := 0
	events := make([]string, 0, len(hooks))
	for e := range hooks {
		events = append(events, e)
	}
	sort.Strings(events)
	for _, event := range events {
		groups, ok := hooks[event].([]any)
		if !ok {
			continue
		}
		kept := []any{}
		for _, g := range groups {
			gm, ok := g.(map[string]any)
			if !ok {
				kept = append(kept, g)
				continue
			}
			handlers, ok := gm["hooks"].([]any)
			if !ok {
				kept = append(kept, g)
				continue
			}
			keptHandlers := []any{}
			for _, h := range handlers {
				if hookCommandContains(h) {
					removed++
				} else {
					keptHandlers = append(keptHandlers, h)
				}
			}
			if len(keptHandlers) > 0 {
				ng := make(map[string]any, len(gm))
				for k, v := range gm {
					ng[k] = v
				}
				ng["hooks"] = keptHandlers
				kept = append(kept, ng)
			}
		}
		if len(kept) > 0 {
			hooks[event] = kept
		} else {
			delete(hooks, event)
		}
	}
	if len(hooks) == 0 {
		delete(settings, "hooks")
	}
	return removed
}

// loadSettings reads settings.json into a map, distinguishing "absent" (→ {})
// from "invalid JSON" and "not a JSON object" (both loud errors).
func loadSettings(path string) (map[string]any, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return map[string]any{}, nil
		}
		return nil, err
	}
	var v any
	if err := json.Unmarshal(data, &v); err != nil {
		return nil, fmt.Errorf("settings file is not valid JSON: %s", path)
	}
	m, ok := v.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("settings file must contain a JSON object: %s", path)
	}
	return m, nil
}

// backupFile copies an existing file to `<name>.bak-<UTCstamp>` and returns the
// backup path ("" if the source did not exist). Mirrors ringer.py backup_file.
func backupFile(path string) (string, error) {
	if _, err := os.Stat(path); err != nil {
		if os.IsNotExist(err) {
			return "", nil
		}
		return "", err
	}
	stamp := time.Now().UTC().Format("20060102T150405.000000") + "Z"
	backup := path + ".bak-" + stamp
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	if err := os.WriteFile(backup, data, 0o644); err != nil {
		return "", err
	}
	return backup, nil
}

// writeSettings backs up any existing file, then atomically writes the map as
// 2-space-indented, sorted-key JSON with a trailing newline and NO HTML
// escaping (matches Python json.dumps(indent=2, sort_keys=True)+"\n").
func writeSettings(path string, settings map[string]any) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	if _, err := backupFile(path); err != nil {
		return err
	}
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetIndent("", "  ")
	enc.SetEscapeHTML(false)
	if err := enc.Encode(settings); err != nil { // Encode appends the trailing "\n"
		return err
	}
	tmp := filepath.Join(filepath.Dir(path), fmt.Sprintf(".%s.%d.tmp", filepath.Base(path), os.Getpid()))
	if err := os.WriteFile(tmp, buf.Bytes(), 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}
```

- [ ] **Step 2: Write `internal/agent/settings_test.go`**

```go
package agent

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestMergePreservesUnknownKeysAndIsIdempotent(t *testing.T) {
	settings := map[string]any{
		"theme":  "dark",
		"custom": map[string]any{"a": float64(1)},
	}
	changed, err := mergeRingerHook(settings, "PreToolUse", "Bash", "/bin/ringer nudge-hook pre-bash")
	if err != nil {
		t.Fatal(err)
	}
	if !changed {
		t.Fatal("first merge should change settings")
	}
	if settings["theme"] != "dark" {
		t.Fatal("unknown key 'theme' was dropped")
	}
	// Idempotent: a second merge for the same event is a no-op.
	changed2, err := mergeRingerHook(settings, "PreToolUse", "Bash", "/bin/ringer nudge-hook pre-bash")
	if err != nil {
		t.Fatal(err)
	}
	if changed2 {
		t.Fatal("second merge should be a no-op")
	}
	if !eventHasRingerHook(settings["hooks"].(map[string]any)["PreToolUse"].([]any)) {
		t.Fatal("expected the ringer hook present")
	}
}

func TestMergeTypeErrors(t *testing.T) {
	settings := map[string]any{"hooks": "not-an-object"}
	if _, err := mergeRingerHook(settings, "PreToolUse", "Bash", "x nudge-hook pre-bash"); err == nil {
		t.Fatal("expected error when hooks is not an object")
	}
	settings2 := map[string]any{"hooks": map[string]any{"PreToolUse": "not-an-array"}}
	if _, err := mergeRingerHook(settings2, "PreToolUse", "Bash", "x nudge-hook pre-bash"); err == nil {
		t.Fatal("expected error when hooks.PreToolUse is not an array")
	}
}

func TestRemoveRingerHooksCountsAndPreserves(t *testing.T) {
	settings := map[string]any{}
	mergeRingerHook(settings, "PreToolUse", "Bash", "/bin/ringer nudge-hook pre-bash")
	// A user's own unrelated hook on the same event must survive.
	hooks := settings["hooks"].(map[string]any)
	hooks["PreToolUse"] = append(hooks["PreToolUse"].([]any), map[string]any{
		"matcher": "Bash",
		"hooks":   []any{map[string]any{"type": "command", "command": "echo mine"}},
	})
	removed := removeRingerHooks(settings)
	if removed != 1 {
		t.Fatalf("expected 1 removed, got %d", removed)
	}
	remaining := settings["hooks"].(map[string]any)["PreToolUse"].([]any)
	if len(remaining) != 1 {
		t.Fatalf("expected the user's own hook to survive, got %d groups", len(remaining))
	}
}

func TestRemoveEmptiesHooksEntirely(t *testing.T) {
	settings := map[string]any{}
	mergeRingerHook(settings, "PreToolUse", "Bash", "x nudge-hook pre-bash")
	mergeRingerHook(settings, "PostToolUse", "Edit|Write", "x nudge-hook post-edit")
	if removeRingerHooks(settings) != 2 {
		t.Fatal("expected 2 removed")
	}
	if _, ok := settings["hooks"]; ok {
		t.Fatal("expected the whole 'hooks' key removed when emptied")
	}
}

func TestWriteSettingsBackupSortedNoHTMLEscape(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "settings.json")
	if err := os.WriteFile(path, []byte("{\n  \"old\": true\n}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	settings := map[string]any{"b": "x>y&z", "a": float64(1)}
	if err := writeSettings(path, settings); err != nil {
		t.Fatal(err)
	}
	// A backup of the prior file exists.
	entries, _ := os.ReadDir(dir)
	foundBackup := false
	for _, e := range entries {
		if len(e.Name()) > len("settings.json.bak-") && e.Name()[:len("settings.json.bak-")] == "settings.json.bak-" {
			foundBackup = true
		}
	}
	if !foundBackup {
		t.Fatal("expected a settings.json.bak-* backup")
	}
	// Written file: sorted keys, no HTML escaping of > & <.
	data, _ := os.ReadFile(path)
	got := string(data)
	if !strings.Contains(got, `"a": 1`) || !strings.Contains(got, `"b": "x>y&z"`) {
		t.Fatalf("expected unescaped, sorted output, got:\n%s", got)
	}
	// Round-trips as valid JSON.
	var back map[string]any
	if err := json.Unmarshal(data, &back); err != nil {
		t.Fatalf("output is not valid JSON: %v", err)
	}
}
```

- [ ] **Step 3: Run the tests**

Run: `./build.sh --test`
Expected: PASS.

- [ ] **Step 4: Commit**

```bash
git add internal/agent/settings.go internal/agent/settings_test.go
git commit -m "agent: settings.json hook merge/remove over map[string]any (preserves unknown keys)"
```

---

## Task 4: `internal/agent` — embed SKILL.md + Install/Uninstall

Adds the embedded skill and the two orchestration entry points, drift-locked to the canonical skill file.

**Files:**
- Create: `internal/agent/agent.go`
- Create: `internal/agent/skill.go`
- Create: `internal/agent/SKILL.md` (byte copy of `.claude/skills/ringer/SKILL.md`)
- Test: `internal/agent/agent_test.go`

**Interfaces:**
- Consumes: Task 3 internals + `SkillMarkdown []byte`.
- Produces (consumed by Task 5): `agent.HookCommand(binPath, action string) string`; `agent.Install(root, binPath string) (InstallResult, error)`; `agent.Uninstall(root string) (UninstallResult, error)`; `InstallResult{SkillTarget, SettingsPath string; HooksChanged bool}`; `UninstallResult{HooksRemoved int; SkillRemoved bool}`.

- [ ] **Step 1: Copy the canonical skill into the package (embeddable location)**

Run:
```bash
cp .claude/skills/ringer/SKILL.md internal/agent/SKILL.md
```
Expected: `internal/agent/SKILL.md` now byte-identical to the canonical file. (This is the regeneration command the drift-lock test references.)

- [ ] **Step 2: Write `internal/agent/skill.go`**

```go
package agent

import _ "embed"

// SkillMarkdown is the ringer Claude Code skill, embedded so the static binary
// can install it without the source tree. It is a committed copy of
// .claude/skills/ringer/SKILL.md — //go:embed cannot reach that path because it
// lives under a dot-directory. TestEmbeddedSkillMatchesCanonical drift-locks
// the two. To regenerate after editing the canonical file:
//
//	cp .claude/skills/ringer/SKILL.md internal/agent/SKILL.md
//
//go:embed SKILL.md
var SkillMarkdown []byte
```

- [ ] **Step 3: Write `internal/agent/agent.go`**

```go
package agent

import (
	"fmt"
	"os"
	"path/filepath"
)

// Frozen hook registration values — match ringer_nudge.py install_agent.
const (
	preBashEvent    = "PreToolUse"
	preBashMatcher  = "Bash"
	postEditEvent   = "PostToolUse"
	postEditMatcher = "Edit|Write"
)

// HookCommand builds the settings.json hook command: the ringer binary path
// plus `nudge-hook <action>`. (Python registered `python3 …/ringer_nudge.py
// <action>`; spec §3 switches this to the binary.)
func HookCommand(binPath, action string) string {
	return fmt.Sprintf("%s nudge-hook %s", binPath, action)
}

type InstallResult struct {
	SkillTarget  string
	SettingsPath string
	HooksChanged bool
}

// Install copies the embedded skill into <root>/skills/ringer/SKILL.md and
// idempotently merges the two ringer hooks into <root>/settings.json (backing
// up any existing file). root is the target .claude directory; binPath is the
// ringer binary path baked into the hook commands.
func Install(root, binPath string) (InstallResult, error) {
	skillTarget := filepath.Join(root, "skills", "ringer", "SKILL.md")
	if err := os.MkdirAll(filepath.Dir(skillTarget), 0o755); err != nil {
		return InstallResult{}, err
	}
	if err := os.WriteFile(skillTarget, SkillMarkdown, 0o644); err != nil {
		return InstallResult{}, err
	}

	settingsPath := filepath.Join(root, "settings.json")
	settings, err := loadSettings(settingsPath)
	if err != nil {
		return InstallResult{}, err
	}
	c1, err := mergeRingerHook(settings, preBashEvent, preBashMatcher, HookCommand(binPath, "pre-bash"))
	if err != nil {
		return InstallResult{}, err
	}
	c2, err := mergeRingerHook(settings, postEditEvent, postEditMatcher, HookCommand(binPath, "post-edit"))
	if err != nil {
		return InstallResult{}, err
	}
	changed := c1 || c2
	_, statErr := os.Stat(settingsPath)
	if changed || os.IsNotExist(statErr) {
		if err := writeSettings(settingsPath, settings); err != nil {
			return InstallResult{}, err
		}
	}
	return InstallResult{SkillTarget: skillTarget, SettingsPath: settingsPath, HooksChanged: changed}, nil
}

type UninstallResult struct {
	HooksRemoved int
	SkillRemoved bool
}

// Uninstall removes the ringer hooks from <root>/settings.json (writing back
// only if something was removed) and deletes <root>/skills/ringer.
func Uninstall(root string) (UninstallResult, error) {
	settingsPath := filepath.Join(root, "settings.json")
	removed := 0
	if _, err := os.Stat(settingsPath); err == nil {
		settings, err := loadSettings(settingsPath)
		if err != nil {
			return UninstallResult{}, err
		}
		removed = removeRingerHooks(settings)
		if removed > 0 {
			if err := writeSettings(settingsPath, settings); err != nil {
				return UninstallResult{}, err
			}
		}
	}
	skillDir := filepath.Join(root, "skills", "ringer")
	removedSkill := false
	if _, err := os.Stat(skillDir); err == nil {
		if err := os.RemoveAll(skillDir); err != nil {
			return UninstallResult{}, err
		}
		removedSkill = true
	}
	return UninstallResult{HooksRemoved: removed, SkillRemoved: removedSkill}, nil
}
```

- [ ] **Step 4: Write `internal/agent/agent_test.go`**

```go
package agent

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestEmbeddedSkillMatchesCanonical(t *testing.T) {
	canonical, err := os.ReadFile(filepath.Join("..", "..", ".claude", "skills", "ringer", "SKILL.md"))
	if err != nil {
		t.Fatalf("read canonical SKILL.md: %v", err)
	}
	if !bytes.Equal(canonical, SkillMarkdown) {
		t.Fatalf("internal/agent/SKILL.md is stale; regenerate:\n  cp .claude/skills/ringer/SKILL.md internal/agent/SKILL.md")
	}
}

func TestInstallThenUninstallRoundTrip(t *testing.T) {
	root := t.TempDir()
	res, err := Install(root, "/opt/ringer")
	if err != nil {
		t.Fatal(err)
	}
	if !res.HooksChanged {
		t.Fatal("expected hooks changed on first install")
	}
	// Skill copied.
	skill, err := os.ReadFile(res.SkillTarget)
	if err != nil || !bytes.Equal(skill, SkillMarkdown) {
		t.Fatalf("skill not installed correctly: err=%v", err)
	}
	// Both hooks registered with the binary path.
	data, _ := os.ReadFile(res.SettingsPath)
	s := string(data)
	if !strings.Contains(s, "/opt/ringer nudge-hook pre-bash") || !strings.Contains(s, "/opt/ringer nudge-hook post-edit") {
		t.Fatalf("expected binary-path hooks, got:\n%s", s)
	}
	if !strings.Contains(s, `"Edit|Write"`) || !strings.Contains(s, `"Bash"`) {
		t.Fatalf("expected frozen matchers, got:\n%s", s)
	}

	// Idempotent second install.
	res2, err := Install(root, "/opt/ringer")
	if err != nil {
		t.Fatal(err)
	}
	if res2.HooksChanged {
		t.Fatal("expected idempotent (unchanged) second install")
	}

	// Uninstall removes both hooks and the skill dir.
	ures, err := Uninstall(root)
	if err != nil {
		t.Fatal(err)
	}
	if ures.HooksRemoved != 2 || !ures.SkillRemoved {
		t.Fatalf("expected 2 hooks + skill removed, got %+v", ures)
	}
	if _, err := os.Stat(filepath.Join(root, "skills", "ringer")); !os.IsNotExist(err) {
		t.Fatal("expected skill dir gone")
	}
	// settings.json remains valid JSON with no ringer hooks.
	data2, _ := os.ReadFile(res.SettingsPath)
	var m map[string]any
	if err := json.Unmarshal(data2, &m); err != nil {
		t.Fatalf("settings.json invalid after uninstall: %v", err)
	}
}

func TestInstallPreservesExistingSettings(t *testing.T) {
	root := t.TempDir()
	settingsPath := filepath.Join(root, "settings.json")
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(settingsPath, []byte(`{"theme":"dark","env":{"X":"1"}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Install(root, "/opt/ringer"); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(settingsPath)
	var m map[string]any
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatal(err)
	}
	if m["theme"] != "dark" {
		t.Fatal("existing 'theme' key was dropped by install")
	}
}
```

- [ ] **Step 5: Run the tests**

Run: `./build.sh --test`
Expected: PASS (including the drift-lock test).

- [ ] **Step 6: Commit**

```bash
git add internal/agent/agent.go internal/agent/skill.go internal/agent/SKILL.md internal/agent/agent_test.go
git commit -m "agent: embed SKILL.md (drift-locked) + Install/Uninstall orchestration"
```

---

## Task 5: `cmd/ringer install-agent` / `uninstall-agent` subcommands

Wires the installer to go-flags, resolving the `.claude` root (user/project) and the ringer binary path.

**Files:**
- Create: `cmd/ringer/agent.go`
- Test: `cmd/ringer/agent_test.go`

**Interfaces:**
- Consumes: `agent.Install`, `agent.Uninstall` from Task 4.
- Produces: registered `install-agent` / `uninstall-agent` commands.

- [ ] **Step 1: Write `cmd/ringer/agent.go`**

```go
// cmd/ringer/agent.go
package main

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/corruptmemory/ringer/internal/agent"
)

type installAgentCmd struct {
	Project bool `long:"project" description:"install into ./.claude instead of ~/.claude"`
}

type uninstallAgentCmd struct {
	Project bool `long:"project" description:"remove from ./.claude instead of ~/.claude"`
}

// claudeRoot resolves the target .claude directory: ./.claude for --project,
// else ~/.claude. Mirrors ringer.py claude_root.
func claudeRoot(project bool) (string, error) {
	var base string
	if project {
		wd, err := os.Getwd()
		if err != nil {
			return "", err
		}
		base = wd
	} else {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		base = home
	}
	return filepath.Join(base, ".claude"), nil
}

// ringerBinPath is the absolute path of the running ringer binary, baked into
// the hook commands. Falls back to the bare name if the OS can't report it.
func ringerBinPath() string {
	exe, err := os.Executable()
	if err != nil || exe == "" {
		return "ringer"
	}
	return exe
}

func (c *installAgentCmd) Execute(args []string) error {
	if len(args) > 0 {
		return fmt.Errorf("install-agent: unexpected argument %q", args[0])
	}
	root, err := claudeRoot(c.Project)
	if err != nil {
		return err
	}
	res, err := agent.Install(root, ringerBinPath())
	if err != nil {
		return err
	}
	scope := "user"
	if c.Project {
		scope = "project"
	}
	fmt.Printf("Installed ringer agent for %s scope.\n", scope)
	fmt.Printf("Skill: %s\n", res.SkillTarget)
	if res.HooksChanged {
		fmt.Printf("Hooks: added PreToolUse Bash and PostToolUse Edit|Write in %s\n", res.SettingsPath)
	} else {
		fmt.Printf("Hooks: already present in %s\n", res.SettingsPath)
	}
	return nil
}

func (c *uninstallAgentCmd) Execute(args []string) error {
	if len(args) > 0 {
		return fmt.Errorf("uninstall-agent: unexpected argument %q", args[0])
	}
	root, err := claudeRoot(c.Project)
	if err != nil {
		return err
	}
	res, err := agent.Uninstall(root)
	if err != nil {
		return err
	}
	scope := "user"
	if c.Project {
		scope = "project"
	}
	fmt.Printf("Uninstalled ringer agent for %s scope.\n", scope)
	fmt.Printf("Hooks removed: %d\n", res.HooksRemoved)
	skillMsg := "no"
	if res.SkillRemoved {
		skillMsg = "yes"
	}
	fmt.Printf("Skill removed: %s\n", skillMsg)
	return nil
}

func init() {
	parser.AddCommand("install-agent",
		"Install the ringer Claude Code skill and hooks",
		"Copy the ringer skill and register the routing-nudge hooks in settings.json (idempotent; backs up settings).",
		&installAgentCmd{})
	parser.AddCommand("uninstall-agent",
		"Remove the ringer Claude Code skill and hooks",
		"Remove the ringer routing-nudge hooks from settings.json and delete the installed skill.",
		&uninstallAgentCmd{})
}
```

- [ ] **Step 2: Write `cmd/ringer/agent_test.go`**

Test the user-scope path by overriding `$HOME` to a temp dir (`os.UserHomeDir` honors `$HOME` on Linux). Drive the command's `Execute` directly with a fresh struct so the test doesn't depend on the global parser's argv handling.

```go
// cmd/ringer/agent_test.go
package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestInstallUninstallAgentUserScope(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	inst := &installAgentCmd{}
	if err := inst.Execute(nil); err != nil {
		t.Fatalf("install-agent: %v", err)
	}
	skill := filepath.Join(home, ".claude", "skills", "ringer", "SKILL.md")
	if _, err := os.Stat(skill); err != nil {
		t.Fatalf("expected skill installed at %s: %v", skill, err)
	}
	settings := filepath.Join(home, ".claude", "settings.json")
	if _, err := os.Stat(settings); err != nil {
		t.Fatalf("expected settings.json at %s: %v", settings, err)
	}

	uninst := &uninstallAgentCmd{}
	if err := uninst.Execute(nil); err != nil {
		t.Fatalf("uninstall-agent: %v", err)
	}
	if _, err := os.Stat(filepath.Join(home, ".claude", "skills", "ringer")); !os.IsNotExist(err) {
		t.Fatal("expected skill dir removed after uninstall")
	}
}

func TestInstallAgentRejectsStrayPositional(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	if err := (&installAgentCmd{}).Execute([]string{"oops"}); err == nil {
		t.Fatal("expected error for stray positional argument")
	}
}
```

- [ ] **Step 3: Run the tests**

Run: `./build.sh --test`
Expected: PASS.

- [ ] **Step 4: Verify the new subcommands are wired**

Run: `./build.sh && ./ringer --help`
Expected: help lists `nudge-hook`, `install-agent`, `uninstall-agent`.

- [ ] **Step 5: Commit**

```bash
git add cmd/ringer/agent.go cmd/ringer/agent_test.go
git commit -m "cmd: install-agent/uninstall-agent subcommands (user/project scope)"
```

---

## Final verification (whole-plan)

- [ ] Run the full suite with the race detector: `./build.sh --test --race` — expect green across all packages.
- [ ] Smoke the installer against a throwaway root:
  ```bash
  ./build.sh
  TMP=$(mktemp -d)
  HOME="$TMP" ./ringer install-agent
  cat "$TMP/.claude/settings.json"          # two ringer hooks, binary-path commands, sorted keys
  HOME="$TMP" ./ringer install-agent        # "Hooks: already present" (idempotent)
  HOME="$TMP" ./ringer uninstall-agent      # Hooks removed: 2 / Skill removed: yes
  rm -rf "$TMP"
  ```
- [ ] Smoke the nudge hook:
  ```bash
  echo '{"session_id":"x","tool_input":{"command":"curl https://api.anthropic.com/v1/messages"}}' \
    | RINGER_HOME=$(mktemp -d) ./ringer nudge-hook pre-bash
  # → prints a hookSpecificOutput JSON with the nudge text; exit 0
  echo '{}' | ./ringer nudge-hook bogus-mode ; echo "exit=$?"   # → no output, exit=0
  ```

## Self-Review (author checklist)

- **Spec coverage:** §3 rows `nudge-hook` + `install-agent`/`uninstall-agent` → Tasks 1–5. §4 `internal/agent` → Tasks 3–5 (plus `internal/nudge`, a documented addition). §9.9 nudge stdin/stdout + exit-0 → Task 1/2. All covered.
- **Frozen values:** NUDGE_TEXT, both regexes, event names, matchers, thresholds, marker format, home resolution — all copied verbatim into Global Constraints and the code.
- **Type consistency:** `Run`, `RingerHome`, `NudgeText` (Task 1 → 2); `Install`/`Uninstall`/`HookCommand`/result structs (Task 4 → 5); `merge/remove/load/write` internals (Task 3 → 4). Names match across tasks.
- **No placeholders:** every code step is complete and compilable.
