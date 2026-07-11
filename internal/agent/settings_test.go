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
