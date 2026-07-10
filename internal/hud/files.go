package hud

import (
	"io"
	"mime"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/corruptmemory/ringer/internal/artifact"
	"github.com/corruptmemory/ringer/internal/state"
	"github.com/go-chi/chi/v5"
)

const workerLogTailBytes = 64 * 1024

// --- /artifacts/<path> ---

func (s *Server) handleArtifacts(w http.ResponseWriter, r *http.Request) {
	full, ok := s.resolveArtifactPath(chi.URLParam(r, "*"))
	if !ok {
		http.NotFound(w, r)
		return
	}
	info, err := os.Stat(full)
	if err != nil || !info.Mode().IsRegular() {
		http.NotFound(w, r)
		return
	}
	data, err := os.ReadFile(full)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", artifactContentType(full))
	w.Header().Set("Cache-Control", "no-store")
	_, _ = w.Write(data)
}

func (s *Server) resolveArtifactPath(rel string) (string, bool) {
	root, err := filepath.Abs(artifact.ArtifactsDir(s.stateDir))
	if err != nil {
		return "", false
	}
	candidate, err := filepath.Abs(filepath.Join(root, filepath.Clean("/"+rel)))
	if err != nil {
		return "", false
	}
	if candidate == root || !strings.HasPrefix(candidate, root+string(filepath.Separator)) {
		return "", false
	}
	return candidate, true
}

func artifactContentType(path string) string {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".html", ".htm":
		return "text/html; charset=utf-8"
	case ".json":
		return "application/json; charset=utf-8"
	}
	if ct := mime.TypeByExtension(filepath.Ext(path)); ct != "" {
		return ct
	}
	return "application/octet-stream"
}

// --- /logs/<run_id>/<task_key> ---

func (s *Server) handleLogs(w http.ResponseWriter, r *http.Request) {
	runID, taskKey, ok := strings.Cut(chi.URLParam(r, "*"), "/")
	if !ok || runID == "" || taskKey == "" {
		http.NotFound(w, r)
		return
	}
	logPath, ok := s.taskLogPath(runID, taskKey)
	if !ok {
		http.NotFound(w, r)
		return
	}
	tail, err := tailBytes(logPath, workerLogTailBytes)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	_, _ = w.Write(tail)
}

func (s *Server) taskLogPath(runID, taskKey string) (string, bool) {
	runsRoot, err := filepath.Abs(filepath.Join(s.stateDir, "runs"))
	if err != nil {
		return "", false
	}
	candidate, err := filepath.Abs(filepath.Join(runsRoot, runID+".json"))
	if err != nil || filepath.Dir(candidate) != runsRoot {
		return "", false
	}
	data, err := os.ReadFile(candidate)
	if err != nil {
		return "", false
	}
	var rs state.RunState
	if err := jsonUnmarshal(data, &rs); err != nil {
		return "", false
	}
	for _, t := range rs.Tasks {
		if t.Key == taskKey && t.LogPath != "" {
			return t.LogPath, true
		}
	}
	return "", false
}

func tailBytes(path string, max int) ([]byte, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() {
		return nil, os.ErrNotExist
	}
	start := int64(0)
	if info.Size() > int64(max) {
		start = info.Size() - int64(max)
	}
	if _, err := f.Seek(start, 0); err != nil {
		return nil, err
	}
	buf := make([]byte, info.Size()-start)
	if _, err := io.ReadFull(f, buf); err != nil {
		return nil, err
	}
	return buf, nil
}

// --- /api/open-folder ---

func (s *Server) handleOpenFolder(w http.ResponseWriter, r *http.Request) {
	root, err := filepath.Abs(artifact.ArtifactsDir(s.stateDir))
	if err != nil {
		http.Error(w, "bad state dir", http.StatusInternalServerError)
		return
	}
	target := filepath.Join(root, "deliverables")
	if runID := r.URL.Query().Get("run"); runID != "" {
		// Guard the raw path before sanitizing (catches traversal attempts)
		rawTarget := filepath.Join(root, "deliverables", runID)
		rawAbs, err := filepath.Abs(rawTarget)
		if err != nil || (rawAbs != root && !strings.HasPrefix(rawAbs, root+string(filepath.Separator))) {
			http.NotFound(w, r)
			return
		}
		target = filepath.Join(target, artifact.SanitizeName(runID))
	}
	abs, err := filepath.Abs(target)
	if err != nil || (abs != root && !strings.HasPrefix(abs, root+string(filepath.Separator))) {
		http.NotFound(w, r)
		return
	}
	if info, err := os.Stat(abs); err != nil || !info.IsDir() {
		abs = root
	}
	if err := openInFileManager(abs); err != nil {
		s.lg.Warnf("hud: open-folder %s: %v", abs, err)
		http.Error(w, "could not open folder", http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// openInFileManager opens a directory: xdg-open on Linux, open on macOS
// (spec §8 fix — upstream was macOS-only). Detached; not waited on.
func openInFileManager(path string) error {
	switch runtime.GOOS {
	case "darwin":
		return exec.Command("open", path).Start()
	case "linux":
		return exec.Command("xdg-open", path).Start()
	default:
		return errUnsupportedOpen
	}
}

var errUnsupportedOpen = &openError{"open-folder is only supported on Linux (xdg-open) and macOS (open)"}

type openError struct{ msg string }

func (e *openError) Error() string { return e.msg }
