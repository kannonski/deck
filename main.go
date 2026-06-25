// dskan — a kanban TUI over the dstask task store. Fully standalone: it reads and
// writes the store directly via the dstask library (github.com/naggie/dstask).
// Columns: TODAY (+now) · NEXT (actionable pool, P3 noise hidden) · WAITING · DONE (today).
// Built in: h/l/j/k move · H/L drag · o open link · d done · n ±today · s start/stop ·
// a capture · N note · / filter · r reload · q quit.
// Optional external hooks, enabled only when the env var is set (else the key hides):
//   DSKAN_OPEN_CMD <url>  → enter   ·  DSKAN_ENRICH_CMD <id> → e
//   DSKAN_INGEST_CMD      → I       ·  DSKAN_CARD_DIR        → detail-pane card
// --once dumps the view to stdout and exits.
package main

import (
	"flag"
	"fmt"
	"os"
	"os/exec"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	dstask "github.com/naggie/dstask"
)

// task is the lightweight view model the TUI renders; it's mapped from dstask.Task.
type task struct {
	ID       int
	UUID     string
	Summary  string
	Status   string // pending | active | paused | resolved
	Tags     []string
	Project  string
	Priority string
	Due      string // "" or YYYY-MM-DD
	Notes    string // source URL on line 1, user notes below
	Resolved string // "" or YYYY-MM-DD
}

func (t task) has(tag string) bool {
	for _, x := range t.Tags {
		if x == tag {
			return true
		}
	}
	return false
}

func (t task) state() string {
	for _, s := range []string{"quick", "deep", "low", "waiting"} {
		if t.has(s) {
			return s
		}
	}
	return ""
}

// toTask maps a dstask.Task into our view model (time.Time → "YYYY-MM-DD" / "").
func toTask(dt dstask.Task) task {
	t := task{
		ID: dt.ID, UUID: dt.UUID, Summary: dt.Summary, Status: dt.Status,
		Tags: dt.Tags, Project: dt.Project, Priority: dt.Priority, Notes: dt.Notes,
	}
	if !dt.Due.IsZero() {
		t.Due = dt.Due.Format("2006-01-02")
	}
	if !dt.Resolved.IsZero() {
		t.Resolved = dt.Resolved.Format("2006-01-02")
	}
	return t
}

type column struct {
	title  string
	accent lipgloss.Color
	cards  []task
}

func emptyCols() []column {
	return []column{
		{"★ TODAY", lipgloss.Color("183"), nil},
		{"NEXT", lipgloss.Color("117"), nil},
		{"WAITING", lipgloss.Color("245"), nil},
		{"✓ DONE", lipgloss.Color("120"), nil},
	}
}

func load() []column {
	conf := dstask.NewConfig()
	ts, err := dstask.LoadTaskSet(conf.Repo, conf.IDsFile, true) // include resolved for the DONE column
	if err != nil {
		return emptyCols()
	}
	today := time.Now().Format("2006-01-02")

	var now, next, waiting, done []task
	for _, dt := range ts.AllTasks() {
		t := toTask(dt)
		switch dt.Status {
		case dstask.STATUS_RESOLVED:
			if strings.HasPrefix(t.Resolved, today) {
				done = append(done, t)
			}
		case dstask.STATUS_PENDING, dstask.STATUS_ACTIVE, dstask.STATUS_PAUSED,
			dstask.STATUS_DELEGATED, dstask.STATUS_DEFERRED:
			switch {
			case t.has("now"):
				now = append(now, t)
			case t.has("waiting"):
				waiting = append(waiting, t)
			case t.Priority != "P3": // hide the declassified / vuln-mgmt noise from the active flow
				next = append(next, t)
			}
		}
	}
	sort.SliceStable(next, func(i, j int) bool { return next[i].Priority < next[j].Priority })

	cols := emptyCols()
	cols[0].cards, cols[1].cards, cols[2].cards, cols[3].cards = now, next, waiting, done
	return cols
}

func areaColor(p string) lipgloss.Color {
	switch p {
	case "customer":
		return lipgloss.Color("211")
	case "team":
		return lipgloss.Color("117")
	case "work":
		return lipgloss.Color("180")
	case "personal":
		return lipgloss.Color("150")
	}
	return lipgloss.Color("245")
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

const maxCards = 12

var (
	idStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("117"))
	dimStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("240"))
	helpStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("240"))
	selStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("212")).Bold(true)
)

type model struct {
	cols      []column
	w, h      int
	col, card int    // cursor: column index + card index
	off       []int  // per-column scroll offset (top visible card)
	mode      string // "" nav · "add" capture · "filter"
	input     string // text buffer while in add/filter mode
	filter    string // active filter (area/state/summary substring); "" = all
	ingesting bool   // a background `I` ingest is in flight
	status    string // last-action feedback, shown in the footer
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

// visible = cards that fit in one column for the current terminal height
// (each card is 2 lines; leave room for the detail pane, footer and box chrome).
func (m model) visible() int {
	h := m.h
	if h <= 0 {
		h = 40
	}
	v := (h - 16) / 2
	if v < 4 {
		v = 4
	}
	return v
}

// scrolled keeps the cursor inside the visible window of its column.
func (m model) scrolled() model {
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
	m.cols = load()
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

// ── store writes, via the dstask library (no subprocess) ──

func addTag(t *dstask.Task, tag string) {
	if !dstask.StrSliceContains(t.Tags, tag) {
		t.Tags = append(t.Tags, tag)
	}
}

func removeTag(t *dstask.Task, tag string) {
	out := make([]string, 0, len(t.Tags))
	for _, x := range t.Tags {
		if x != tag {
			out = append(out, x)
		}
	}
	t.Tags = out
}

// mutate loads the store, applies fn to task #id, then saves + commits.
func mutate(id int, msg string, fn func(t *dstask.Task)) error {
	c := dstask.NewConfig()
	ts, err := dstask.LoadTaskSet(c.Repo, c.IDsFile, false)
	if err != nil {
		return err
	}
	t, err := ts.GetByID(id)
	if err != nil {
		return err
	}
	fn(&t)
	if err := ts.UpdateTask(t); err != nil {
		return err
	}
	ts.SavePendingChanges()
	return dstask.GitCommit(c.Repo, msg+" %s", t)
}

func done(id int) error {
	return mutate(id, "Resolved", func(t *dstask.Task) {
		t.Status, t.Resolved = dstask.STATUS_RESOLVED, time.Now()
	})
}
func startTask(id int) error {
	return mutate(id, "Started", func(t *dstask.Task) { t.Status = dstask.STATUS_ACTIVE })
}
func stopTask(id int) error {
	return mutate(id, "Stopped", func(t *dstask.Task) { t.Status = dstask.STATUS_PAUSED })
}
func setTags(id int, add, remove []string) error {
	return mutate(id, "Modified", func(t *dstask.Task) {
		for _, a := range add {
			addTag(t, a)
		}
		for _, r := range remove {
			removeTag(t, r)
		}
	})
}

// addTask captures a new task, parsing +tags / project: / Pn from the text.
func addTask(text string) error {
	c := dstask.NewConfig()
	ts, err := dstask.LoadTaskSet(c.Repo, c.IDsFile, false)
	if err != nil {
		return err
	}
	q := dstask.ParseQuery(strings.Fields(text)...)
	if strings.TrimSpace(q.Text) == "" {
		return nil
	}
	t := dstask.Task{
		WritePending: true, Status: dstask.STATUS_PENDING, Summary: q.Text,
		Tags: q.Tags, Project: q.Project, Priority: q.Priority, Due: q.Due, Notes: q.Note,
	}
	if t, err = ts.LoadTask(t); err != nil {
		return err
	}
	ts.SavePendingChanges()
	return dstask.GitCommit(c.Repo, "Added %s", t)
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

// splitNote separates the source URL (line 1, if any) from the user notes below.
func splitNote(notes string) (url, body string) {
	notes = strings.TrimRight(notes, "\n")
	if notes == "" {
		return "", ""
	}
	parts := strings.SplitN(notes, "\n", 2)
	if strings.HasPrefix(parts[0], "http") {
		url = strings.TrimSpace(parts[0])
		if len(parts) > 1 {
			body = strings.TrimSpace(parts[1])
		}
		return
	}
	return "", strings.TrimSpace(notes)
}

// appendNote adds a line to a task's notes (keeps the URL on line 1 if present).
func appendNote(id int, line string) error {
	return mutate(id, "Note on", func(t *dstask.Task) {
		if strings.TrimSpace(t.Notes) == "" {
			t.Notes = line
		} else {
			t.Notes += "\n" + line
		}
	})
}

// ── detail-pane cards: pre-generated "<ref>.md" files under DSKAN_CARD_DIR ──
var (
	refRe    = regexp.MustCompile(`\[(gl[!#][0-9]+|mail:[^\]]+)\]$`)
	nonAlnum = regexp.MustCompile(`[^a-zA-Z0-9]`)
)

// ── optional external integrations, wired via env so the core stays standalone ──
//   DSKAN_OPEN_CMD   <url> → `enter`  (e.g. open a repo / workspace)
//   DSKAN_ENRICH_CMD <id>  → `e`      (generate the detail card)
//   DSKAN_INGEST_CMD       → `I`      (pull in new tasks)
//   DSKAN_CARD_DIR         → folder of pre-generated "<ref>.md" detail cards
func hookSet(env string) bool { return strings.TrimSpace(os.Getenv(env)) != "" }

// hookCmd builds a command from an env var (space-split) plus extra args; nil if unset.
func hookCmd(env string, args ...string) *exec.Cmd {
	parts := strings.Fields(os.Getenv(env))
	if len(parts) == 0 {
		return nil
	}
	return exec.Command(parts[0], append(parts[1:], args...)...)
}

// descPath maps a task's source ref to its card file under DSKAN_CARD_DIR ("" if unset).
func descPath(summary string) string {
	dir := strings.TrimSpace(os.Getenv("DSKAN_CARD_DIR"))
	if dir == "" {
		return ""
	}
	mm := refRe.FindStringSubmatch(strings.TrimSpace(summary))
	if mm == nil {
		return ""
	}
	return dir + "/" + nonAlnum.ReplaceAllString(mm[1], "_") + ".md"
}

func cardText(t task) string {
	f := descPath(t.Summary)
	if f == "" {
		return "" // card feature not configured (DSKAN_CARD_DIR unset)
	}
	if b, err := os.ReadFile(f); err == nil && len(strings.TrimSpace(string(b))) > 0 {
		return strings.TrimRight(string(b), "\n")
	}
	if hookSet("DSKAN_ENRICH_CMD") {
		return dimStyle.Render("⏳ no card yet — press e to generate")
	}
	return ""
}

// detailView is the bottom pane: the selected task's heading, meta, link and card.
func (m model) detailView() string {
	dw := 76
	if m.w > 6 {
		dw = m.w - 4
	}
	box := lipgloss.NewStyle().Width(dw).Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("240")).Padding(0, 1)
	t, ok := m.selected()
	if !ok {
		return box.Render(dimStyle.Render("no card selected"))
	}
	meta := fmt.Sprintf("#%d · %s", t.ID, t.Project)
	if s := t.state(); s != "" {
		meta += " · " + s
	}
	if t.Status == "active" {
		meta += " · ▶ active"
	}
	if !strings.HasPrefix(t.Due, "0001") && t.Due != "" {
		meta += " · due " + t.Due[:10]
	}
	body := lipgloss.NewStyle().Foreground(areaColor(t.Project)).Bold(true).Render(trunc(t.Summary, dw-2)) +
		"\n" + dimStyle.Render(meta)
	url, notes := splitNote(t.Notes)
	if url != "" {
		body += "\n" + dimStyle.Render("↗ "+trunc(url, dw-2))
	}
	if notes != "" {
		body += "\n\n" + selStyle.Render(fmt.Sprintf("📝 notes (%d)", strings.Count(notes, "\n")+1)) + "\n" + notes
	}
	if card := cardText(t); card != "" {
		body += "\n\n" + card
	}
	return box.Render(body)
}

// enrichedMsg arrives when an async DSKAN_ENRICH_CMD finishes.
type enrichedMsg struct {
	id  int
	err bool
}

func enrichCmd(id int) tea.Cmd {
	c := hookCmd("DSKAN_ENRICH_CMD", strconv.Itoa(id))
	if c == nil {
		return nil
	}
	return func() tea.Msg { return enrichedMsg{id: id, err: c.Run() != nil} }
}

// openedMsg arrives after `enter` finishes opening a workspace tab.
type openedMsg struct{ err bool }

// ingestedMsg arrives when a background DSKAN_INGEST_CMD (the `I` key) finishes.
type ingestedMsg struct{ err bool }

func ingestCmd() tea.Cmd {
	c := hookCmd("DSKAN_INGEST_CMD")
	if c == nil {
		return nil
	}
	return func() tea.Msg { return ingestedMsg{c.Run() != nil} }
}

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
	case ingestedMsg:
		m.ingesting = false
		m = m.reloaded()
		if msg.err {
			m.status = "⚠ ingest failed (check glab / claude auth)"
		} else {
			m.status = "✓ ingest done — board refreshed"
		}
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
		case "I": // background ingest via DSKAN_INGEST_CMD; auto-reloads when done
			if m.ingesting {
				m.status = "📥 already ingesting…"
			} else if c := ingestCmd(); c != nil {
				m.ingesting = true
				m.status = "📥 ingesting… (board stays usable; refreshes when done)"
				return m, c
			} else {
				m.status = "set DSKAN_INGEST_CMD to enable I"
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

		// ── actions on the selected card (open tasks only; DONE cards have no id) ──
		case "enter": // work on it: hand off to DSKAN_OPEN_CMD (suspends so it can run a picker)
			if t, ok := m.selected(); ok {
				url, _ := splitNote(t.Notes)
				if c := hookCmd("DSKAN_OPEN_CMD", url); c != nil {
					m.status = "opening workspace…"
					return m, tea.ExecProcess(c, func(err error) tea.Msg { return openedMsg{err != nil} })
				}
				m.status = "set DSKAN_OPEN_CMD to enable enter"
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
		case "e": // generate the detail card via DSKAN_ENRICH_CMD (async)
			if t, ok := m.selected(); ok && t.ID > 0 {
				if c := enrichCmd(t.ID); c != nil {
					m.status = fmt.Sprintf("describing #%d…", t.ID)
					return m, c
				}
				m.status = "set DSKAN_ENRICH_CMD to enable e"
			}
		case "s": // start ↔ stop (activate ↔ deactivate)
			if t, ok := m.selected(); ok && t.ID > 0 {
				if t.Status == dstask.STATUS_ACTIVE {
					m = m.act(stopTask(t.ID), fmt.Sprintf("■ stopped #%d", t.ID))
				} else {
					m = m.act(startTask(t.ID), fmt.Sprintf("▶ started #%d", t.ID))
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

func (m model) View() string {
	n := len(m.cols)
	cw := 30
	if m.w > 0 {
		cw = m.w/n - 2
		if cw < 16 {
			cw = 16
		}
	}
	vis := m.visible()
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
		end := off + vis
		if end > len(cards) {
			end = len(cards)
		}
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
			if t.Status == "active" {
				meta = "▶ active · " + meta
				metaSt = lipgloss.NewStyle().Foreground(lipgloss.Color("120"))
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
		border := c.accent
		if !active {
			border = lipgloss.Color("238")
		}
		box := lipgloss.NewStyle().
			Width(cw).Border(lipgloss.RoundedBorder()).BorderForeground(border).Padding(0, 1).
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
	default:
		hints := []string{"h/l/j/k move", "H/L drag"}
		if hookSet("DSKAN_OPEN_CMD") {
			hints = append(hints, "↵ work")
		}
		hints = append(hints, "a add", "N note", "/ filter", "o open", "d done", "n today", "s start/stop")
		if hookSet("DSKAN_ENRICH_CMD") {
			hints = append(hints, "e card")
		}
		if hookSet("DSKAN_INGEST_CMD") {
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
	}
	return board + "\n" + m.detailView() + "\n" + foot
}

func main() {
	once := flag.Bool("once", false, "render the kanban once to stdout and exit")
	flag.Parse()

	c := load()
	m := model{cols: c, off: make([]int, len(c))}
	if *once {
		fmt.Println(m.View())
		return
	}
	if _, err := tea.NewProgram(m, tea.WithAltScreen()).Run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
