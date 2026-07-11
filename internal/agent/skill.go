package agent

import _ "embed"

// SkillMarkdown is the ringer Claude Code skill, embedded so the static binary
// can install it without the source tree. It is a committed copy of
// .claude/skills/ringer/SKILL.md — //go:embed cannot reach that path because it
// lives under a dot-directory. TestEmbeddedSkillMatchesCanonical drift-locks
// the two. To regenerate after editing the canonical file:
//
//	cp .claude/skills/ringer/SKILL.md internal/agent/SKILL.md
//
//go:embed SKILL.md
var SkillMarkdown []byte
