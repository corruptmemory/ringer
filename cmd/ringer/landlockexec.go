//go:build linux

package main

import (
	"fmt"
	"os"
	"os/exec"
	"syscall"

	"github.com/landlock-lsm/go-landlock/landlock"
)

// landlockExecCmd is the hidden trampoline behind the Landlock isolator:
// `ringer landlock-exec --rw P … --ro P … -- BIN ARGS…` applies a Landlock
// ruleset to THIS process, then execs BIN — the ruleset survives execve
// and is inherited by every descendant. Hidden: process plumbing, not user
// surface.
type landlockExecCmd struct {
	RW   []string `long:"rw" description:"path allowed read-write"`
	RO   []string `long:"ro" description:"path allowed read-only"`
	Args struct {
		Argv []string `positional-arg-name:"CMD" required:"1"`
	} `positional-args:"yes" required:"yes"`
}

func (c *landlockExecCmd) Execute(args []string) error {
	rules := make([]landlock.Rule, 0, len(c.RO)+len(c.RW))
	for _, p := range c.RO {
		rules = append(rules, landlock.RODirs(p))
	}
	for _, p := range c.RW {
		rules = append(rules, landlock.RWDirs(p))
	}
	// BestEffort enforces the newest ABI the kernel offers and degrades on
	// older kernels. Select() has already refused when Landlock is entirely
	// absent, so best-effort can never mean "no confinement at all". A
	// restriction failure here is fatal: exec'ing UNCONFINED would be a
	// silent isolation downgrade.
	if err := landlock.V5.BestEffort().RestrictPaths(rules...); err != nil {
		return fmt.Errorf("landlock restrict: %w", err)
	}
	bin, err := exec.LookPath(c.Args.Argv[0])
	if err != nil {
		return err
	}
	return syscall.Exec(bin, c.Args.Argv, os.Environ())
}

func init() {
	cmd, err := parser.AddCommand("landlock-exec",
		"Apply a Landlock ruleset and exec a command (internal)",
		"Internal trampoline used by the isolation fallback; not for direct use.",
		&landlockExecCmd{})
	if err == nil {
		cmd.Hidden = true
	}
}
