// Command lazyshell is a TUI shell session manager.
package main

import (
	"fmt"
	"os"

	"github.com/thomas-gleizes/lazyshell/pkg/app"
)

func main() {
	a := app.New()

	// Run returns only once the terminal has been restored, so it is safe to
	// write to stderr here.
	if err := a.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "lazyshell: %v\n", err)
		os.Exit(1)
	}
}
