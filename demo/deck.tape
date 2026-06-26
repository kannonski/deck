# vhs script for the deck demo gif. Seed first: ./demo/seed.sh
# Render: vhs demo/deck.tape   →   demo/demo.gif
Output demo/demo.gif

Set Shell bash
Set FontSize 15
Set Width 1180
Set Height 700
Set Padding 16
Set TypingSpeed 55ms
Set Theme "Catppuccin Mocha"

# launch deck against the throwaway demo store (hidden from the recording)
Hide
Type 'export XDG_CONFIG_HOME=/tmp/deck-demo-xdg DSTASK_GIT_REPO=/tmp/deck-demo; clear; ~/.local/bin/deck'
Enter
Sleep 1.5s
Show

Sleep 1.2s
Down                       # move within TODAY
Sleep 1s
Right                      # into NEXT
Sleep 900ms
Down 3                     # browse the pool — detail pane follows
Sleep 1.5s
Left                       # back to TODAY (a card with a detail card)
Sleep 1.4s
Type '/'                   # live filter
Sleep 500ms
Type 'deep'
Sleep 1.6s
Escape
Sleep 800ms
Type 'a'                   # capture a task
Sleep 400ms
Type 'record the demo gif +quick'
Sleep 700ms
Enter
Sleep 1.4s
Type 'N'                   # jot a note on the selected card
Sleep 400ms
Type 'keep it under ten seconds'
Sleep 700ms
Enter
Sleep 1.8s
Type 'q'
Sleep 500ms
