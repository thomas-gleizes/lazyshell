package tasks

import (
	"context"
	"sync"
	"time"
)

// task is a single unit of cancelable background work.
type task struct {
	cancel context.CancelFunc
	done   chan struct{}
}

// stop cancels the task and blocks until its function has returned.
func (t *task) stop() {
	t.cancel()
	<-t.done
}

// Manager runs at most one task at a time: starting a new one stops whichever
// was running first. It only ever owns display/reading goroutines — never
// anything that should keep running once nobody is watching it (that is what
// pkg/session's own drain goroutines are for; cancelling a task here must
// never reach into a session's process).
type Manager struct {
	mu      sync.Mutex
	current *task
}

// NewManager returns an empty Manager, ready to run tasks.
func NewManager() *Manager {
	return &Manager{}
}

// NewTask stops the currently running task, if any, then starts f in its own
// goroutine with a fresh cancelable context.
func (m *Manager) NewTask(f func(ctx context.Context)) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.current != nil {
		m.current.stop()
	}

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	m.current = &task{cancel: cancel, done: done}

	go func() {
		defer close(done)
		f(ctx)
	}()
}

// NewTickerTask is NewTask wrapping f in a loop that also runs on every tick
// of interval, until stopped. f runs once immediately, then again on each
// tick — the same "run once, then on ticks" shape as gui.goEvery.
func (m *Manager) NewTickerTask(interval time.Duration, f func(ctx context.Context)) {
	m.NewTask(func(ctx context.Context) {
		f(ctx)

		ticker := time.NewTicker(interval)
		defer ticker.Stop()

		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				f(ctx)
			}
		}
	})
}

// Stop stops the currently running task, if any.
func (m *Manager) Stop() {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.current != nil {
		m.current.stop()
		m.current = nil
	}
}
