# Reading the output: scrollback, search, fullscreen.
Output docs/assets/logs.gif

Set Shell bash
Set FontSize 15
Set Width 1200
Set Height 680
Set Padding 12
Set Theme "Catppuccin Mocha"
Set Framerate 15
Set TypingSpeed 80ms

Hide
Type `cd "$DEMO_ROOT/acme-shop" && clear`
Enter
Sleep 500ms
Type "lazyshell"
Enter
Sleep 1s
Type "jj"
Sleep 1s
Show

Sleep 2s
# Search the history, then walk through the matches.
Type "/"
Sleep 600ms
Type@150ms "ERROR"
Sleep 600ms
Enter
Sleep 1.5s
Type "n"
Sleep 1.5s
Type "N"
Sleep 1.5s
Escape
Sleep 500ms
Type "G"
Sleep 1.5s

# Fullscreen output.
Type "f"
Sleep 3s
Escape
Sleep 2s

# Key reference.
Type "?"
Sleep 4s
Escape
Sleep 1s
