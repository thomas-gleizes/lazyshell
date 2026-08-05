package gui

import (
	"context"
	"fmt"

	"github.com/jesseduffield/gocui"

	"github.com/thomas-gleizes/lazyshell/pkg/session"
)

// showOutput starts a task that renders sess's screen into the output view on
// every tick, until superseded by another call (a different session gets
// selected) or the application quits. The task only owns this rendering
// loop: cancelling it never touches sess's process or its drain goroutine —
// those belong to pkg/session and keep running regardless of what is
// currently displayed, which is what guarantees no output is ever lost while
// a session is hidden.
func (gui *Gui) showOutput(sess *session.Session) {
	gui.outputTasks.NewTickerTask(reRenderInterval, func(context.Context) {
		content := sess.Screen().Render()

		gui.g.Update(func(g *gocui.Gui) error {
			view, err := g.View(outputViewName)
			if err != nil {
				return nil
			}

			view.Clear()
			fmt.Fprint(view, content)

			return nil
		})
	})
}
