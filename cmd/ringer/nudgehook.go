// cmd/ringer/nudgehook.go
package main

import (
	"io"
	"os"

	"github.com/corruptmemory/ringer/internal/nudge"
)

type nudgeHookCmd struct {
	Args struct {
		Mode string `positional-arg-name:"MODE" description:"pre-bash or post-edit"`
	} `positional-args:"yes"`
}

// runNudgeHook resolves the ringer home and runs the nudge engine, swallowing
// every error and panic: a Claude Code hook must ALWAYS exit 0 so it can never
// break the agent's tool call (frozen contract, spec §9.9).
func runNudgeHook(mode string, stdin io.Reader, stdout io.Writer) (err error) {
	defer func() { _ = recover(); err = nil }()
	_ = nudge.Run(mode, stdin, stdout, nudge.RingerHome())
	return nil
}

func (c *nudgeHookCmd) Execute(args []string) error {
	return runNudgeHook(c.Args.Mode, os.Stdin, os.Stdout)
}

func init() {
	parser.AddCommand("nudge-hook",
		"Claude Code routing nudge hook (pre-bash|post-edit)",
		"Read a Claude Code hook payload on stdin and, for swarm-shaped inline work, print a routing nudge. Always exits 0.",
		&nudgeHookCmd{})
}
