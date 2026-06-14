# Demo tape for kage. Rendered with ascii-gif (github.com/tamnd/ascii-gif),
# which supplies the window chrome and theme; this file is just the action.
#
#   ascii-gif render docs/demo/kage.tape -o docs/static/demo.gif
#
# kage must be on PATH inside the recording shell, and Chrome available.

Hide
Type "export PS1='$ ' PATH=/tmp/kagebin:$PATH && cd $(mktemp -d) && clear"
Enter
Show

Sleep 600ms
Type "kage clone paulgraham.com --max-pages 3 --out ."
Sleep 700ms
Enter
Sleep 9s

Type "kage pack ./paulgraham.com"
Sleep 700ms
Enter
Sleep 2.5s

Type "kage pack ./paulgraham.com --format binary -o paulgraham"
Sleep 700ms
Enter
Sleep 3s

Type "kage open paulgraham.com.zim --open=false"
Sleep 700ms
Enter
Sleep 3s
Ctrl+C
Sleep 1.2s
