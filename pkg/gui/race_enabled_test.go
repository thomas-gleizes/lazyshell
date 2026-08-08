//go:build race

package gui

// raceEnabled is true when the test binary was built with -race. Go has no
// standard way to ask, so it is answered by this pair of build-tagged files.
//
// It exists for skipMainLoopUnderRace — see race_disabled_test.go for the whole
// story.
const raceEnabled = true
