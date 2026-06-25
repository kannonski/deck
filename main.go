// deck — a kanban TUI over the dstask task store. Fully standalone: it reads and
// writes the store directly via the dstask library (github.com/naggie/dstask).
// Columns: TODAY (+now) · NEXT (actionable pool, P3 noise hidden) · WAITING · DONE (today).
// Built in: h/l/j/k move · H/L drag · J/K scroll detail · o open · d done · n ±today ·
// s start/stop · f focus (pomodoro) · u undo · a capture · N note · E edit · / filter · r/q.
// Optional external hooks, enabled only when the env var is set (else the key hides):
//
//	DECK_OPEN_CMD <url>          → enter   ·  DECK_ENRICH_CMD <id> → e
//	DECK_INGEST_CMD              → I       ·  DECK_CARD_DIR        → detail-pane card
//	DECK_AGENT_CMD <id> <instr>  → :       (act on the task: draft / comment, foreground)
//
// --once dumps the view to stdout and exits.
//
// The code is split by concern: task.go (view model + load) · store.go (dstask writes) ·
// hooks.go (env integrations + async cmds) · model.go (state + layout) · update.go
// (key handling) · view.go (rendering).
package main

import (
	"flag"
	"fmt"
	"os"

	tea "github.com/charmbracelet/bubbletea"
)

func main() {
	once := flag.Bool("once", false, "render the kanban once to stdout and exit")
	flag.Parse()

	c, s := load()
	m := model{cols: c, streak: s, off: make([]int, len(c))}
	if *once {
		fmt.Println(m.View())
		return
	}
	if _, err := tea.NewProgram(m, tea.WithAltScreen()).Run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
