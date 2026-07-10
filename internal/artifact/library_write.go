package artifact

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/corruptmemory/ringer/internal/state"
)

const ArtifactLibraryMaxVersions = 20

// OutcomeFromState maps a run snapshot to its library outcome (ringer.py:1934-
// 1941, minus the "died" branch which only reconcile produces): live while
// running, else fail if any task failed/timed out, else pass.
func OutcomeFromState(rs state.RunState) string {
	if !rs.Done {
		return "live"
	}
	for _, t := range rs.Tasks {
		if t.Status == "failed" || t.Status == "timeout" {
			return "fail"
		}
	}
	return "pass"
}

// UpdateLibraryLive read-modify-writes the entry for run_name, setting the
// live fields and preserving any existing versions (ringer.py:1967-1989). The
// artifacts map is keyed by the RAW run_name.
func UpdateLibraryLive(stateDir, runName, runID, identity, livePath, entryState, nowISO string) error {
	lib := ReadLibrary(stateDir)
	prev := lib.Artifacts[runName] // zero Entry if absent; Versions nil is fine
	lib.Artifacts[runName] = Entry{
		LivePath:     livePath,
		State:        entryState,
		Identity:     identity,
		CurrentRunID: runID,
		UpdatedAt:    nowISO,
		Versions:     prev.Versions,
	}
	return WriteLibrary(stateDir, lib)
}

// VersionRecord bundles the fields AppendLibraryVersion needs to build a
// frozen version + flip the entry to its final outcome.
type VersionRecord struct {
	RunName, RunID, Identity, LivePath, VersionPath string
	ReportPath                                      *string
	Outcome                                         string
	TasksPass, TasksFail                            int
	Deliverables                                    []state.Deliverable
}

// AppendLibraryVersion prepends a new version (de-duping any prior version with
// the same run_id), rewrites the entry with state=outcome, caps to 20 kept, and
// prunes the pruned tail's files off disk (ringer.py:1992-2054).
func AppendLibraryVersion(stateDir string, r VersionRecord, nowISO string) error {
	lib := ReadLibrary(stateDir)
	prev := lib.Artifacts[r.RunName]
	newVersion := Version{
		RunID:        r.RunID,
		Path:         r.VersionPath,
		ReportPath:   r.ReportPath,
		FinishedAt:   nowISO,
		Outcome:      r.Outcome,
		TasksPass:    r.TasksPass,
		TasksFail:    r.TasksFail,
		Deliverables: r.Deliverables,
	}
	versions := []Version{newVersion}
	for _, v := range prev.Versions {
		if v.RunID != r.RunID {
			versions = append(versions, v)
		}
	}
	kept, pruned := versions, []Version(nil)
	if len(versions) > ArtifactLibraryMaxVersions {
		kept = versions[:ArtifactLibraryMaxVersions]
		pruned = versions[ArtifactLibraryMaxVersions:]
	}
	lib.Artifacts[r.RunName] = Entry{
		LivePath:     r.LivePath,
		State:        r.Outcome,
		Identity:     r.Identity,
		CurrentRunID: r.RunID,
		UpdatedAt:    nowISO,
		Versions:     kept,
	}
	if err := WriteLibrary(stateDir, lib); err != nil {
		return err
	}
	pruneVersions(stateDir, pruned)
	return nil
}

// pruneVersions deletes each pruned version's page + report file, but only when
// the path resolves strictly INSIDE the artifacts root (ringer.py:2039-2054).
// Best-effort: errors are swallowed (the JSON is already correct). Deliverables
// are never pruned.
func pruneVersions(stateDir string, pruned []Version) {
	root, err := filepath.EvalSymlinks(ArtifactsDir(stateDir))
	if err != nil {
		return
	}
	del := func(p string) {
		if p == "" {
			return
		}
		resolved, err := filepath.EvalSymlinks(p)
		if err != nil {
			return
		}
		rel, err := filepath.Rel(root, resolved)
		if err != nil || rel == "." || rel == ".." || strings.HasPrefix(rel, ".."+string(os.PathSeparator)) {
			return // not strictly inside the artifacts root
		}
		if info, err := os.Stat(resolved); err == nil && info.Mode().IsRegular() {
			_ = os.Remove(resolved)
			_ = os.Remove(filepath.Dir(resolved)) // rmdir if now empty (best-effort)
		}
	}
	for _, v := range pruned {
		del(v.Path)
		if v.ReportPath != nil {
			del(*v.ReportPath)
		}
	}
}
