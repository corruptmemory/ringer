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
