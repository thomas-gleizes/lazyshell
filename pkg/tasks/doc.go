// Package tasks is a port of the pattern behind lazydocker's TaskManager: one
// task is one goroutine plus a cancelable context, and starting a new task
// stops the previous one.
//
// It only ever owns rendering/reading goroutines. Cancelling a task must never
// stop the underlying shell process, which keeps running in the background even
// when its session is not displayed.
package tasks
