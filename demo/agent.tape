# vhs script: the `:` agent summarises a task and resolves it (visible on the board).
# Seed first: ./demo/seed.sh — render from the repo root: vhs demo/agent.tape
Output demo/agent.gif

Set Shell bash
Set FontSize 15
Set Width 1180
Set Height 700
Set Padding 16
Set TypingSpeed 55ms
Set Theme "Catppuccin Mocha"

Hide
Type 'export XDG_CONFIG_HOME=/tmp/deck-demo-xdg DSTASK_GIT_REPO=/tmp/deck-demo DECK_AGENT_CMD="$PWD/demo/fake-agent.sh"; clear; ~/.local/bin/deck'
Enter
Sleep 1.5s
Show

Sleep 1.2s
Down                  # select a TODAY task
Sleep 1s
Type ':'
Sleep 500ms
Type 'summarise the thread and close it'
Sleep 900ms
Enter                 # agent reads → prints a summary → asks
Sleep 4s
Type 'y'              # mark done — actually resolves it
Enter
Sleep 1.6s
Enter                 # return; board reloads — the task is gone from TODAY
Sleep 1.5s
Right@450ms 3         # over to DONE — there it is
Sleep 2.2s
Type 'q'
Sleep 500ms
