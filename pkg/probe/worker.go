package probe

import (
	"sync"

	"github.com/Qendolin/mod-bisect-tool/pkg/logging"
)

// Worker serializes directory probes: at most one probe runs at a time. While a
// probe is in flight, further requests for different paths replace the single
// pending slot, so only the most recent request is processed after the current
// one finishes. Requests for paths that are not valid directories are ignored.
type Worker struct {
	mu      sync.Mutex
	running bool
	pending *request
}

type request struct {
	path string
	done func(ProbeResult)
}

// NewWorker creates a Worker.
func NewWorker() *Worker {
	return &Worker{}
}

// Request queues a probe for the given path. If the path is not a valid
// directory, the request is dropped and done is never called. Otherwise done is
// invoked on a worker goroutine with the probe result. Request never blocks the
// caller, and a queued request may be superseded by a more recent one.
func (w *Worker) Request(path string, done func(ProbeResult)) {
	if !IsValidDir(path) {
		return
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.running {
		w.pending = &request{path: path, done: done}
		return
	}
	w.running = true
	go w.process(request{path: path, done: done})
}

// process runs a single probe and then starts the latest pending request, if
// any.
func (w *Worker) process(req request) {
	defer logging.HandlePanic()
	req.done(ProbeModsDirectory(req.path))

	w.mu.Lock()
	defer w.mu.Unlock()
	if w.pending != nil {
		next := *w.pending
		w.pending = nil
		go w.process(next)
	} else {
		w.running = false
	}
}
