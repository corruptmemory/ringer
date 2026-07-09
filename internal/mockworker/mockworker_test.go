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

func TestRunGrammarEdgeCases(t *testing.T) {
	tests := []struct {
		name       string
		spec       string
		wantCode   int
		wantStderr string
		wantFile   string // relative path that must NOT exist afterward
	}{
		{
			name:       "MOCK_FAIL after a completed MOCK_FILE block prevents the write",
			spec:       "MOCK_FILE: out.txt\nhello\nMOCK_END\nMOCK_FAIL\n",
			wantCode:   1,
			wantStderr: "simulated failure",
			wantFile:   "out.txt",
		},
		{
			name:       "unterminated MOCK_FILE block fails without writing",
			spec:       "MOCK_FILE: out.txt\nhello world\n",
			wantCode:   1,
			wantStderr: "unterminated",
			wantFile:   "out.txt",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			var errb bytes.Buffer
			code := Run(tt.spec, dir, &bytes.Buffer{}, &errb)
			if code != tt.wantCode {
				t.Errorf("exit = %d, want %d", code, tt.wantCode)
			}
			if !bytes.Contains(errb.Bytes(), []byte(tt.wantStderr)) {
				t.Errorf("stderr = %q, want substring %q", errb.String(), tt.wantStderr)
			}
			if _, err := os.Stat(filepath.Join(dir, tt.wantFile)); !os.IsNotExist(err) {
				t.Errorf("file %q: want not-exist, stat err = %v", tt.wantFile, err)
			}
		})
	}
}
