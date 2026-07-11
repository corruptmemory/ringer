package main

import (
	"log/slog"
	"os"

	"github.com/corruptmemory/ringer/internal/config"
	"github.com/jessevdk/go-flags"
)

type rootOptions struct {
	Config   string `long:"config" description:"Path to config TOML (default: $RINGER_CONFIG or ~/.config/ringer/config.toml)"`
	LogLevel string `long:"log-level" description:"Minimum log level: debug, info, warn, error (default: [logging].level, or info)"`
}

var opts rootOptions
var parser = flags.NewParser(&opts, flags.Default)

// loadConfig resolves the config path (--config flag, else
// config.DefaultPath()) and loads it. Shared by commands that need cfg
// before doing anything else; hud.go/run.go still inline this (out of scope
// to refactor here — see Task 7's brief).
func loadConfig() (*config.AppConfig, error) {
	cfgPath := opts.Config
	if cfgPath == "" {
		cfgPath = config.DefaultPath()
	}
	return config.Load(cfgPath)
}

// resolveLogLevel implements flag ?? config ?? default precedence. cfg may be nil.
func resolveLogLevel(flagValue string, cfg *config.AppConfig) (slog.Level, error) {
	if flagValue != "" {
		var lvl slog.Level
		if err := lvl.UnmarshalText([]byte(flagValue)); err != nil {
			return 0, err
		}
		return lvl, nil
	}
	if cfg != nil {
		return cfg.Logging.Level, nil // zero value == slog.LevelInfo
	}
	return slog.LevelInfo, nil
}

func main() {
	if _, err := parser.Parse(); err != nil {
		if flags.WroteHelp(err) {
			os.Exit(0)
		}
		os.Exit(1)
	}
}
