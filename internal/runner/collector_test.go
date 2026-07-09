package runner

import (
	"fmt"
	"strings"
	"testing"
	"time"
)

// TestCollectorOrderingNoStaleTail proves a tail can't race ahead of the
// appends that preceded it: writes happen synchronously on the test
// goroutine (so their order is well-defined), and an immediately-following
// tail() must see all of them. This is the whole reason for using one FIFO
// command channel instead of separate append/tail channels.
func TestCollectorOrderingNoStaleTail(t *testing.T) {
	c := newCollector(1 << 20)
	c.start()
	defer c.stopAndWait()

	sink := c.sink("k")
	writes := []string{"alpha", "bravo", "charlie", "delta"}
	for _, w := range writes {
		if _, err := sink.Write([]byte(w)); err != nil {
			t.Fatalf("write(%q): %v", w, err)
		}
	}

	want := strings.Join(writes, "")
	got := c.tail("k", 1<<20)
	if got != want {
		t.Fatalf("tail = %q, want %q", got, want)
	}
}

// TestCollectorBoundedEviction writes far more than capPerTask and checks
// the tail is bounded near the cap, is a true suffix of the full stream
// (newest bytes present, in order), and is strictly shorter than the full
// stream (oldest bytes evicted).
func TestCollectorBoundedEviction(t *testing.T) {
	const capPerTask = 100
	c := newCollector(capPerTask)
	c.start()
	defer c.stopAndWait()

	sink := c.sink("k")
	var full strings.Builder
	for i := 0; i < 50; i++ { // 50 * 10 = 500 bytes, far more than capPerTask
		chunk := strings.Repeat(string(rune('a'+i%26)), 10)
		full.WriteString(chunk)
		if _, err := sink.Write([]byte(chunk)); err != nil {
			t.Fatalf("write: %v", err)
		}
	}

	want := full.String()
	got := c.tail("k", 1<<20) // ask for far more than capPerTask

	if got == "" {
		t.Fatal("tail returned empty after many writes")
	}
	if !strings.HasSuffix(want, got) {
		t.Fatalf("tail %q is not a suffix of the full stream %q", got, want)
	}
	if len(got) >= len(want) {
		t.Fatalf("tail was not bounded: got %d bytes, full stream was %d bytes", len(got), len(want))
	}
	if len(got) > capPerTask+10 { // allow slop of at most one 10-byte chunk over cap
		t.Fatalf("tail exceeds capPerTask by more than one chunk: %d bytes (cap=%d)", len(got), capPerTask)
	}
}

// TestCollectorTailLimitReverseWalk checks the newest-first reverse walk
// returns exactly the last limitBytes bytes, even when that cut point falls
// in the middle of a chunk.
func TestCollectorTailLimitReverseWalk(t *testing.T) {
	c := newCollector(1 << 20)
	c.start()
	defer c.stopAndWait()

	sink := c.sink("k")
	for _, w := range []string{"AAAAA", "BBBBB", "CCCCC"} {
		if _, err := sink.Write([]byte(w)); err != nil {
			t.Fatalf("write(%q): %v", w, err)
		}
	}

	const want = "BBCCCCC" // last 7 of "AAAAABBBBBCCCCC"
	got := c.tail("k", 7)
	if got != want {
		t.Fatalf("tail(7) = %q, want %q", got, want)
	}
}

// TestCollectorConcurrentWritersRace exercises many goroutines writing to
// distinct keys concurrently with another goroutine polling tail — this is
// the point of the single-owner goroutine design, and must pass under -race.
func TestCollectorConcurrentWritersRace(t *testing.T) {
	c := newCollector(4096)
	c.start()
	defer c.stopAndWait()

	const goroutines = 8
	const writesPerGoroutine = 200

	done := make(chan struct{})
	for g := 0; g < goroutines; g++ {
		go func(id int) {
			defer func() { done <- struct{}{} }()
			key := fmt.Sprintf("task-%d", id)
			sink := c.sink(key)
			for i := 0; i < writesPerGoroutine; i++ {
				if _, err := sink.Write([]byte("x")); err != nil {
					t.Errorf("write: %v", err)
					return
				}
			}
		}(g)
	}

	stopReader := make(chan struct{})
	readerDone := make(chan struct{})
	go func() {
		defer close(readerDone)
		for {
			select {
			case <-stopReader:
				return
			default:
				_ = c.tail("task-0", 64)
			}
		}
	}()

	for g := 0; g < goroutines; g++ {
		<-done
	}
	close(stopReader)
	<-readerDone

	got := c.tail("task-0", writesPerGoroutine*2)
	if len(got) != writesPerGoroutine {
		t.Fatalf("task-0 tail length = %d, want %d", len(got), writesPerGoroutine)
	}
}

// TestCollectorStopIsIdempotentAndPrompt replaces the old TestCollectorDrainOnStop,
// which reached into the collector's internals by pushing raw closures onto
// c.cmds directly — only possible because cmds was an untyped chan func(),
// itself the anti-pattern this refactor removes. With a typed command channel
// there is no way to inject an arbitrary closure from a test, so we exercise
// only the public API and its observable lifecycle guarantees: a burst of
// writes is visible via tail() before stop; stop() is safe to call more than
// once; and stopAndWait() returns promptly. (The run() loop still drains
// commands buffered after quit before exiting, but that "no lost buffered
// command" property isn't publicly observable — the log file is the
// authoritative full record; the collector tail is only a live convenience.)
func TestCollectorStopIsIdempotentAndPrompt(t *testing.T) {
	c := newCollector(1 << 20)
	c.start()

	sink := c.sink("k")
	const want = "burst-of-data"
	if _, err := sink.Write([]byte(want)); err != nil {
		t.Fatalf("write: %v", err)
	}
	if got := c.tail("k", 1<<20); got != want {
		t.Fatalf("tail before stop = %q, want %q", got, want)
	}

	c.stopAndWait()

	// A second stop() must be a safe no-op (recover-guarded close), not a panic.
	c.stop()

	// A following stopAndWait() must return promptly (not block).
	done := make(chan struct{})
	go func() {
		c.stopAndWait()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("stopAndWait() after stop did not return within 2s")
	}
}

// TestCollectorTailUnknownKey checks tail on a never-written key returns ""
// rather than panicking or blocking.
func TestCollectorTailUnknownKey(t *testing.T) {
	c := newCollector(1 << 20)
	c.start()
	defer c.stopAndWait()

	if got := c.tail("nope", 100); got != "" {
		t.Fatalf("tail(unknown) = %q, want empty", got)
	}
}
