//go:build !race

package gui

// raceEnabled is false when the test binary was built without -race. See
// race_enabled_test.go for the other half, and wait_test.go's
// skipMainLoopUnderRace for what it is for.
const raceEnabled = false
