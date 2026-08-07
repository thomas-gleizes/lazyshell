# Demo GIF for the README, covering phases 6-7: a declarative project file,
# direct index jump, zoom, and restarting a session that has exited.
#
# Regenerate with:
#   make demo
# (requires https://github.com/charmbracelet/vhs installed locally — it is
# not run in CI)

Output docs/demo.gif

Set Shell "bash"
Set FontSize 16
Set Width 1000
Set Height 620
Set Theme "Dracula"

Type "cd $(mktemp -d)"
Enter
Sleep 500ms

# A single-line printf rather than a heredoc: vhs's Type only takes one
# double-quoted string, and does not support a multi-line literal spanning
# several physical lines of the tape file — \n here is a literal two-character
# escape typed into the shell, decoded by printf itself, not by vhs.
Type@5ms "printf 'sessions:\n  - name: api\n    command: npm run dev\n  - name: build\n    command: echo build done && sleep 1 && exit 0\n  - name: shell\n' > lazyshell.yml"
Enter
Sleep 500ms

Type "lazyshell allow"
Enter
Sleep 500ms

Type "lazyshell"
Enter
Sleep 1s

# Jump straight to the "build" session, watch it run to completion.
Type "2"
Sleep 2s

# Zoom the output panel full-screen, then back out.
Type "z"
Sleep 1s
Type "z"
Sleep 500ms

# Restart the session that just exited, without retyping its command.
Type "R"
Sleep 1s

# Back to the first session by index, then quit.
Type "1"
Sleep 500ms
Type "q"
Sleep 500ms
