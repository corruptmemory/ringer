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
