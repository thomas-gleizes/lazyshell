package tasks

import (
	"context"
	"sync/atomic"
	"testing"
	"time"
)

const testTimeout = 2 * time.Second

func TestNewTaskRuns(t *testing.T) {
	m := NewManager()

	ran := make(chan struct{})
	m.NewTask(func(context.Context) { close(ran) })

	select {
	case <-ran:
	case <-time.After(testTimeout):
		t.Fatal("task never ran")
	}
}

// Starting a new task must stop the previous one before returning — not
// "eventually", but synchronously, so a caller that immediately reads shared
// state after NewTask never races against the old task's goroutine.
func TestNewTaskStopsThePrevious(t *testing.T) {
	m := NewManager()

	stopped := make(chan struct{})
	m.NewTask(func(ctx context.Context) {
		<-ctx.Done()
		close(stopped)
	})

	m.NewTask(func(context.Context) {})

	select {
	case <-stopped:
	default:
		t.Fatal("the previous task was not stopped by the time NewTask returned")
	}
}

func TestNewTickerTaskCallsFRepeatedly(t *testing.T) {
	m := NewManager()

	var calls atomic.Int32
	m.NewTickerTask(5*time.Millisecond, func(context.Context) { calls.Add(1) })

	deadline := time.Now().Add(testTimeout)
	for time.Now().Before(deadline) {
		if calls.Load() >= 3 {
			break
		}

		time.Sleep(5 * time.Millisecond)
	}

	if got := calls.Load(); got < 3 {
		t.Fatalf("f was called %d times in %s, want at least 3", got, testTimeout)
	}
}

func TestStopStopsFurtherTicks(t *testing.T) {
	m := NewManager()

	var calls atomic.Int32
	m.NewTickerTask(5*time.Millisecond, func(context.Context) { calls.Add(1) })

	// Let it tick at least once before stopping.
	deadline := time.Now().Add(testTimeout)
	for time.Now().Before(deadline) && calls.Load() == 0 {
		time.Sleep(5 * time.Millisecond)
	}

	m.Stop()
	afterStop := calls.Load()

	time.Sleep(50 * time.Millisecond)

	if calls.Load() != afterStop {
		t.Errorf("f kept being called after Stop: %d calls before, %d after waiting", afterStop, calls.Load())
	}
}

func TestStopWithNoCurrentTaskDoesNotPanic(t *testing.T) {
	m := NewManager()
	m.Stop()
}
