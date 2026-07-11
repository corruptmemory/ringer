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
