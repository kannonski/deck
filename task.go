package main

import (
	"slices"
	"sort"
	"strings"
	"time"

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

func (t task) has(tag string) bool { return slices.Contains(t.Tags, tag) }

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

// load reads the store and buckets tasks into the four columns; also returns the streak.
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
