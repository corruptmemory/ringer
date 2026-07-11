// cmd/ringer/genconfig.go
package main

import (
	"fmt"
	"os"

	"github.com/corruptmemory/ringer/internal/config"
)

type genConfigCmd struct {
	Output string `short:"o" long:"output" description:"output path, or - for stdout (default stdout)"`
	Force  bool   `long:"force" description:"overwrite an existing output file"`
}

func (c *genConfigCmd) Execute(args []string) error {
	if len(args) > 0 {
		return fmt.Errorf("gen-config: unexpected argument %q", args[0])
	}
	out, err := config.RenderDocumented(config.ExampleConfig())
	if err != nil {
		return err
	}
	if c.Output == "" || c.Output == "-" {
		fmt.Print(out)
		return nil
	}
	if !c.Force {
		if _, err := os.Stat(c.Output); err == nil {
			return fmt.Errorf("gen-config: %s exists (use --force to overwrite)", c.Output)
		}
	}
	if err := os.WriteFile(c.Output, []byte(out), 0o644); err != nil {
		return err
	}
	fmt.Printf("wrote %s\n", c.Output)
	return nil
}

func init() {
	parser.AddCommand("gen-config", "Generate a documented sample config",
		"Generate a documented TOML config from the config structs (self-documenting; won't drift).",
		&genConfigCmd{})
}
