package runner

import (
	"io"
	"sync"

	"github.com/corruptmemory/ringer/internal/logging"
)

// collectorOp tags the closed set of commands the collector actor accepts.
type collectorOp uint8

const (
	opAppend collectorOp = iota
	opTail
)

// collectorCmd is the single typed message carried on the collector's command
// channel. Which fields are meaningful depends on op:
//   - opAppend: key, data
//   - opTail:   key, limit, reply
type collectorCmd struct {
	op    collectorOp
	key   string
	data  []byte      // opAppend
	limit int         // opTail
	reply chan string // opTail
}

// collector is the run-scoped owner of per-task worker output tails. Workers
// forward chunks via sink (fire-and-forget); readers call tail (request-reply).
// A single owner goroutine drains one buffered command channel carrying BOTH
// append and tail commands — one FIFO channel (not two) so a tail always sees
// every append that preceded it. Lifecycle mirrors the run's actor:
// recover-guarded stop (logging any recovered double-stop, never silent),
// WaitGroup wait, drain-then-exit.
type collector struct {
	runID      string
	capPerTask int
	cmds       chan collectorCmd
	quit       chan struct{}
	wg         sync.WaitGroup
	lg         logging.Logger
	tails      map[string]*taskTail // owned solely by run()
}

// taskTail is a bounded, chunk-granular FIFO of recent output for one task.
type taskTail struct {
	chunks [][]byte
	bytes  int
}

func newCollector(capPerTask int, runID string, lg logging.Logger) *collector {
	return &collector{
		runID:      runID,
		capPerTask: capPerTask,
		cmds:       make(chan collectorCmd, 256),
		quit:       make(chan struct{}),
		lg:         lg,
		tails:      map[string]*taskTail{},
	}
}

func (c *collector) start() {
	c.wg.Add(1)
	go func() {
		defer c.wg.Done()
		c.run()
	}()
}

// run is the actor loop: a single goroutine owns c.tails and processes typed
// commands off c.cmds until quit is closed, then drains any commands already
// buffered before exiting.
func (c *collector) run() {
	for {
		select {
		case cmd := <-c.cmds:
			c.handle(cmd)
		case <-c.quit:
			// drain-then-exit: absorb buffered commands so a worker's final
			// burst isn't lost, then exit.
			for {
				select {
				case cmd := <-c.cmds:
					c.handle(cmd)
				default:
					return
				}
			}
		}
	}
}

func (c *collector) handle(cmd collectorCmd) {
	switch cmd.op {
	case opAppend:
		c.append(cmd.key, cmd.data)
	case opTail:
		cmd.reply <- c.assembleTail(cmd.key, cmd.limit)
	}
}

func (c *collector) append(key string, data []byte) {
	t := c.tails[key]
	if t == nil {
		t = &taskTail{}
		c.tails[key] = t
	}
	t.chunks = append(t.chunks, data)
	t.bytes += len(data)
	for t.bytes > c.capPerTask && len(t.chunks) > 1 {
		t.bytes -= len(t.chunks[0])
		t.chunks[0] = nil // drop the reference so the evicted chunk's backing
		// array is collectible; slicing alone leaves a stale pointer to it
		// live in t.chunks' own backing array until the next growth.
		t.chunks = t.chunks[1:]
	}
}

// sink returns an io.Writer forwarding this task's output to the collector.
// It copies each write (the caller reuses the slice) and sends async.
func (c *collector) sink(key string) io.Writer { return &collectorSink{c: c, key: key} }

type collectorSink struct {
	c   *collector
	key string
}

func (s *collectorSink) Write(p []byte) (int, error) {
	b := make([]byte, len(p))
	copy(b, p)
	select {
	case s.c.cmds <- collectorCmd{op: opAppend, key: s.key, data: b}:
	case <-s.c.quit:
	}
	return len(p), nil
}

// tail returns up to limitBytes of the most recent output for key, in order.
// limitBytes <= 0 means "no output": it returns "" immediately without
// sending a command, so the owner goroutine (assembleTail) never has to
// handle a non-positive limit — a negative bound there would panic slicing.
func (c *collector) tail(key string, limitBytes int) string {
	if limitBytes <= 0 {
		return ""
	}
	reply := make(chan string, 1)
	select {
	case c.cmds <- collectorCmd{op: opTail, key: key, limit: limitBytes, reply: reply}:
		select {
		case s := <-reply:
			return s
		case <-c.quit:
			return ""
		}
	case <-c.quit:
		return ""
	}
}

func (c *collector) assembleTail(key string, limitBytes int) string {
	t := c.tails[key]
	if t == nil {
		return ""
	}
	var b []byte
	total := 0
	// Walk chunks newest-first until we have >= limitBytes, collecting indices.
	start := len(t.chunks)
	for i := len(t.chunks) - 1; i >= 0; i-- {
		start = i
		total += len(t.chunks[i])
		if total >= limitBytes {
			break
		}
	}
	for i := start; i < len(t.chunks); i++ {
		b = append(b, t.chunks[i]...)
	}
	if len(b) > limitBytes {
		b = b[len(b)-limitBytes:]
	}
	return string(b)
}

// stop is the shutdown trigger, mirroring actor.stop(): idempotent
// recover-guarded close. A recovered double-stop is a correct no-op but also
// evidence of a stray stop() caller, so it is logged (never swallowed),
// keyed by runID — see actor.stop() for the full rationale.
func (c *collector) stop() {
	defer func() {
		if r := recover(); r != nil {
			c.lg.Warnf("output collector %s: recovered panic in stop (double-stop?): %v", c.runID, r)
		}
	}()
	close(c.quit)
}
func (c *collector) wait()        { c.wg.Wait() }
func (c *collector) stopAndWait() { c.stop(); c.wait() }
