package main

import (
	"fmt"

	"github.com/corruptmemory/ringer/internal/config"
	"github.com/corruptmemory/ringer/internal/hud"
	"github.com/corruptmemory/ringer/internal/logging"
)

type hudCmd struct {
	Port   int  `long:"port" description:"Ringside port (default 8700)"`
	NoOpen bool `long:"no-open" description:"do not open a browser (accepted for the detached-spawn path)"`
}

func (c *hudCmd) Execute(args []string) error {
	cfgPath := opts.Config
	if cfgPath == "" {
		cfgPath = config.DefaultPath()
	}
	cfg, err := config.Load(cfgPath)
	if err != nil {
		return err
	}
	lvl, err := resolveLogLevel(opts.LogLevel, cfg)
	if err != nil {
		return fmt.Errorf("--log-level: %w", err)
	}
	lg, err := logging.New(logging.Config{Level: lvl, Format: cfg.Logging.Format})
	if err != nil {
		return err
	}
	port := c.Port
	if port == 0 {
		port = cfg.HudPort()
	}
	return hud.New(cfg.StateDirPath(), lg).ListenAndServe(port) // blocks until killed
}

func init() {
	parser.AddCommand("hud", "Serve the Ringside dashboard",
		"Run the Ringside HUD on 127.0.0.1:8700 (templ+htmx; single fixed port, fails if taken).",
		&hudCmd{})
}
