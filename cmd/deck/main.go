// deck — a kanban TUI over the dstask task store. Fully standalone: it reads and
// writes the store directly via the dstask library (github.com/naggie/dstask).
// Columns: TODAY (+now) · NEXT (actionable pool, P3 noise hidden) · WAITING · DONE (today).
// Built in: h/l/j/k move · H/L drag · J/K scroll detail · o open · d done · n ±today ·
// s start/stop · f focus (pomodoro) · u undo · a capture · N note · E edit · / filter · r/q.
// Hooks (enter/e/I/:), theme and columns are configured in ~/.config/deck/config.toml
// (or DECK_* env; the file supersedes). With no config it's a plain standalone board.
// See config.example.toml. --once dumps the view to stdout and exits.
//
// The code is split by concern: config.go (config) · task.go (view model + load) ·
// store.go (dstask writes) · hooks.go (integrations + async cmds) · model.go (state +
// layout) · update.go (key handling) · view.go (rendering).
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

	cfg = loadConfig()
	applyTheme()
	c, s, sp := load()
	m := model{cols: c, streak: s, spark: sp, off: make([]int, len(c)), dragFrom: -1}
	if *once {
		fmt.Println(m.View())
		return
	}
	opts := []tea.ProgramOption{tea.WithAltScreen()}
	if cfg.UI.Mouse { // wheel-scroll, click-select, drag-to-column
		opts = append(opts, tea.WithMouseCellMotion())
	}
	if _, err := tea.NewProgram(m, opts...).Run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
