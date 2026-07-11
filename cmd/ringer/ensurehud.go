package main

import (
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"syscall"
	"time"

	"github.com/corruptmemory/ringer/internal/logging"
)

// hudIsAlive probes 127.0.0.1:<port>/healthz with a short timeout; true only
// on a 200.
func hudIsAlive(port int) bool {
	client := &http.Client{Timeout: 400 * time.Millisecond}
	resp, err := client.Get(fmt.Sprintf("http://127.0.0.1:%d/healthz", port))
	if err != nil {
		return false
	}
	defer resp.Body.Close()
	return resp.StatusCode == http.StatusOK
}

// ensureHUD is the seam runManifestFile calls through so tests can assert
// whether a HUD would be spawned (e.g. dry-run must NOT) without ever
// launching a real detached `ringer hud` subprocess — Ringer has a fork-bomb
// history here (Plan 4), so a regressed guard must fail a test, not spawn.
var ensureHUD = ensureHUDRunning

// ensureHUDRunning makes the Ringside HUD available: if nothing answers the
// probe, spawn a detached `ringer hud`, poll up to ~3s for it, then open the
// browser exactly once — only when it was not already alive. Best-effort: a
// spawn/browser failure is logged, never fatal.
func ensureHUDRunning(stateDir string, port int, lg logging.Logger, openBrowser bool) {
	url := fmt.Sprintf("http://127.0.0.1:%d", port)
	alreadyAlive := hudIsAlive(port)
	if !alreadyAlive {
		if err := spawnDetachedHUD(stateDir, port); err != nil {
			lg.Warnf("hud: spawn detached: %v", err)
		} else {
			for i := 0; i < 20; i++ {
				time.Sleep(150 * time.Millisecond)
				if hudIsAlive(port) {
					break
				}
			}
		}
	}
	if openBrowser && !alreadyAlive && hudIsAlive(port) {
		if err := openInBrowser(url); err != nil {
			lg.Warnf("hud: open browser: %v", err)
		}
	}
	lg.Infof("Ringside: %s", url)
}

func spawnDetachedHUD(stateDir string, port int) error {
	self, err := os.Executable()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(stateDir, 0o755); err != nil {
		return err
	}
	logFile, err := os.OpenFile(filepath.Join(stateDir, "hud.log"), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return err
	}
	defer logFile.Close() // the child holds its own fd after Start
	cmd := exec.Command(self, "hud", "--no-open", "--port", fmt.Sprintf("%d", port))
	cmd.Stdin = nil
	cmd.Stdout = logFile
	cmd.Stderr = logFile
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true} // detach: new session
	return cmd.Start()
}

func openInBrowser(url string) error {
	switch runtime.GOOS {
	case "darwin":
		return exec.Command("open", url).Start()
	case "linux":
		return exec.Command("xdg-open", url).Start()
	default:
		return fmt.Errorf("no browser opener for %s", runtime.GOOS)
	}
}
