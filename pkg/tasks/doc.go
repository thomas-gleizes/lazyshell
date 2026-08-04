// Package tasks will hold a port of lazydocker's TaskManager: one task is one
// goroutine plus a cancelable context, and starting a new task stops the
// previous one.
//
// It only ever owns rendering/reading goroutines. Cancelling a task must never
// stop the underlying shell process, which keeps running in the background even
// when its session is not displayed.
//
// Implemented in phase 3 of ROADMAP.md.
package tasks
