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

Type@10ms `cat > lazyshell.yml <<'EOF'
sessions:
  - name: api
    command: npm run dev
  - name: build
    command: 'echo build done && sleep 1 && exit 0'
  - name: shell
EOF`
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
