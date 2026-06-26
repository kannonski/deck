package main

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
	dstask "github.com/naggie/dstask"
)

const maxCards = 12

var (
	idStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("117"))
	dimStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("240"))
	helpStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("240"))
	selStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("212")).Bold(true)
)

func areaColor(p string) lipgloss.Color {
	if c := cfg.Theme.Area[p]; c != "" {
		return lipgloss.Color(c)
	}
	return lipgloss.Color(cfg.Theme.Area["default"])
}

func trunc(s string, n int) string {
	r := []rune(s)
	if n < 1 {
		n = 1
	}
	if len(r) <= n {
		return s
	}
	return string(r[:n-1]) + "…"
}

// detailLines builds the (already styled, width-truncated so nothing wraps) content
// lines of the detail pane for the selected task.
func (m model) detailLines(iw int) []string {
	t, ok := m.selected()
	if !ok {
		return []string{dimStyle.Render("no card selected")}
	}
	meta := fmt.Sprintf("#%d · %s", t.ID, t.Project)
	if s := t.state(); s != "" {
		meta += " · " + s
	}
	if t.Status == dstask.STATUS_ACTIVE {
		meta += " · ▶ active"
	}
	if t.Due != "" {
		meta += " · due " + t.Due
	}
	lines := []string{
		lipgloss.NewStyle().Foreground(areaColor(t.Project)).Bold(true).Render(trunc(t.Summary, iw)),
		dimStyle.Render(trunc(meta, iw)),
	}
	url, notes := splitNote(t.Notes)
	if url != "" {
		lines = append(lines, dimStyle.Render(trunc("↗ "+url, iw)))
	}
	if notes != "" {
		lines = append(lines, "", selStyle.Render(fmt.Sprintf("📝 notes (%d)", strings.Count(notes, "\n")+1)))
		for n := range strings.SplitSeq(notes, "\n") {
			lines = append(lines, trunc(n, iw))
		}
	}
	if card := cardText(t); card != "" {
		lines = append(lines, "")
		for c := range strings.SplitSeq(card, "\n") {
			lines = append(lines, dimStyle.Render(trunc(c, iw)))
		}
	}
	return lines
}

func detailInnerWidth(w int) int {
	iw := 74
	if w > 6 {
		iw = w - 6
	}
	if iw < 10 {
		iw = 10
	}
	return iw
}

// detailView is the bottom pane, rendered at a FIXED height (no flicker on navigate),
// windowed by m.detailOff so long notes/cards can be scrolled with J/K.
func (m model) detailView() string {
	_, detailH, _ := m.layout()
	dw := 76
	if m.w > 6 {
		dw = m.w - 4
	}
	box := lipgloss.NewStyle().Width(dw).Height(detailH).
		Border(lipgloss.RoundedBorder()).BorderForeground(lipgloss.Color("240")).Padding(0, 1)
	lines := m.detailLines(detailInnerWidth(m.w))
	off := max(min(m.detailOff, len(lines)-detailH), 0)
	view := lines[off:]
	if len(view) > detailH {
		view = view[:detailH]
		view[detailH-1] = dimStyle.Render("  ↓ more (J)")
	}
	if off > 0 {
		view[0] = dimStyle.Render("  ↑ more (K)")
	}
	return box.Render(strings.Join(view, "\n"))
}

// detailMaxOff is the furthest the detail pane can scroll for the current selection.
func (m model) detailMaxOff() int {
	_, detailH, _ := m.layout()
	return max(len(m.detailLines(detailInnerWidth(m.w)))-detailH, 0)
}

func (m model) View() string {
	n := len(m.cols)
	cw := 30
	if m.w > 0 {
		cw = max(m.w/n-2, 16)
	}
	colH, _, vis := m.layout()
	boxes := make([]string, 0, n)
	for ci, c := range m.cols {
		active := ci == m.col
		cards := m.shown(ci)
		ttl := fmt.Sprintf("%s  %d", c.title, len(cards))
		if active {
			ttl = "▸ " + ttl
		}
		lines := []string{lipgloss.NewStyle().Foreground(c.accent).Bold(true).Render(ttl), ""}
		off := 0
		if ci < len(m.off) {
			off = m.off[ci]
		}
		if off > len(cards) {
			off = 0
		}
		if off > 0 {
			lines = append(lines, dimStyle.Render(fmt.Sprintf("  ↑ %d more", off)))
		}
		end := min(off+vis, len(cards))
		for i := off; i < end; i++ {
			t := cards[i]
			meta := t.Project
			if s := t.state(); s != "" {
				meta += " · " + s
			}
			if !strings.HasPrefix(t.Due, "0001") && t.Due != "" {
				meta += " · " + t.Due[:10]
			}
			metaSt := dimStyle
			if t.Status == dstask.STATUS_ACTIVE {
				meta = "▶ active · " + meta
				metaSt = lipgloss.NewStyle().Foreground(lipgloss.Color(cfg.Theme.Active))
			}
			sumSt := lipgloss.NewStyle().Foreground(areaColor(t.Project))
			marker := "  "
			if active && i == m.card {
				sumSt = sumSt.Bold(true)
				marker = selStyle.Render("▸ ")
			}
			lines = append(lines,
				marker+idStyle.Render(fmt.Sprintf("%d", t.ID))+" "+sumSt.Render(trunc(t.Summary, cw-8)),
				metaSt.Render("    "+trunc(meta, cw-7)))
		}
		if end < len(cards) {
			lines = append(lines, dimStyle.Render(fmt.Sprintf("  ↓ %d more", len(cards)-end)))
		}
		if len(lines) > colH { // safety: never exceed the fixed column height
			lines = lines[:colH]
		}
		border := c.accent
		if !active {
			border = lipgloss.Color("238")
		}
		box := lipgloss.NewStyle().
			Width(cw).Height(colH).Border(lipgloss.RoundedBorder()).BorderForeground(border).Padding(0, 1).
			Render(strings.Join(lines, "\n"))
		boxes = append(boxes, box)
	}
	board := lipgloss.JoinHorizontal(lipgloss.Top, boxes...)
	var foot string
	switch m.mode {
	case "add":
		foot = selStyle.Render("  add ▸ ") + m.input + "▌  " + helpStyle.Render("enter add · esc cancel")
	case "filter":
		foot = selStyle.Render("  filter ▸ ") + m.input + "▌  " + helpStyle.Render("enter apply · esc clear")
	case "note":
		foot = selStyle.Render("  note ▸ ") + m.input + "▌  " + helpStyle.Render("enter save · esc cancel")
	case "agent":
		foot = selStyle.Render("  agent ▸ ") + m.input + "▌  " + helpStyle.Render("enter run · esc cancel")
	default:
		hints := []string{"h/l/j/k move", "H/L drag"}
		if cfg.Hooks.Open != "" {
			hints = append(hints, "↵ work")
		}
		hints = append(hints, "a add", "N note", "E edit", "/ filter", "o open", "f focus", "d done", "n today", "s start/stop", "u undo")
		if cfg.Hooks.Agent != "" {
			hints = append(hints, ": agent")
		}
		if cfg.Hooks.Enrich != "" {
			hints = append(hints, "e card")
		}
		if cfg.Hooks.Ingest != "" {
			hints = append(hints, "I ingest")
		}
		hints = append(hints, "r reload", "q quit")
		foot = helpStyle.Render("  " + strings.Join(hints, " · "))
		if m.filter != "" {
			foot = selStyle.Render("  ⦿ filter: "+m.filter) + "\n" + foot
		}
		if m.ingesting {
			foot = selStyle.Render("  📥 ingesting mail + GitLab…") + "\n" + foot
		}
		if m.status != "" {
			foot += "\n  " + selStyle.Render(m.status)
		}
		if m.focusing {
			rem := max(time.Until(m.focusEnds), 0)
			foot = lipgloss.NewStyle().Foreground(lipgloss.Color("213")).
				Render(fmt.Sprintf("  ⏳ focus #%d · %02d:%02d", m.focusID, int(rem.Minutes()), int(rem.Seconds())%60)) + "\n" + foot
		}
		// dopamine line on top: what you've closed + the streak
		done := 0
		if len(m.cols) > 3 {
			done = len(m.cols[3].cards)
		}
		stats := lipgloss.NewStyle().Foreground(lipgloss.Color("120")).Render(fmt.Sprintf("  ✓ %d today", done))
		if m.streak > 0 {
			stats += lipgloss.NewStyle().Foreground(lipgloss.Color("214")).Render(fmt.Sprintf("  ·  🔥 %d", m.streak))
		}
		foot = stats + "\n" + foot
	}
	out := board + "\n" + m.detailView() + "\n" + foot
	if m.h > 0 { // never emit more lines than the terminal — alt-screen garbles on overflow
		if lines := strings.Split(out, "\n"); len(lines) > m.h {
			out = strings.Join(lines[:m.h], "\n")
		}
	}
	return out
}
