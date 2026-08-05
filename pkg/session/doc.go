// Package session owns the lifecycle of the shell processes: one pty per
// session, a *screen.Screen (pkg/screen) rendering it and providing bounded
// scrollback, a drain goroutine per session and a Manager exposing
// New/Kill/List/Get.
//
// It is deliberately independent from pkg/tasks: the TaskManager only owns
// display goroutines, never the shell processes themselves.
//
// Implemented in phase 2 of ROADMAP.md.
package session
