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
	providerRE    = regexp.MustCompile(`(?i)(api\.anthropic\.com|api\.openai\.com|openrouter\.ai|generativelanguage\.googleapis|/v1/chat/completions|/v1/messages)`)
	harnessRE     = regexp.MustCompile(`(?i)\b(?:node|python3?|bun|deno)\s+\S*(?:simulat|probe|smoke|harness|persona|grader|eval)\S*\.(?:mjs|js|ts|py)\b`)
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
	tokens := strings.Fields(command)
	if len(tokens) == 0 {
		return false
	}

	// Check first token (the command being invoked)
	first := tokens[0]
	if first == "ringer" || first == "ringer.py" ||
		strings.HasSuffix(first, "/ringer") || strings.HasSuffix(first, "/ringer.py") {
		return true
	}

	// For interpreter + script patterns (e.g., "python3 ringer.py"), check second token
	// but only if it's exactly ringer.py or ends with /ringer.py, to avoid false matches like "grep ringer notes.txt"
	if len(tokens) > 1 {
		second := tokens[1]
		if second == "ringer.py" || strings.HasSuffix(second, "/ringer.py") {
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
