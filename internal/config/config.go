package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/BurntSushi/toml"
)

type EngineConfig struct {
	Bin            string   `toml:"bin"`
	ArgsTemplate   []string `toml:"args_template"`
	SandboxArgs    []string `toml:"sandbox_args"`
	FullAccessArgs []string `toml:"full_access_args"`
	TokenRegex     string   `toml:"token_regex"`
	ModelDefault   string   `toml:"model_default"`
	Isolation      string   `toml:"isolation"` // "", "none", "jail"
	JailStateDirs  []string `toml:"jail_state_dirs"`
}

type ArtifactConfig struct {
	Enabled   bool   `toml:"enabled"`
	Out       string `toml:"out"`
	ReportOut string `toml:"report_out"`
	IndexOut  string `toml:"index_out"`
}

type EvalConfig struct {
	DBPath string `toml:"db_path"` // empty -> <state_dir>/ringer.db
}

type AppConfig struct {
	IdentityDefault string                  `toml:"identity_default"`
	StateDir        string                  `toml:"state_dir"` // empty -> ~/.ringer
	AllowFullAccess bool                    `toml:"allow_full_access"`
	Eval            EvalConfig              `toml:"eval"`
	Artifact        ArtifactConfig          `toml:"artifact"`
	Engines         map[string]EngineConfig `toml:"engines"`
}

func DefaultPath() string {
	if p := os.Getenv("RINGER_CONFIG"); p != "" {
		return p
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "config.toml"
	}
	return filepath.Join(home, ".config", "ringer", "config.toml")
}

func expandHome(p string) string {
	if p == "~" || strings.HasPrefix(p, "~/") {
		if home, err := os.UserHomeDir(); err == nil {
			return filepath.Join(home, strings.TrimPrefix(strings.TrimPrefix(p, "~"), "/"))
		}
	}
	return p
}

func (c *AppConfig) StateDirPath() string {
	if c.StateDir != "" {
		return expandHome(c.StateDir)
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ".ringer"
	}
	return filepath.Join(home, ".ringer")
}

func (c *AppConfig) DBPath() string {
	if c.Eval.DBPath != "" {
		return expandHome(c.Eval.DBPath)
	}
	return filepath.Join(c.StateDirPath(), "ringer.db")
}

func Load(path string) (*AppConfig, error) {
	var c AppConfig
	md, err := toml.DecodeFile(path, &c)
	if err != nil {
		if os.IsNotExist(err) {
			return &AppConfig{}, nil // sane defaults without a config file
		}
		return nil, fmt.Errorf("config %s: %w", path, err)
	}
	for _, k := range md.Undecoded() {
		key := k.String()
		for removed, hint := range removedKeys {
			if key == removed || strings.HasPrefix(key, removed+".") {
				return nil, fmt.Errorf("config %s: key %q was removed in the Go rewrite — %s", path, key, hint)
			}
		}
		return nil, fmt.Errorf("config %s: unknown key %q (typo? removed?)", path, key)
	}
	for name, e := range c.Engines {
		switch e.Isolation {
		case "", "none", "jail":
		default:
			return nil, fmt.Errorf("config %s: engines.%s.isolation must be \"none\" or \"jail\", got %q", path, name, e.Isolation)
		}
	}
	return &c, nil
}
