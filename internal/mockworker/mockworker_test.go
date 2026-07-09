package mockworker

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

func TestRunWritesFiles(t *testing.T) {
	dir := t.TempDir()
	spec := "MOCK_FILE: out.txt\nhello world\nMOCK_END\n"
	code := Run(spec, dir, &bytes.Buffer{}, &bytes.Buffer{})
	if code != 0 {
		t.Fatalf("exit = %d, want 0", code)
	}
	got, err := os.ReadFile(filepath.Join(dir, "out.txt"))
	if err != nil || string(got) != "hello world\n" {
		t.Fatalf("file content = %q, err %v", got, err)
	}
}

func TestRunSimulatedFailure(t *testing.T) {
	var errb bytes.Buffer
	code := Run("MOCK_FAIL\n", t.TempDir(), &bytes.Buffer{}, &errb)
	if code != 1 {
		t.Errorf("exit = %d, want 1", code)
	}
	if !bytes.Contains(errb.Bytes(), []byte("simulated failure")) {
		t.Errorf("stderr = %q", errb.String())
	}
}

func TestRunRejectsPathEscape(t *testing.T) {
	for _, bad := range []string{"/etc/passwd", "../escape.txt"} {
		code := Run("MOCK_FILE: "+bad+"\nx\nMOCK_END\n", t.TempDir(), &bytes.Buffer{}, &bytes.Buffer{})
		if code != 1 {
			t.Errorf("path %q: exit = %d, want 1", bad, code)
		}
	}
}
