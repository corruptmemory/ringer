// Package artifact holds the on-disk artifact-tree contract shared by the
// HUD (which serves + renders it) and the Plan-4b renderer (which writes
// it): the canonical path layout under <state_dir>/artifacts and the
// frozen, unversioned library.json schema.
package artifact

import (
	"path/filepath"
	"regexp"
	"strings"
)

func ArtifactsDir(stateDir string) string { return filepath.Join(stateDir, "artifacts") }
func LibraryPath(stateDir string) string {
	return filepath.Join(ArtifactsDir(stateDir), "library.json")
}

func DeliverablesDir(stateDir, runID, taskKey string) string {
	return filepath.Join(ArtifactsDir(stateDir), "deliverables", SanitizeName(runID), SanitizeName(taskKey))
}

var unsafeNameRe = regexp.MustCompile(`[^A-Za-z0-9._-]+`)

// SanitizeName maps a run_id/run_name/task_key to one safe path component
// (mirrors upstream sanitize_artifact_name so on-disk paths match).
func SanitizeName(s string) string {
	cleaned := strings.Trim(unsafeNameRe.ReplaceAllString(s, "-"), "-")
	if cleaned == "" {
		return "unnamed"
	}
	return cleaned
}
