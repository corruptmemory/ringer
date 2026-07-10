package artifact

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/corruptmemory/ringer/internal/state"
)

const (
	DeliverableMaxBytes     = 20 * 1024 * 1024 // 20 MiB; oversized deliverables are skipped
	FallbackHarvestMaxFiles = 8
)

// fallbackHarvestSuffixes is (TEXT_DELIVERABLE − {.log}) ∪ IMAGE ∪ extras
// (ringer.py:74-78): file types worth rescuing from a task dir that declared
// no expect_files. .log excluded — the worker log is linked separately.
var fallbackHarvestSuffixes = map[string]bool{
	".md": true, ".txt": true,
	".avif": true, ".gif": true, ".jpeg": true, ".jpg": true, ".png": true, ".svg": true, ".webp": true,
	".html": true, ".htm": true, ".json": true, ".csv": true, ".pdf": true, ".mp4": true, ".webm": true, ".mov": true,
}

// HarvestOnPass copies a passing task's deliverable files into the artifacts
// tree and returns the records + human-readable notes for anything skipped.
// Ported from _harvest_deliverables_on_pass (ringer.py:6923-6983). Never
// returns a hard error for a per-file copy failure that leaves the run
// unaffected; err is non-nil only for an unexpected filesystem failure while
// preparing the destination.
func HarvestOnPass(stateDir, runID, taskKey, taskDir string, expectFiles []string, worktrees bool) ([]state.Deliverable, []string, error) {
	names := expectFiles
	var notes []string
	if len(names) == 0 {
		if worktrees {
			return nil, nil, nil // worktree taskdir is a whole repo; nothing to guess
		}
		globbed, capNote := fallbackCandidates(taskDir)
		names = globbed
		if capNote != "" {
			notes = append(notes, capNote)
		}
	}
	if len(names) == 0 {
		return nil, notes, nil
	}
	targetDir := DeliverablesDir(stateDir, runID, taskKey)
	var out []state.Deliverable
	for _, rel := range names {
		src := expectFilePath(taskDir, rel)
		info, err := os.Stat(src)
		if err != nil || !info.Mode().IsRegular() {
			continue // missing / non-file: silently skipped (verify already guaranteed presence on PASS)
		}
		if info.Size() > DeliverableMaxBytes {
			notes = append(notes, fmt.Sprintf("%s was not copied because it is larger than 20 MB.", filepath.Base(src)))
			continue
		}
		dst := filepath.Join(targetDir, filepath.Base(src))
		if err := copyFilePreservingMtime(src, dst); err != nil {
			return nil, notes, fmt.Errorf("harvest %s: %w", rel, err)
		}
		di, _ := os.Stat(dst)
		var size int64
		if di != nil {
			size = di.Size()
		}
		out = append(out, state.Deliverable{TaskKey: taskKey, Name: filepath.Base(src), Path: dst, Bytes: size})
	}
	return out, notes, nil
}

// expectFilePath resolves a declared expect_files entry: absolute or ~-prefixed
// as-is, otherwise relative to taskDir (ringer.py:6747-6749).
func expectFilePath(taskDir, p string) string {
	if strings.HasPrefix(p, "~") {
		if home, err := os.UserHomeDir(); err == nil {
			return filepath.Join(home, strings.TrimPrefix(strings.TrimPrefix(p, "~"), "/"))
		}
	}
	if filepath.IsAbs(p) {
		return p
	}
	return filepath.Join(taskDir, p)
}

// fallbackCandidates globs the top level of taskDir for harvestable files,
// alphabetical, capped at FallbackHarvestMaxFiles. Returns the kept names and
// a note if the cap truncated the list.
func fallbackCandidates(taskDir string) ([]string, string) {
	entries, err := os.ReadDir(taskDir)
	if err != nil {
		return nil, ""
	}
	var names []string
	for _, e := range entries {
		if e.IsDir() || strings.HasPrefix(e.Name(), ".") {
			continue
		}
		if !fallbackHarvestSuffixes[strings.ToLower(filepath.Ext(e.Name()))] {
			continue
		}
		if info, err := e.Info(); err != nil || !info.Mode().IsRegular() {
			continue
		}
		names = append(names, e.Name())
	}
	sort.Strings(names)
	if len(names) > FallbackHarvestMaxFiles {
		note := fmt.Sprintf("Showing the first %d of %d files produced.", FallbackHarvestMaxFiles, len(names))
		return names[:FallbackHarvestMaxFiles], note
	}
	return names, ""
}

func copyFilePreservingMtime(src, dst string) error {
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		return err
	}
	if err := out.Close(); err != nil {
		return err
	}
	if info, err := os.Stat(src); err == nil {
		_ = os.Chtimes(dst, info.ModTime(), info.ModTime())
	}
	return nil
}
