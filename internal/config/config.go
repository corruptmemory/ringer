package config

import (
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"

	"github.com/BurntSushi/toml"
)

// LoggingConfig mirrors logging.Config field-for-field (Level slog.Level,
// Format string) instead of importing internal/logging, keeping config
// dependency-light; the CLI boundary does the trivial conversion. slog.Level
// implements encoding.TextUnmarshaler, so `level = "debug"` decodes for free.
// Zero value == {Info, ""} == the sane default (absent section => Info/text).
type LoggingConfig struct {
	Level  slog.Level `toml:"level"`
	Format string     `toml:"format"`
}

type EngineConfig struct {
	Bin            string   `toml:"bin"`
	ArgsTemplate   []string `toml:"args_template"`
	SandboxArgs    []string `toml:"sandbox_args"`
	FullAccessArgs []string `toml:"full_access_args"`
	TokenRegex     string   `toml:"token_regex"`
	ModelDefault   string   `toml:"model_default"`
	Isolation      string   `toml:"isolation"`       // "", "none", "jail"
	JailStateDirs  []string `toml:"jail_state_dirs"` // rw binds/rules inside the sandbox (engine state)
	JailRoBinds    []string `toml:"jail_ro_binds"`   // ro binds/rules (engine installs outside the host toolchain, e.g. ~/.opencode)
}

type ArtifactConfig struct {
	Enabled   *bool  `toml:"enabled"` // nil (absent) -> true, Python parity; see AppConfig.ArtifactEnabled
	Out       string `toml:"out"`
	ReportOut string `toml:"report_out"`
	IndexOut  string `toml:"index_out"`
}

type EvalConfig struct {
	DBPath string `toml:"db_path"` // empty -> <state_dir>/ringer.db
}

type ScoreboardConfig struct {
	ModelIdentityPath string `toml:"model_identity_path"` // empty -> embedded registry/model-identity.toml
	ModelNotesPath    string `toml:"model_notes_path"`    // empty -> embedded docs/MODEL-NOTES.md
}

// CatalogConfig overrides where the OpenRouter model catalog is fetched
// from. Source empty -> catalog.DefaultSource (see AppConfig.CatalogSource).
type CatalogConfig struct {
	Source string `toml:"source"`
}

type AppConfig struct {
	IdentityDefault string                  `toml:"identity_default"`
	StateDir        string                  `toml:"state_dir"` // empty -> ~/.ringer
	AllowFullAccess bool                    `toml:"allow_full_access"`
	Logging         LoggingConfig           `toml:"logging"`
	Eval            EvalConfig              `toml:"eval"`
	Artifact        ArtifactConfig          `toml:"artifact"`
	Scoreboard      ScoreboardConfig        `toml:"scoreboard"`
	Catalog         CatalogConfig           `toml:"catalog"`
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

// ExpandUser expands a leading "~" or "~/" to the current user's home
// directory, mirroring Python's Path.expanduser for the paths ringer's
// config and manifests carry. Non-tilde paths pass through unchanged.
func ExpandUser(p string) string {
	if p == "~" || strings.HasPrefix(p, "~/") {
		if home, err := os.UserHomeDir(); err == nil {
			return filepath.Join(home, strings.TrimPrefix(strings.TrimPrefix(p, "~"), "/"))
		}
	}
	return p
}

func (c *AppConfig) StateDirPath() string {
	if c.StateDir != "" {
		return ExpandUser(c.StateDir)
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ".ringer"
	}
	return filepath.Join(home, ".ringer")
}

// ArtifactEnabled resolves the artifact.enabled default: absent -> true
// (Python parity), explicit `enabled = false` -> false.
func (c *AppConfig) ArtifactEnabled() bool {
	return c.Artifact.Enabled == nil || *c.Artifact.Enabled
}

func (c *AppConfig) DBPath() string {
	if c.Eval.DBPath != "" {
		return ExpandUser(c.Eval.DBPath)
	}
	return filepath.Join(c.StateDirPath(), "ringer.db")
}

// ModelIdentityPath returns the expanded override path for the identity
// registry, or "" to signal "use the embedded default".
func (c *AppConfig) ModelIdentityPath() string {
	if c.Scoreboard.ModelIdentityPath == "" {
		return ""
	}
	return ExpandUser(c.Scoreboard.ModelIdentityPath)
}

// ModelNotesPath is the override for MODEL-NOTES, or "" for the embedded default.
func (c *AppConfig) ModelNotesPath() string {
	if c.Scoreboard.ModelNotesPath == "" {
		return ""
	}
	return ExpandUser(c.Scoreboard.ModelNotesPath)
}

// CatalogSource returns the configured catalog fetch source override, or ""
// to signal "use the caller's default" (catalog.DefaultSource).
func (c *AppConfig) CatalogSource() string {
	return c.Catalog.Source
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
	switch c.Logging.Format {
	case "", "text", "json":
	default:
		return nil, fmt.Errorf("config %s: logging.format must be \"text\" or \"json\", got %q", path, c.Logging.Format)
	}
	return &c, nil
}
