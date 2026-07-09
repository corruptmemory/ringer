package logging

import (
	"bytes"
	"log/slog"
	"sync"
)

// Capture is a deterministic, synchronous sink for a Logger built with
// NewCapture. Every logging call writes into the buffer, under a mutex,
// before the logging method returns — no background flush, timer, or linger.
// A test can call String() immediately after a logged call and see the line.
// Also usable as the backing store for a future in-process HUD.
type Capture struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (c *Capture) Write(p []byte) (int, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.buf.Write(p)
}

func (c *Capture) String() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.buf.String()
}

// NewCapture returns a Logger writing synchronously into the returned Capture.
func NewCapture() (Logger, *Capture) {
	c := &Capture{}
	return newSlogLogger(c, slog.LevelInfo, "text"), c
}
