package main

import (
	"os"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

// ── detail-pane cards: pre-generated "<ref>.md" files under DECK_CARD_DIR ──
var (
	refRe    = regexp.MustCompile(`\[(gl[!#][0-9]+|mail:[^\]]+)\]$`)
	nonAlnum = regexp.MustCompile(`[^a-zA-Z0-9]`)
)

// ── optional external integrations. Configured in config.toml ([hooks] open/agent/
// enrich/ingest, [cards] dir) or via DECK_* env (the file supersedes env). An empty
// hook hides its key, so the core stays a standalone dstask kanban. ──
//
// hookCmd builds a command from a configured command line (space-split) plus extra
// args; nil if the command line is empty (the corresponding key is then hidden).
func hookCmd(cmdline string, args ...string) *exec.Cmd {
	parts := strings.Fields(cmdline)
	if len(parts) == 0 {
		return nil
	}
	return exec.Command(parts[0], append(parts[1:], args...)...)
}

// descPath maps a task's source ref to its card file under DECK_CARD_DIR ("" if unset).
func descPath(summary string) string {
	dir := cfg.Cards.Dir
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
		return "" // card feature not configured (DECK_CARD_DIR unset)
	}
	if b, err := os.ReadFile(f); err == nil && len(strings.TrimSpace(string(b))) > 0 {
		return strings.TrimRight(string(b), "\n")
	}
	if cfg.Hooks.Enrich != "" {
		return "⏳ no card yet — press e to generate"
	}
	return ""
}

// enrichedMsg arrives when an async DECK_ENRICH_CMD finishes.
type enrichedMsg struct {
	id  int
	err bool
}

func enrichCmd(id int) tea.Cmd {
	c := hookCmd(cfg.Hooks.Enrich, strconv.Itoa(id))
	if c == nil {
		return nil
	}
	return func() tea.Msg { return enrichedMsg{id: id, err: c.Run() != nil} }
}

// openedMsg arrives after `enter` finishes opening a workspace tab.
type openedMsg struct{ err bool }

// ingestedMsg arrives when a background DECK_INGEST_CMD (the `I` key) finishes.
type ingestedMsg struct{ err bool }

func ingestCmd() tea.Cmd {
	c := hookCmd(cfg.Hooks.Ingest)
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

// noteEditedMsg carries the edited note text back from $EDITOR.
type noteEditedMsg struct {
	id  int
	val string
	ok  bool
}

// editNoteCmd opens the task's note in $EDITOR (temp file), then returns the result.
func editNoteCmd(id int, current string) tea.Cmd {
	f, err := os.CreateTemp("", "deck-note-*.md")
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

// agentDoneMsg arrives after the `:` agent (DECK_AGENT_CMD) finishes.
type agentDoneMsg struct{ err bool }

// agentCmd hands the task id + instruction to DECK_AGENT_CMD in the FOREGROUND
// (suspends the TUI, like enter) so it can read the source, draft, and — for GitLab —
// show the draft and prompt to post, all on the terminal. Returns nil if unset.
func agentCmd(id int, instruction string) tea.Cmd {
	c := hookCmd(cfg.Hooks.Agent, strconv.Itoa(id), instruction)
	if c == nil {
		return nil
	}
	return tea.ExecProcess(c, func(err error) tea.Msg { return agentDoneMsg{err != nil} })
}
