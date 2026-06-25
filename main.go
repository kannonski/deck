// dskan — a kanban TUI over the dstask task store. Fully standalone: it reads and
// writes the store directly via the dstask library (github.com/naggie/dstask).
// Columns: TODAY (+now) · NEXT (actionable pool, P3 noise hidden) · WAITING · DONE (today).
// Built in: h/l/j/k move · H/L drag · o open · d done · n ±today · s start/stop ·
// f focus (in-board pomodoro) · u undo · a capture · N note · / filter · r reload · q quit.
// Optional external hooks, enabled only when the env var is set (else the key hides):
//   DSKAN_OPEN_CMD <url>      → enter   ·  DSKAN_ENRICH_CMD <id> → e
//   DSKAN_INGEST_CMD          → I       ·  DSKAN_CARD_DIR        → detail-pane card
//   DSKAN_AGENT_CMD <id> <instr> → :    (act on the task: draft / comment, in the foreground)
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

func load() ([]column, int) {
	conf := dstask.NewConfig()
	ts, err := dstask.LoadTaskSet(conf.Repo, conf.IDsFile, true) // include resolved for the DONE column
	if err != nil {
		return emptyCols(), 0
	}
	today := time.Now().Format("2006-01-02")

	var now, next, waiting, done []task
	resolvedDays := map[string]bool{}
	for _, dt := range ts.AllTasks() {
		t := toTask(dt)
		switch dt.Status {
		case dstask.STATUS_RESOLVED:
			if t.Resolved != "" {
				resolvedDays[t.Resolved] = true
			}
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
	return cols, streakFrom(resolvedDays)
}

// streakFrom counts consecutive days (through today, or yesterday if nothing yet
// today) that have at least one resolved task.
func streakFrom(days map[string]bool) int {
	if len(days) == 0 {
		return 0
	}
	d := time.Now()
	if !days[d.Format("2006-01-02")] {
		d = d.AddDate(0, 0, -1) // nothing closed today yet is fine — no broken streak
	}
	n := 0
	for days[d.Format("2006-01-02")] {
		n++
		d = d.AddDate(0, 0, -1)
	}
	return n
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
	detailOff int    // scroll offset within the detail pane (J/K)
	mode      string // "" nav · "add" capture · "filter"
	input     string // text buffer while in add/filter mode
	filter    string // active filter (area/state/summary substring); "" = all
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
	const footer = 4 // stats line + help + up to two of filter/ingesting/status
	detail := h * 2 / 5 // a big detail pane: ~40% of the screen
	if detail < 10 {
		detail = 10
	}
	if mx := h - footer - 10; detail > mx { // always leave ≥10 rows for the board
		detail = mx
	}
	if detail < 5 {
		detail = 5
	}
	colH = (h - footer - detail) - 2 // -2 for the column box border
	detailH = detail - 2             // -2 for the detail box border
	if colH < 5 {
		colH = 5
	}
	if detailH < 3 {
		detailH = 3
	}
	vis = (colH - 4) / 2 // title + blank + two ↑/↓ indicator lines
	if vis < 3 {
		vis = 3
	}
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

// undoLast reverts the most recent change — every mutation is its own git commit.
func undoLast() error {
	c := dstask.NewConfig()
	return dstask.RunGitCmd(c.Repo, "revert", "--no-edit", "HEAD")
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
		return "⏳ no card yet — press e to generate"
	}
	return ""
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
		for _, n := range strings.Split(notes, "\n") {
			lines = append(lines, trunc(n, iw))
		}
	}
	if card := cardText(t); card != "" {
		lines = append(lines, "")
		for _, c := range strings.Split(card, "\n") {
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
	off := m.detailOff
	if mx := len(lines) - detailH; off > mx {
		off = mx
	}
	if off < 0 {
		off = 0
	}
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
	if mx := len(m.detailLines(detailInnerWidth(m.w))) - detailH; mx > 0 {
		return mx
	}
	return 0
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

// tickMsg drives the focus countdown; gen lets stale ticks (from an old/ended focus
// session) be ignored, so restarting focus never double-speeds the clock.
type tickMsg struct{ gen int }

func tickCmd(gen int) tea.Cmd {
	return tea.Tick(time.Second, func(time.Time) tea.Msg { return tickMsg{gen} })
}

const focusMinutes = 25

// noteEditedMsg carries the edited note text back from $EDITOR.
type noteEditedMsg struct {
	id  int
	val string
	ok  bool
}

// editNoteCmd opens the task's note in $EDITOR (temp file), then returns the result.
func editNoteCmd(id int, current string) tea.Cmd {
	f, err := os.CreateTemp("", "dskan-note-*.md")
	if err != nil {
		return func() tea.Msg { return noteEditedMsg{id: id} }
	}
	name := f.Name()
	_, _ = f.WriteString(current)
	_ = f.Close()
	editor := os.Getenv("EDITOR")
	if editor == "" {
		editor = os.Getenv("VISUAL")
	}
	if editor == "" {
		editor = "vi"
	}
	parts := strings.Fields(editor)
	c := exec.Command(parts[0], append(parts[1:], name)...)
	return tea.ExecProcess(c, func(err error) tea.Msg {
		b, rerr := os.ReadFile(name)
		_ = os.Remove(name)
		if err != nil || rerr != nil {
			return noteEditedMsg{id: id}
		}
		return noteEditedMsg{id: id, val: strings.TrimRight(string(b), "\n"), ok: true}
	})
}

func setNote(id int, content string) error {
	return mutate(id, "Note on", func(t *dstask.Task) { t.Notes = content })
}

// agentDoneMsg arrives after the `:` agent (DSKAN_AGENT_CMD) finishes.
type agentDoneMsg struct{ err bool }

// agentCmd hands the task id + instruction to DSKAN_AGENT_CMD in the FOREGROUND
// (suspends the TUI, like enter) so it can read the source, draft, and — for GitLab —
// show the draft and prompt to post, all on the terminal. Returns nil if unset.
func agentCmd(id int, instruction string) tea.Cmd {
	c := hookCmd("DSKAN_AGENT_CMD", strconv.Itoa(id), instruction)
	if c == nil {
		return nil
	}
	return tea.ExecProcess(c, func(err error) tea.Msg { return agentDoneMsg{err != nil} })
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
			_ = exec.Command("notify-send", "dskan", fmt.Sprintf("focus block done on #%d", id)).Start()
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
				if hookSet("DSKAN_AGENT_CMD") {
					m.mode, m.input = "agent", ""
				} else {
					m.status = "set DSKAN_AGENT_CMD to enable the agent"
				}
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

func (m model) View() string {
	n := len(m.cols)
	cw := 30
	if m.w > 0 {
		cw = m.w/n - 2
		if cw < 16 {
			cw = 16
		}
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
		if hookSet("DSKAN_OPEN_CMD") {
			hints = append(hints, "↵ work")
		}
		hints = append(hints, "a add", "N note", "E edit", "/ filter", "o open", "f focus", "d done", "n today", "s start/stop", "u undo")
		if hookSet("DSKAN_AGENT_CMD") {
			hints = append(hints, ": agent")
		}
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
		if m.focusing {
			rem := time.Until(m.focusEnds)
			if rem < 0 {
				rem = 0
			}
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
	return board + "\n" + m.detailView() + "\n" + foot
}

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
