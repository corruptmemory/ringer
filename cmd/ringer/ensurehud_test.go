package main

import (
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/corruptmemory/ringer/internal/logging"
)

func portOf(t *testing.T, addr string) int {
	t.Helper()
	_, p, _ := net.SplitHostPort(addr)
	var n int
	fmt.Sscanf(p, "%d", &n)
	return n
}

func TestHudProbe(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/healthz" {
			w.WriteHeader(http.StatusOK)
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer ts.Close()
	if !hudIsAlive(portOf(t, ts.Listener.Addr().String())) {
		t.Fatal("probe should detect the live server")
	}
	ln, _ := net.Listen("tcp", "127.0.0.1:0")
	free := portOf(t, ln.Addr().String())
	ln.Close()
	if hudIsAlive(free) {
		t.Fatal("probe should be false on a closed port")
	}
}

func TestEnsureHudRunningBestEffort(t *testing.T) {
	// openBrowser=false; a free port with no server. Must return promptly,
	// not panic, not error even if the spawned hud never comes up.
	ln, _ := net.Listen("tcp", "127.0.0.1:0")
	port := portOf(t, ln.Addr().String())
	ln.Close()
	ensureHUDRunning(t.TempDir(), port, logging.Default(), false)
}
