package main

import (
	"fmt"
	"os"

	"github.com/corruptmemory/ringer/internal/lint"
	"github.com/corruptmemory/ringer/internal/manifest"
)

type lintCmd struct {
	Args struct {
		Manifest string `positional-arg-name:"MANIFEST" description:"path to the manifest JSON"`
	} `positional-args:"yes" required:"yes"`
}

func (c *lintCmd) Execute(args []string) error {
	m, err := manifest.FromPath(c.Args.Manifest)
	if err != nil {
		return err
	}
	findings := lint.Check(m)
	printLintFindings(findings)
	if len(findings) > 0 {
		os.Exit(1)
	}
	fmt.Fprintf(os.Stdout, "lint: clean (%d tasks)\n", len(m.Tasks))
	return nil
}

// printLintFindings prints one "lint: ..." line per finding, matching the
// Python original's print_lint_findings.
func printLintFindings(findings []lint.Finding) {
	for _, f := range findings {
		key := f.TaskKey
		if key == "" {
			key = "manifest"
		}
		fmt.Fprintf(os.Stdout, "lint: %s: %s\n", key, f.Message)
	}
}

func init() {
	parser.AddCommand("lint", "Lint a manifest", "Check a manifest for checks/specs that can't be trusted.", &lintCmd{})
}
