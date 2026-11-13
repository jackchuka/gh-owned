# Regenerate with: vhs assets/demo.tape (from the repository root)
# Runs fully offline: assets/demo runs the real CLI on a fabricated "acme" org.

Output assets/demo.gif

Set Shell "bash"
Set FontSize 16
Set Width 1400
Set Height 400
Set Padding 24
Set Theme "Catppuccin Mocha"
Set TypingSpeed 45ms

Hide
Type `export D=/tmp/gh-owned-demo && rm -rf "$D" && mkdir -p "$D" && go build -o "$D/demo" ./assets/demo && gh() { shift; "$D/demo" "$@"; } && cd "$D" && export PS1='\[\e[1;35m\]$ \[\e[0m\]' && clear`
Enter
Sleep 5s
Show

Type "gh owned --org acme"
Sleep 400ms
Enter
Sleep 5s

Type "clear"
Enter
Type "gh owned --org acme --ignore-team @acme/everyone"
Sleep 400ms
Enter
Sleep 5s

Type "clear"
Enter
Type "gh owned --org acme --ignore-team @acme/everyone --format gh-dash"
Sleep 400ms
Enter
Sleep 5s
