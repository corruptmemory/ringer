package views

import (
	"sort"

	"github.com/corruptmemory/ringer/internal/artifact"
)

// LibraryEntry pairs an artifact library key (the raw run_name) with its entry
// for ordered rendering.
type LibraryEntry struct {
	Name  string
	Entry artifact.Entry
}

// SortedLibraryEntries returns the library's entries in a STABLE, deterministic
// order — newest UpdatedAt first, run_name as tiebreaker. LibraryPanel must
// render from this, never from a raw `range lib.Artifacts`: Go randomizes map
// iteration order per access, so a raw range makes the polled panel's rows
// flip position every refresh.
func SortedLibraryEntries(lib artifact.Library) []LibraryEntry {
	out := make([]LibraryEntry, 0, len(lib.Artifacts))
	for name, e := range lib.Artifacts {
		out = append(out, LibraryEntry{Name: name, Entry: e})
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Entry.UpdatedAt != out[j].Entry.UpdatedAt {
			return out[i].Entry.UpdatedAt > out[j].Entry.UpdatedAt
		}
		return out[i].Name < out[j].Name
	})
	return out
}
