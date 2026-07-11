// cmd/ringer/agent.go
package main

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/corruptmemory/ringer/internal/agent"
)

type installAgentCmd struct {
	Project bool `long:"project" description:"install into ./.claude instead of ~/.claude"`
}

type uninstallAgentCmd struct {
	Project bool `long:"project" description:"remove from ./.claude instead of ~/.claude"`
}

// claudeRoot resolves the target .claude directory: ./.claude for --project,
// else ~/.claude. Mirrors ringer.py claude_root.
func claudeRoot(project bool) (string, error) {
	var base string
	if project {
		wd, err := os.Getwd()
		if err != nil {
			return "", err
		}
		base = wd
	} else {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		base = home
	}
	return filepath.Join(base, ".claude"), nil
}

// ringerBinPath is the absolute path of the running ringer binary, baked into
// the hook commands. Falls back to the bare name if the OS can't report it.
func ringerBinPath() string {
	exe, err := os.Executable()
	if err != nil || exe == "" {
		return "ringer"
	}
	return exe
}

func (c *installAgentCmd) Execute(args []string) error {
	if len(args) > 0 {
		return fmt.Errorf("install-agent: unexpected argument %q", args[0])
	}
	root, err := claudeRoot(c.Project)
	if err != nil {
		return err
	}
	res, err := agent.Install(root, ringerBinPath())
	if err != nil {
		return err
	}
	scope := "user"
	if c.Project {
		scope = "project"
	}
	fmt.Printf("Installed ringer agent for %s scope.\n", scope)
	fmt.Printf("Skill: %s\n", res.SkillTarget)
	if res.HooksChanged {
		fmt.Printf("Hooks: added PreToolUse Bash and PostToolUse Edit|Write in %s\n", res.SettingsPath)
	} else {
		fmt.Printf("Hooks: already present in %s\n", res.SettingsPath)
	}
	return nil
}

func (c *uninstallAgentCmd) Execute(args []string) error {
	if len(args) > 0 {
		return fmt.Errorf("uninstall-agent: unexpected argument %q", args[0])
	}
	root, err := claudeRoot(c.Project)
	if err != nil {
		return err
	}
	res, err := agent.Uninstall(root)
	if err != nil {
		return err
	}
	scope := "user"
	if c.Project {
		scope = "project"
	}
	fmt.Printf("Uninstalled ringer agent for %s scope.\n", scope)
	fmt.Printf("Hooks removed: %d\n", res.HooksRemoved)
	skillMsg := "no"
	if res.SkillRemoved {
		skillMsg = "yes"
	}
	fmt.Printf("Skill removed: %s\n", skillMsg)
	return nil
}

func init() {
	parser.AddCommand("install-agent",
		"Install the ringer Claude Code skill and hooks",
		"Copy the ringer skill and register the routing-nudge hooks in settings.json (idempotent; backs up settings).",
		&installAgentCmd{})
	parser.AddCommand("uninstall-agent",
		"Remove the ringer Claude Code skill and hooks",
		"Remove the ringer routing-nudge hooks from settings.json and delete the installed skill.",
		&uninstallAgentCmd{})
}
