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

func TestLoadAcceptsLoggingSection(t *testing.T) {
	p := writeConfig(t, "[logging]\nlevel = \"debug\"\nformat = \"json\"\n")
	c, err := Load(p)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if c.Logging.Format != "json" {
		t.Errorf("logging.format = %q, want \"json\"", c.Logging.Format)
	}
}

func TestInvalidLoggingFormatRejected(t *testing.T) {
	_, err := Load(writeConfig(t, "[logging]\nformat = \"xml\"\n"))
	if err == nil || !strings.Contains(err.Error(), "logging.format") {
		t.Fatalf("want logging.format validation error, got %v", err)
	}
}

func TestEngineJailKeysDecode(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")
	body := `
[engines.opencode]
bin = "opencode"
args_template = ["run", "{spec}"]
isolation = "jail"
jail_state_dirs = ["~/.config/opencode", "~/.local/share/opencode"]
jail_ro_binds = ["~/.opencode"]
`
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	c, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	e := c.Engines["opencode"]
	if e.Isolation != "jail" {
		t.Fatalf("Isolation = %q, want jail", e.Isolation)
	}
	if len(e.JailStateDirs) != 2 || len(e.JailRoBinds) != 1 || e.JailRoBinds[0] != "~/.opencode" {
		t.Fatalf("jail dirs = %v / %v", e.JailStateDirs, e.JailRoBinds)
	}
}

func TestArtifactEnabledDefaultsTrue(t *testing.T) {
	c, err := Load(filepath.Join(t.TempDir(), "nope.toml"))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if !c.ArtifactEnabled() {
		t.Errorf("ArtifactEnabled() = false, want true for an absent [artifact] section")
	}
}

func TestArtifactEnabledExplicitFalse(t *testing.T) {
	p := writeConfig(t, "[artifact]\nenabled = false\n")
	c, err := Load(p)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if c.ArtifactEnabled() {
		t.Errorf("ArtifactEnabled() = true, want false for explicit enabled = false")
	}
}

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

func TestExpandUser(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil {
		t.Skipf("no home dir: %v", err)
	}
	cases := []struct{ in, want string }{
		{"~/x", filepath.Join(home, "x")},
		{"~", home},
		{"/abs/path", "/abs/path"},
		{"rel/path", "rel/path"},
	}
	for _, c := range cases {
		if got := ExpandUser(c.in); got != c.want {
			t.Errorf("ExpandUser(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}
