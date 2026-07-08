package config

import (
	"os"
	"path/filepath"
	"testing"
)

// requireNoAncestorFleetAgent skips the test if a stray .fleet-agent exists
// in any real ancestor of dir — ResolveIdentity's walk would find it and
// shadow the fallback under test. Ambient machine state, not a code bug.
func requireNoAncestorFleetAgent(t *testing.T, dir string) {
	t.Helper()
	for d := dir; ; d = filepath.Dir(d) {
		if _, err := os.Stat(filepath.Join(d, ".fleet-agent")); err == nil {
			t.Skipf("stray .fleet-agent at %s shadows the fallback under test", d)
		}
		if d == filepath.Dir(d) {
			return
		}
	}
}

func TestResolveIdentityOrder(t *testing.T) {
	// Directory tree: root/.fleet-agent contains "repo-bot"; work in root/sub/deep.
	root := t.TempDir()
	deep := filepath.Join(root, "sub", "deep")
	if err := os.MkdirAll(deep, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, ".fleet-agent"), []byte("repo-bot\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg := &AppConfig{IdentityDefault: "cfg-default"}

	cases := []struct {
		name, flag, fleetEnv, ringerEnv, startDir string
		cfg                                       *AppConfig
		want                                      string
		guardAncestors                            bool
	}{
		{"flag wins over everything", "cli-id", "env-f", "env-r", deep, cfg, "cli-id", false},
		{"FLEET_IDENTITY beats RINGER_IDENTITY", "", "env-f", "env-r", deep, cfg, "env-f", false},
		{"RINGER_IDENTITY beats file", "", "", "env-r", deep, cfg, "env-r", false},
		{"fleet-agent file found walking up", "", "", "", deep, cfg, "repo-bot", false},
		{"config default when no file", "", "", "", t.TempDir(), cfg, "cfg-default", true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if tc.guardAncestors {
				requireNoAncestorFleetAgent(t, tc.startDir)
			}
			t.Setenv("FLEET_IDENTITY", tc.fleetEnv)
			t.Setenv("RINGER_IDENTITY", tc.ringerEnv)
			if got := ResolveIdentity(tc.flag, tc.cfg, tc.startDir); got != tc.want {
				t.Errorf("got %q, want %q", got, tc.want)
			}
		})
	}
}

func TestResolveIdentityHostnameFallback(t *testing.T) {
	dir := t.TempDir()
	requireNoAncestorFleetAgent(t, dir)
	t.Setenv("FLEET_IDENTITY", "")
	t.Setenv("RINGER_IDENTITY", "")
	got := ResolveIdentity("", &AppConfig{}, dir)
	if got == "" {
		t.Fatal("hostname fallback must never return empty")
	}
}
