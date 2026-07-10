package artifact

import (
	"os"
	"path/filepath"
	"testing"
)

func writeFile(t *testing.T, path string, size int) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, make([]byte, size), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestHarvestExpectFilesInOrder(t *testing.T) {
	sd := t.TempDir()
	td := t.TempDir()
	writeFile(t, filepath.Join(td, "b.txt"), 3)
	writeFile(t, filepath.Join(td, "a.txt"), 4)
	got, notes, err := HarvestOnPass(sd, "run-1", "alpha", td, []string{"b.txt", "a.txt"}, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(notes) != 0 {
		t.Errorf("unexpected notes: %v", notes)
	}
	if len(got) != 2 || got[0].Name != "b.txt" || got[1].Name != "a.txt" {
		t.Fatalf("want [b.txt a.txt] in declaration order, got %+v", got)
	}
	if got[0].TaskKey != "alpha" || got[0].Bytes != 3 {
		t.Errorf("record wrong: %+v", got[0])
	}
	// Copied under deliverables/<run>/<task>/<name>.
	want := filepath.Join(DeliverablesDir(sd, "run-1", "alpha"), "b.txt")
	if got[0].Path != want {
		t.Errorf("path = %q, want %q", got[0].Path, want)
	}
	if _, err := os.Stat(want); err != nil {
		t.Errorf("deliverable not copied: %v", err)
	}
}

func TestHarvestMissingDeclaredFileSkippedSilently(t *testing.T) {
	sd, td := t.TempDir(), t.TempDir()
	writeFile(t, filepath.Join(td, "present.txt"), 2)
	got, notes, err := HarvestOnPass(sd, "r", "t", td, []string{"present.txt", "gone.txt"}, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Name != "present.txt" || len(notes) != 0 {
		t.Fatalf("missing file must be skipped silently: got=%+v notes=%v", got, notes)
	}
}

func TestHarvestFallbackGlobSortedCappedAt8(t *testing.T) {
	sd, td := t.TempDir(), t.TempDir()
	for _, n := range []string{"09.md", "01.md", "02.md", "03.md", "04.md", "05.md", "06.md", "07.md", "08.md"} {
		writeFile(t, filepath.Join(td, n), 1)
	}
	writeFile(t, filepath.Join(td, ".hidden.md"), 1) // dotfile excluded
	writeFile(t, filepath.Join(td, "note.log"), 1)   // .log excluded from fallback
	writeFile(t, filepath.Join(td, "data.bin"), 1)   // unlisted suffix excluded
	got, notes, err := HarvestOnPass(sd, "r", "t", td, nil, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 8 {
		t.Fatalf("want cap 8, got %d", len(got))
	}
	if got[0].Name != "01.md" || got[7].Name != "08.md" {
		t.Errorf("want first-8-alphabetical, got first=%s last=%s", got[0].Name, got[7].Name)
	}
	if len(notes) == 0 {
		t.Errorf("expected a cap note")
	}
}

func TestHarvestWorktreesNoExpectFilesHarvestsNothing(t *testing.T) {
	sd, td := t.TempDir(), t.TempDir()
	writeFile(t, filepath.Join(td, "x.md"), 1)
	got, _, err := HarvestOnPass(sd, "r", "t", td, nil, true)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 {
		t.Errorf("worktrees + no expect_files must harvest nothing, got %+v", got)
	}
}

func TestHarvestOversizedSkippedWithNote(t *testing.T) {
	sd, td := t.TempDir(), t.TempDir()
	writeFile(t, filepath.Join(td, "big.md"), int(DeliverableMaxBytes)+1)
	writeFile(t, filepath.Join(td, "ok.md"), 5)
	got, notes, err := HarvestOnPass(sd, "r", "t", td, []string{"big.md", "ok.md"}, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Name != "ok.md" {
		t.Fatalf("oversized must be skipped, got %+v", got)
	}
	if len(notes) != 1 {
		t.Errorf("want 1 oversize note, got %v", notes)
	}
}
