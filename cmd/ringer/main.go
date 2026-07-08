package main

import (
	"os"

	"github.com/jessevdk/go-flags"
)

type rootOptions struct {
	Config string `long:"config" description:"Path to config TOML (default: $RINGER_CONFIG or ~/.config/ringer/config.toml)"`
}

var opts rootOptions
var parser = flags.NewParser(&opts, flags.Default)

func main() {
	if _, err := parser.Parse(); err != nil {
		if flags.WroteHelp(err) {
			os.Exit(0)
		}
		os.Exit(1)
	}
}
