# Startup with dependencies, session control, and sessions surviving the TUI.
Output docs/assets/hero.gif

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
Show

Sleep 800ms
Type "lazyshell"
Sleep 400ms
Enter
Sleep 1s

# api waits for install and db, web waits for api.
Type "jj"
Sleep 6s
Type "j"
Sleep 1.5s
Type "j"
Sleep 2s

# A failing task blocks the one depending on it.
Type "j"
Type "j"
Sleep 800ms
Type "s"
Sleep 1.2s
Type "k"
Sleep 3s

# Restart the whole backend group.
Type@250ms "kkk"
Sleep 800ms
Type "R"
Sleep 6s

# Quit the TUI: sessions keep running.
Type "q"
Sleep 800ms
Type "lazyshell status"
Enter
Sleep 4s
