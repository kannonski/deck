package main

import (
	"fmt"
	"os/exec"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	dstask "github.com/naggie/dstask"
)

func (m model) Init() tea.Cmd { return nil }

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.w, m.h = msg.Width, msg.Height
	case enrichedMsg:
		m = m.reloaded()
		if msg.err {
			m.status = fmt.Sprintf("⚠ could not describe #%d", msg.id)
		} else {
			m.status = fmt.Sprintf("✎ card ready for #%d", msg.id)
		}
	case openedMsg:
		m = m.reloaded()
		if msg.err {
			m.status = "⚠ open failed"
		} else {
			m.status = "↗ opened workspace"
		}
	case noteEditedMsg:
		if msg.ok {
			m = m.act(setNote(msg.id, msg.val), fmt.Sprintf("📝 note saved on #%d", msg.id))
		}
	case agentDoneMsg:
		m = m.reloaded()
		if msg.err {
			m.status = "⚠ agent failed"
		} else {
			m.status = "🤖 agent done — see the card"
		}
	case ingestedMsg:
		m.ingesting = false
		m = m.reloaded()
		if msg.err {
			m.status = "⚠ ingest failed (check glab / claude auth)"
		} else {
			m.status = "✓ ingest done — board refreshed"
		}
	case tickMsg:
		if !m.focusing || msg.gen != m.focusGen {
			return m, nil // stale tick from an ended/restarted focus session
		}
		if !time.Now().Before(m.focusEnds) {
			id := m.focusID
			m.focusing, m.focusGen = false, m.focusGen+1
			_ = stopTask(id)
			m = m.reloaded()
			_ = exec.Command("notify-send", "deck", fmt.Sprintf("focus block done on #%d", id)).Start()
			m.status = fmt.Sprintf("⏰ focus done on #%d — another round? f", id)
			return m, nil
		}
		return m, tickCmd(m.focusGen)
	case tea.KeyMsg:
		if m.mode != "" { // capture / filter input mode swallows keystrokes
			switch msg.Type {
			case tea.KeyCtrlC:
				return m, tea.Quit
			case tea.KeyEsc:
				if m.mode == "filter" {
					m.filter = ""
				}
				m.mode, m.input = "", ""
				m.card = clampi(m.card, m.visN())
				return m.scrolled(), nil
			case tea.KeyEnter:
				switch m.mode {
				case "add":
					if strings.TrimSpace(m.input) != "" {
						err := addTask(m.input) // parses +tags / project: / Pn
						txt := m.input
						m.mode, m.input = "", ""
						m = m.act(err, "+ captured: "+trunc(txt, 40))
					} else {
						m.mode, m.input = "", ""
					}
				case "note":
					if t, ok := m.selected(); ok && t.ID > 0 && strings.TrimSpace(m.input) != "" {
						err := appendNote(t.ID, m.input)
						m.mode, m.input = "", ""
						m = m.act(err, fmt.Sprintf("📝 noted #%d", t.ID))
					} else {
						m.mode, m.input = "", ""
					}
				case "agent":
					if t, ok := m.selected(); ok && t.ID > 0 && strings.TrimSpace(m.input) != "" {
						instr := m.input
						m.mode, m.input = "", ""
						m.status = "🤖 working…"
						return m, agentCmd(t.ID, instr)
					}
					m.mode, m.input = "", ""
				default: // filter
					m.filter = m.input
					m.mode, m.input = "", ""
					m.card = clampi(m.card, m.visN())
					m = m.scrolled()
				}
				return m, nil
			case tea.KeyBackspace:
				if r := []rune(m.input); len(r) > 0 {
					m.input = string(r[:len(r)-1])
				}
			case tea.KeyCtrlU:
				m.input = ""
			case tea.KeySpace:
				m.input += " "
			case tea.KeyRunes:
				m.input += string(msg.Runes)
			}
			if m.mode == "filter" { // live-filter as you type
				m.filter = m.input
				m.card = clampi(m.card, m.visN())
				m = m.scrolled()
			}
			return m, nil
		}
		switch msg.String() {
		case "q", "esc", "ctrl+c":
			return m, tea.Quit
		case "r":
			m = m.reloaded()
			m.status = "↻ reloaded"
		case "u": // undo the last change (revert the last commit)
			m = m.act(undoLast(), "↩ undid the last change")
		case "I": // background ingest via DECK_INGEST_CMD; auto-reloads when done
			if m.ingesting {
				m.status = "📥 already ingesting…"
			} else if c := ingestCmd(); c != nil {
				m.ingesting = true
				m.status = "📥 ingesting… (board stays usable; refreshes when done)"
				return m, c
			} else {
				m.status = "set DECK_INGEST_CMD to enable I"
			}
		case "left", "h":
			m.col = clampi(m.col-1, len(m.cols))
			m.card = clampi(m.card, m.visN())
			m = m.scrolled()
		case "right", "l":
			m.col = clampi(m.col+1, len(m.cols))
			m.card = clampi(m.card, m.visN())
			m = m.scrolled()
		case "up", "k":
			m.card = clampi(m.card-1, m.visN())
			m = m.scrolled()
		case "down", "j":
			m.card = clampi(m.card+1, m.visN())
			m = m.scrolled()
		case "K": // scroll the detail pane up
			if m.detailOff > 0 {
				m.detailOff--
			}
		case "J": // scroll the detail pane down
			if m.detailOff < m.detailMaxOff() {
				m.detailOff++
			}
		case "g", "home":
			m.card = 0
			m = m.scrolled()
		case "G", "end":
			m.card = clampi(1<<30, m.visN())
			m = m.scrolled()
		case "a": // capture a new task (type, enter to add)
			m.mode, m.input = "add", ""
		case "/": // filter the board by area / state / text
			m.mode, m.input = "filter", ""
		case "N": // jot a note on the selected card
			if t, ok := m.selected(); ok && t.ID > 0 {
				m.mode, m.input = "note", ""
			}
		case "E": // edit the selected card's full note in $EDITOR
			if t, ok := m.selected(); ok && t.ID > 0 {
				return m, editNoteCmd(t.ID, t.Notes)
			}
		case ":": // ask the agent to act on the selected card (draft reply, comment, summarise…)
			if t, ok := m.selected(); ok && t.ID > 0 {
				if hookSet("DECK_AGENT_CMD") {
					m.mode, m.input = "agent", ""
				} else {
					m.status = "set DECK_AGENT_CMD to enable the agent"
				}
			}

		// ── actions on the selected card (open tasks only; DONE cards have no id) ──
		case "enter": // work on it: hand off to DECK_OPEN_CMD (suspends so it can run a picker)
			if t, ok := m.selected(); ok {
				url, _ := splitNote(t.Notes)
				if c := hookCmd("DECK_OPEN_CMD", url); c != nil {
					m.status = "opening workspace…"
					return m, tea.ExecProcess(c, func(err error) tea.Msg { return openedMsg{err != nil} })
				}
				m.status = "set DECK_OPEN_CMD to enable enter"
			}
		case "d": // resolve
			if t, ok := m.selected(); ok && t.ID > 0 {
				m = m.act(done(t.ID), fmt.Sprintf("✓ resolved #%d", t.ID))
			}
		case "n": // toggle today (+now)
			if t, ok := m.selected(); ok && t.ID > 0 {
				if t.has("now") {
					m = m.act(setTags(t.ID, nil, []string{"now"}), fmt.Sprintf("← #%d out of today", t.ID))
				} else {
					m = m.act(setTags(t.ID, []string{"now"}, nil), fmt.Sprintf("→ #%d to today", t.ID))
				}
			}
		case "e": // generate the detail card via DECK_ENRICH_CMD (async)
			if t, ok := m.selected(); ok && t.ID > 0 {
				if c := enrichCmd(t.ID); c != nil {
					m.status = fmt.Sprintf("describing #%d…", t.ID)
					return m, c
				}
				m.status = "set DECK_ENRICH_CMD to enable e"
			}
		case "s": // start ↔ stop (activate ↔ deactivate)
			if t, ok := m.selected(); ok && t.ID > 0 {
				if t.Status == dstask.STATUS_ACTIVE {
					m = m.act(stopTask(t.ID), fmt.Sprintf("■ stopped #%d", t.ID))
				} else {
					m = m.act(startTask(t.ID), fmt.Sprintf("▶ started #%d", t.ID))
				}
			}
		case "f": // focus: a 25-min pomodoro on the selected card, with an in-board countdown
			if t, ok := m.selected(); ok && t.ID > 0 {
				if m.focusing && m.focusID == t.ID { // toggle off
					m.focusing, m.focusGen = false, m.focusGen+1
					m = m.act(stopTask(t.ID), fmt.Sprintf("■ focus ended on #%d", t.ID))
				} else {
					m.focusID, m.focusEnds, m.focusing = t.ID, time.Now().Add(focusMinutes*time.Minute), true
					m.focusGen++
					gen := m.focusGen
					m = m.act(startTask(t.ID), fmt.Sprintf("⏳ focus #%d · %02d:00", t.ID, focusMinutes))
					return m, tickCmd(gen)
				}
			}
		case "o": // open the source (issue/MR/mail) in the browser
			if t, ok := m.selected(); ok {
				if url, _ := splitNote(t.Notes); url != "" {
					_ = exec.Command("xdg-open", url).Start()
					m.status = "↗ opened source in browser"
				} else {
					m.status = "this card has no link"
				}
			}
		case "H", "L": // drag the card across columns (TODAY→+now, WAITING→+waiting, DONE→resolve, NEXT→plain)
			dir := -1
			if msg.String() == "L" {
				dir = 1
			}
			if t, ok := m.selected(); ok && t.ID > 0 {
				if target := clampi(m.col+dir, len(m.cols)); target != m.col {
					dest := m.cols[target].title
					var err error
					switch target { // columns are fixed in load(): 0 TODAY · 1 NEXT · 2 WAITING · 3 DONE
					case 0:
						err = setTags(t.ID, []string{"now"}, []string{"waiting"})
					case 1:
						err = setTags(t.ID, nil, []string{"now", "waiting"})
					case 2:
						err = setTags(t.ID, []string{"waiting"}, []string{"now"})
					case 3:
						err = done(t.ID)
					}
					m = m.act(err, fmt.Sprintf("moved #%d → %s", t.ID, dest))
				}
			}
		}
	}
	return m, nil
}
