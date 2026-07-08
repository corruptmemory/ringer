package config

import (
	"os"
	"path/filepath"
	"testing"
)

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
	}{
		{"flag wins over everything", "cli-id", "env-f", "env-r", deep, cfg, "cli-id"},
		{"FLEET_IDENTITY beats RINGER_IDENTITY", "", "env-f", "env-r", deep, cfg, "env-f"},
		{"RINGER_IDENTITY beats file", "", "", "env-r", deep, cfg, "env-r"},
		{"fleet-agent file found walking up", "", "", "", deep, cfg, "repo-bot"},
		{"config default when no file", "", "", "", t.TempDir(), cfg, "cfg-default"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("FLEET_IDENTITY", tc.fleetEnv)
			t.Setenv("RINGER_IDENTITY", tc.ringerEnv)
			if got := ResolveIdentity(tc.flag, tc.cfg, tc.startDir); got != tc.want {
				t.Errorf("got %q, want %q", got, tc.want)
			}
		})
	}
}

func TestResolveIdentityHostnameFallback(t *testing.T) {
	t.Setenv("FLEET_IDENTITY", "")
	t.Setenv("RINGER_IDENTITY", "")
	got := ResolveIdentity("", &AppConfig{}, t.TempDir())
	if got == "" {
		t.Fatal("hostname fallback must never return empty")
	}
}
