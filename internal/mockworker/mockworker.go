package mockworker

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

func Run(spec, workDir string, stdout, stderr io.Writer) int {
	lines := strings.Split(spec, "\n")
	for i := 0; i < len(lines); i++ {
		line := lines[i]
		if strings.TrimSpace(line) == "MOCK_FAIL" {
			fmt.Fprintln(stderr, "mock-worker: simulated failure")
			return 1
		}
		if rel, ok := strings.CutPrefix(line, "MOCK_FILE: "); ok {
			var content []string
			i++
			for ; i < len(lines); i++ {
				if strings.TrimSpace(lines[i]) == "MOCK_END" {
					break
				}
				content = append(content, lines[i])
			}
			dest, err := resolveOutputPath(workDir, strings.TrimSpace(rel))
			if err != nil {
				fmt.Fprintf(stderr, "mock-worker: %v\n", err)
				return 1
			}
			if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
				fmt.Fprintf(stderr, "mock-worker: %v\n", err)
				return 1
			}
			body := strings.Join(content, "\n")
			if len(content) > 0 {
				body += "\n"
			}
			if err := os.WriteFile(dest, []byte(body), 0o644); err != nil {
				fmt.Fprintf(stderr, "mock-worker: %v\n", err)
				return 1
			}
			fmt.Fprintf(stdout, "mock-worker: wrote %s\n", rel)
		}
	}
	return 0
}

func resolveOutputPath(workDir, rel string) (string, error) {
	if filepath.IsAbs(rel) {
		return "", fmt.Errorf("absolute path not allowed: %s", rel)
	}
	dest := filepath.Join(workDir, rel)
	clean := filepath.Clean(dest)
	base := filepath.Clean(workDir)
	if clean != base && !strings.HasPrefix(clean, base+string(os.PathSeparator)) {
		return "", fmt.Errorf("path escapes task dir: %s", rel)
	}
	return clean, nil
}
