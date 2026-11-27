package platform

import (
	"context"
	"io"
	"strconv"
	"sync"
	"time"

	"github.com/rendimiento-ai/rendimiento/internal/events"
	"github.com/rendimiento-ai/rendimiento/internal/pipeline"
)

// StepUpdate implements pipeline.Recorder: persist, then notify the UI.
func (p *Platform) StepUpdate(runID, stepID string, r pipeline.StepResult) {
	id, _ := strconv.ParseInt(runID, 10, 64)
	started, finished, err := p.Store.UpdateStepTimes(context.Background(), id, stepID, r)
	if err != nil {
		p.Log.Error("update step", "run", runID, "step", stepID, "err", err)
		started, finished = timeOrNil(r.Started), timeOrNil(r.Finished)
	}
	p.Hub.Publish(RunTopic(id), events.Event{Type: "step", Data: stepEvent(stepID, r, started, finished)})
}

// stepEvent is what the run's page is told of a step that changed: its
// status and, with the names the API's steps use, when it started and
// finished as stored, so a bar stops where the step did without a reload.
// A time not known yet is left out.
func stepEvent(stepID string, r pipeline.StepResult, started, finished *time.Time) map[string]any {
	e := map[string]any{"id": stepID, "status": r.Status, "digest": r.Digest, "message": r.Message}
	if started != nil {
		e["startedAt"] = started.UTC()
	}
	if finished != nil {
		e["finishedAt"] = finished.UTC()
	}
	if len(r.Lugares) > 0 {
		e["lugares"] = r.Lugares
	}
	return e
}

func timeOrNil(t time.Time) *time.Time {
	if t.IsZero() {
		return nil
	}
	return &t
}

// StepLog implements pipeline.Recorder. Lines reach the UI immediately;
// the database is written in batches to keep Postgres load low.
func (p *Platform) StepLog(runID, stepID string) io.WriteCloser {
	id, _ := strconv.ParseInt(runID, 10, 64)
	w := &logWriter{p: p, run: id, step: stepID, done: make(chan struct{}), stopped: make(chan struct{})}
	go w.flushLoop()
	return w
}

type logWriter struct {
	p    *Platform
	run  int64
	step string

	mu      sync.Mutex
	buf     []byte
	done    chan struct{}
	stopped chan struct{}
	closed  bool
}

// Write buffers step output and flushes it to the store and live subscribers.
func (w *logWriter) Write(b []byte) (int, error) {
	w.p.Hub.Publish(RunTopic(w.run), events.Event{Type: "log", Data: map[string]string{"step": w.step, "text": string(b)}})
	w.mu.Lock()
	w.buf = append(w.buf, b...)
	full := len(w.buf) >= 16<<10
	w.mu.Unlock()
	if full {
		w.flush()
	}
	return len(b), nil
}

// flush appends what was written since the last flush to the step's log
// in the database.
func (w *logWriter) flush() {
	w.mu.Lock()
	chunk := w.buf
	w.buf = nil
	w.mu.Unlock()
	if len(chunk) == 0 {
		return
	}
	if err := w.p.Store.AppendLog(context.Background(), w.run, w.step, string(chunk)); err != nil {
		w.p.Log.Warn("append log", "run", w.run, "step", w.step, "err", err)
	}
}

// flushLoop flushes every second until the step ends.
func (w *logWriter) flushLoop() {
	t := time.NewTicker(time.Second)
	defer t.Stop()
	defer close(w.stopped)
	for {
		select {
		case <-t.C:
			w.flush()
		case <-w.done:
			w.flush()
			return
		}
	}
}

// Close flushes what is left.
func (w *logWriter) Close() error {
	w.mu.Lock()
	if w.closed {
		w.mu.Unlock()
		return nil
	}
	w.closed = true
	w.mu.Unlock()
	close(w.done)
	<-w.stopped
	return nil
}
