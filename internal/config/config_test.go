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
