package runner

import (
	"sync"

	"github.com/corruptmemory/ringer/internal/logging"
	"github.com/corruptmemory/ringer/internal/state"
)

// actorOp tags the closed set of operations the actor serializes. The command
// channel is TYPED (not chan func()) so run() reads as the state machine and
// callers cannot inject arbitrary work.
type actorOp uint8

const (
	opSetStatus actorOp = iota
	opSetResult
	opSnapshot
)

type actorCmd struct {
	op                actorOp
	key               string
	status            string
	attempt           int
	tokens            int64
	verified, logPath string
	ts                string              // RFC3339; opSetStatus/opSetResult only
	deliverables      []state.Deliverable // opSetResult only
	checkTail         string              // opSetResult only
	notes             []string            // opSetResult only
	reply             chan state.RunState // opSnapshot only
}

type actor struct {
	runID, runName, identity string
	keys                     []string
	cmds                     chan actorCmd
	wg                       sync.WaitGroup // wait() blocks on this — N callers ok
	lg                       logging.Logger
	tasks                    map[string]*state.TaskView
}

// newActor seeds each key's TaskView with its effective engine/model, known
// (via runner.resolveTaskEngine) before any attempt runs — engine/model are
// static per task for the life of the run, so they're set once here rather
// than threaded through every setStatus/setResult call. engineByKey/
// modelByKey may omit a key (or newActor may be called with nil maps in
// tests that don't care), in which case that TaskView's Engine/Model stays
// the zero value "".
func newActor(runID, runName, identity string, keys []string, engineByKey, modelByKey map[string]string, lg logging.Logger) *actor {
	tasks := make(map[string]*state.TaskView, len(keys))
	for _, k := range keys {
		tasks[k] = &state.TaskView{Key: k, Engine: engineByKey[k], Model: modelByKey[k], Status: "pending"}
	}
	return &actor{
		runID: runID, runName: runName, identity: identity, keys: keys,
		cmds: make(chan actorCmd), lg: lg, tasks: tasks,
	}
}

func (a *actor) start() {
	a.wg.Add(1)
	go a.run()
}

// run is the single owner of the task map. It applies each typed command in
// receive order; because the op set is closed, all mutation logic lives here in
// one readable switch — the state machine, not an executor of arbitrary funcs.
func (a *actor) run() {
	defer a.wg.Done()
	for c := range a.cmds { // drain accepted commands, then exit on close
		switch c.op {
		case opSetStatus:
			if tv := a.tasks[c.key]; tv != nil {
				tv.Status = c.status
				tv.Attempt = c.attempt
				if tv.StartedAt == "" { // first transition to running only; a retry's 2nd setStatus must not overwrite it
					tv.StartedAt = c.ts
				}
				if c.logPath != "" {
					tv.LogPath = c.logPath // make the live worker log reachable WHILE the task runs, not only after it finishes
				}
			}
		case opSetResult:
			if tv := a.tasks[c.key]; tv != nil {
				tv.Status = c.status
				tv.Tokens = c.tokens
				tv.Verified = c.verified
				tv.LogPath = c.logPath
				tv.EndedAt = c.ts
				tv.Deliverables = c.deliverables
				tv.CheckTail = c.checkTail
				tv.DeliverableNotes = c.notes
			}
		case opSnapshot:
			out := state.RunState{RunID: a.runID, RunName: a.runName, Identity: a.identity}
			for _, k := range a.keys {
				out.Tasks = append(out.Tasks, *a.tasks[k])
			}
			c.reply <- out
		}
	}
}

// stop is the shutdown trigger: it closes cmds (drain-then-exit) and returns
// immediately — it does NOT wait. It is idempotent: a second or concurrent
// stop re-closes cmds, panics, and is recovered. A recovered double-stop is a
// correct no-op but also evidence of a stray stop() caller, so it is logged
// (never swallowed), keyed by runID. Add debug.Stack() to the log line when
// hunting the wayward caller. Producers must have quiesced before stop() — the
// setStatus/setResult/snapshot sends rendezvous with run()'s receive, so once
// every caller has returned there are no in-flight sends to panic on the closed
// channel, and run() finishes its current command before range exits.
func (a *actor) stop() {
	defer func() {
		if r := recover(); r != nil {
			a.lg.Warnf("runner actor %s: recovered panic in stop (double stop?): %v", a.runID, r)
		}
	}()
	close(a.cmds)
}

// wait blocks until the actor goroutine has exited. Safe for any number of
// callers (it is a sync.WaitGroup).
func (a *actor) wait() { a.wg.Wait() }

// stopAndWait is the named convenience for "stop, then block until exited" —
// the blocking wait is visible at the call site, never hidden inside stop().
func (a *actor) stopAndWait() { a.stop(); a.wait() }

// setStatus and setResult are fire-and-forget: the send rendezvous with run()'s
// receive (so ordering is preserved), but the caller does not block until the
// mutation is applied — it has no need to.
func (a *actor) setStatus(key, status string, attempt int, logPath, ts string) {
	a.cmds <- actorCmd{op: opSetStatus, key: key, status: status, attempt: attempt, logPath: logPath, ts: ts}
}

func (a *actor) setResult(key, status string, tokens int64, verified, logPath, ts string, deliverables []state.Deliverable, checkTail string, notes []string) {
	a.cmds <- actorCmd{op: opSetResult, key: key, status: status, tokens: tokens, verified: verified, logPath: logPath, ts: ts, deliverables: deliverables, checkTail: checkTail, notes: notes}
}

// snapshot is request-reply: it sends its own reply channel and blocks for the
// consistent point-in-time copy that run() assembles and sends back.
func (a *actor) snapshot() state.RunState {
	reply := make(chan state.RunState, 1)
	a.cmds <- actorCmd{op: opSnapshot, reply: reply}
	return <-reply
}
