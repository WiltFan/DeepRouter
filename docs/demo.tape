Output deeprouter-setup-v0.3.7.gif
Set Shell bash
Set FontSize 16
Set Width 1100
Set Height 700
Set Theme "Dracula"
Set Padding 20
Set TypingSpeed 45ms
Hide
Type "exec env -i HOME=/tmp/demo PATH=/usr/bin:/bin TERM=xterm-256color LANG=zh_CN.UTF-8 LC_ALL=zh_CN.UTF-8 DEEPROUTER_API_KEY=$DEEPROUTER_API_KEY bash --norc --noprofile" Enter
Sleep 500ms
Type "mkdir -p ~; cd ~; export PS1='\[\e[32m\]dev@box\[\e[0m\]:\[\e[34m\]~\[\e[0m\]$ '; clear" Enter
Sleep 500ms
Show
Type "curl -fsSL https://deeprouter.top/install.sh | sh"
Sleep 500ms
Enter
Sleep 7s
Sleep 1500ms
Enter
Sleep 1s
Sleep 1500ms
Enter
Sleep 1s
Sleep 2500ms
Enter
Sleep 3s
Enter
Sleep 2500ms
Enter
Set WaitTimeout 40s
Sleep 2s
Wait+Line /Y.n/
Sleep 2s
Type "n"
Sleep 300ms
Enter
Sleep 2500ms
