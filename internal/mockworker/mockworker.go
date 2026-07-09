package mockworker

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// mockFileBlock is one parsed MOCK_FILE...MOCK_END block: the raw (untrimmed)
// path text and the content to write.
type mockFileBlock struct {
	path    string
	content string
}

// mockFailOnceMarker is the per-taskdir sentinel file MOCK_FAIL_ONCE checks
// for. This is NOT part of the frozen Python-parity grammar (MOCK_FILE /
// MOCK_END / MOCK_FAIL) — it is a Go-only test seam, added deliberately (per
// Plan 2 Task 9's brief) so the runner's fail-then-retry path can be
// exercised deterministically end-to-end without faking a retry. Kept
// minimal and additive: MOCK_FAIL_ONCE is inert once the marker exists.
const mockFailOnceMarker = ".mock-fail-once"

// Run executes the MOCK_FILE/MOCK_END/MOCK_FAIL spec grammar, mirroring
// engines/mock_worker.py exactly:
//
//   - MOCK_FAIL is detected via an upfront scan of every line in the spec
//     (has_fail_directive in Python), not sequentially during block parsing.
//     A MOCK_FAIL line anywhere — even inside what looks like a MOCK_FILE
//     block's content — short-circuits with zero filesystem side effects.
//   - The whole spec is parsed into blocks (parse_blocks in Python) before
//     any file is written. An unterminated MOCK_FILE block (MOCK_END never
//     reached) is a parse error, so it also produces zero filesystem side
//     effects, matching Python's parse-then-write phasing.
//
// A third, Go-only directive, MOCK_FAIL_ONCE, fails exactly the first
// invocation for a given workDir: on that first call the per-taskdir marker
// file (mockFailOnceMarker) is absent, so it is created and the run fails
// with zero other side effects (same contract as MOCK_FAIL). On any
// subsequent invocation against the same workDir the marker already exists,
// so MOCK_FAIL_ONCE is treated as absent and processing falls through to
// normal MOCK_FILE/MOCK_END handling. This lets a caller (the runner's
// fail-then-retry loop, which re-invokes the worker in the same taskDir)
// deterministically fail attempt 1 and pass attempt 2.
func Run(spec, workDir string, stdout, stderr io.Writer) int {
	lines := strings.Split(spec, "\n")

	for _, line := range lines {
		if strings.TrimSpace(line) == "MOCK_FAIL" {
			fmt.Fprintln(stderr, "mock-worker: simulated failure")
			return 1
		}
	}

	for _, line := range lines {
		if strings.TrimSpace(line) != "MOCK_FAIL_ONCE" {
			continue
		}
		marker := filepath.Join(workDir, mockFailOnceMarker)
		if _, err := os.Stat(marker); err == nil {
			break // marker already present: MOCK_FAIL_ONCE is spent, proceed normally
		} else if !os.IsNotExist(err) {
			fmt.Fprintf(stderr, "mock-worker: %v\n", err)
			return 1
		}
		if err := os.WriteFile(marker, nil, 0o644); err != nil {
			fmt.Fprintf(stderr, "mock-worker: %v\n", err)
			return 1
		}
		fmt.Fprintln(stderr, "mock-worker: simulated failure (once)")
		return 1
	}

	blocks, err := parseBlocks(lines)
	if err != nil {
		fmt.Fprintf(stderr, "mock-worker: %v\n", err)
		return 1
	}

	for _, b := range blocks {
		dest, err := resolveOutputPath(workDir, b.path)
		if err != nil {
			fmt.Fprintf(stderr, "mock-worker: %v\n", err)
			return 1
		}
		if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
			fmt.Fprintf(stderr, "mock-worker: %v\n", err)
			return 1
		}
		if err := os.WriteFile(dest, []byte(b.content), 0o644); err != nil {
			fmt.Fprintf(stderr, "mock-worker: %v\n", err)
			return 1
		}
		fmt.Fprintf(stdout, "mock-worker: wrote %s\n", b.path)
	}

	return 0
}

// parseBlocks parses every MOCK_FILE...MOCK_END block in lines, mirroring
// parse_blocks in engines/mock_worker.py. It returns an error (and no
// blocks) on the first malformed block — including a MOCK_FILE block that
// runs off the end of the spec without a matching MOCK_END — so that a
// malformed block anywhere in the spec prevents every write, not just the
// write for that block.
func parseBlocks(lines []string) ([]mockFileBlock, error) {
	var blocks []mockFileBlock
	i := 0
	for i < len(lines) {
		rel, ok := strings.CutPrefix(lines[i], "MOCK_FILE: ")
		if !ok {
			i++
			continue
		}
		path := strings.TrimSpace(rel)
		if path == "" {
			return nil, fmt.Errorf("empty MOCK_FILE path")
		}
		i++

		var content []string
		for i < len(lines) && strings.TrimSpace(lines[i]) != "MOCK_END" {
			content = append(content, lines[i])
			i++
		}
		if i >= len(lines) {
			return nil, fmt.Errorf("unterminated MOCK_FILE block: %s", path)
		}

		body := strings.Join(content, "\n")
		if len(content) > 0 {
			body += "\n"
		}
		blocks = append(blocks, mockFileBlock{path: path, content: body})
		i++ // skip MOCK_END
	}
	return blocks, nil
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
