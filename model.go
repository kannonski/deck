package main

import (
	"strings"
	"time"
)

type model struct {
	cols      []column
	w, h      int
	col, card int       // cursor: column index + card index
	off       []int     // per-column scroll offset (top visible card)
	detailOff int       // scroll offset within the detail pane (J/K)
	mode      string    // "" nav · "add" capture · "filter" · "note" · "agent"
	input     string    // text buffer while in an input mode
	filter    string    // active filter (area/state/summary substring); "" = all
	ingesting bool      // a background `I` ingest is in flight
	streak    int       // consecutive days with a resolved task (computed on load)
	focusing  bool      // a focus countdown is running
	focusID   int       // the task being focused
	focusEnds time.Time // when the current focus block ends
	focusGen  int       // bumped on (re)start/stop to invalidate stale ticks
	status    string    // last-action feedback, shown in the footer
}

// clampi keeps v inside [0, n-1]; returns 0 when the column is empty.
func clampi(v, n int) int {
	switch {
	case n <= 0:
		return 0
	case v >= n:
		return n - 1
	case v < 0:
		return 0
	}
	return v
}

// shown = the cards visible in column ci after the active filter.
func (m model) shown(ci int) []task {
	if ci < 0 || ci >= len(m.cols) {
		return nil
	}
	cs := m.cols[ci].cards
	if m.filter == "" {
		return cs
	}
	f := strings.ToLower(m.filter)
	out := make([]task, 0, len(cs))
	for _, t := range cs {
		if strings.Contains(strings.ToLower(t.Project+" "+t.state()+" "+t.Summary), f) {
			out = append(out, t)
		}
	}
	return out
}

// visN = navigable cards in the current column (after filtering).
func (m model) visN() int { return len(m.shown(m.col)) }

// layout splits the terminal height into FIXED column + detail-pane content heights
// (the border adds 2 to each box) plus cards-per-column. Fixed sizes mean the total
// height is constant as you navigate — the detail pane never grows into the columns,
// so there's no jump/flicker. Returns content heights and the visible card count.
func (m model) layout() (colH, detailH, vis int) {
	h := m.h
	if h <= 0 {
		h = 40
	}
	const footer = 4                  // stats line + help + up to two of filter/ingesting/status
	detail := max(h*2/5, 10)          // a big detail pane: ~40% of the screen, ≥10 rows
	detail = min(detail, h-footer-10) // always leave ≥10 rows for the board
	detail = max(detail, 5)
	colH = max((h-footer-detail)-2, 5) // -2 for the column box border
	detailH = max(detail-2, 3)         // -2 for the detail box border
	vis = max((colH-4)/2, 3)           // title + blank + two ↑/↓ indicator lines
	return
}

func (m model) visible() int { _, _, v := m.layout(); return v }

// scrolled keeps the cursor inside the visible window of its column.
func (m model) scrolled() model {
	m.detailOff = 0 // a selection change (or reload) resets detail-pane scroll
	if m.col < 0 || m.col >= len(m.off) {
		return m
	}
	v := m.visible()
	if m.card < m.off[m.col] {
		m.off[m.col] = m.card
	}
	if m.card >= m.off[m.col]+v {
		m.off[m.col] = m.card - v + 1
	}
	if m.off[m.col] < 0 {
		m.off[m.col] = 0
	}
	return m
}

// selected returns the task under the cursor (false if the column is empty).
func (m model) selected() (task, bool) {
	cs := m.shown(m.col)
	if m.card < 0 || m.card >= len(cs) {
		return task{}, false
	}
	return cs[m.card], true
}

// reloaded re-reads dstask and clamps the cursor + offsets back into range.
func (m model) reloaded() model {
	m.cols, m.streak = load()
	if len(m.off) != len(m.cols) {
		m.off = make([]int, len(m.cols))
	}
	for i := range m.off {
		if m.off[i] < 0 || m.off[i] > len(m.cols[i].cards) {
			m.off[i] = 0
		}
	}
	m.col = clampi(m.col, len(m.cols))
	m.card = clampi(m.card, m.visN())
	return m.scrolled()
}

// act reloads + reports success, or surfaces the error in the status line.
func (m model) act(err error, ok string) model {
	if err != nil {
		m.status = "⚠ " + err.Error()
		return m
	}
	m = m.reloaded()
	m.status = ok
	return m
}
