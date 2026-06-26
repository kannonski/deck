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

// buildColumns turns the configured columns into render columns (without cards).
func buildColumns() []column {
	cols := make([]column, len(cfg.Columns))
	for i, cc := range cfg.Columns {
		cols[i] = column{title: cc.Title, accent: lipgloss.Color(cc.Accent)}
	}
	return cols
}

// load reads the store and buckets tasks into the configured columns; also returns the
// streak. An open task lands in the first column whose `tag` it carries, else the `pool`
// column (skipping `hide_priority`); resolved-today tasks go to `resolved_today`.
func load() ([]column, int) {
	cols := buildColumns()
	conf := dstask.NewConfig()
	ts, err := dstask.LoadTaskSet(conf.Repo, conf.IDsFile, true) // include resolved for the DONE column
	if err != nil {
		return cols, 0
	}
	today := time.Now().Format("2006-01-02")
	buckets := make([][]task, len(cfg.Columns))
	resolvedDays := map[string]bool{}

	poolIdx := -1
	for i, cc := range cfg.Columns {
		if cc.Pool {
			poolIdx = i
		}
	}

	for _, dt := range ts.AllTasks() {
		t := toTask(dt)
		switch dt.Status {
		case dstask.STATUS_RESOLVED:
			if t.Resolved != "" {
				resolvedDays[t.Resolved] = true
			}
			if strings.HasPrefix(t.Resolved, today) {
				for i, cc := range cfg.Columns {
					if cc.ResolvedToday {
						buckets[i] = append(buckets[i], t)
					}
				}
			}
		case dstask.STATUS_PENDING, dstask.STATUS_ACTIVE, dstask.STATUS_PAUSED,
			dstask.STATUS_DELEGATED, dstask.STATUS_DEFERRED:
			placed := false
			for i, cc := range cfg.Columns {
				if cc.Tag != "" && t.has(cc.Tag) {
					buckets[i] = append(buckets[i], t)
					placed = true
					break
				}
			}
			if !placed && poolIdx >= 0 {
				if hp := cfg.Columns[poolIdx].HidePriority; hp == "" || t.Priority != hp {
					buckets[poolIdx] = append(buckets[poolIdx], t)
				}
			}
		}
	}
	if poolIdx >= 0 {
		sort.SliceStable(buckets[poolIdx], func(i, j int) bool {
			return buckets[poolIdx][i].Priority < buckets[poolIdx][j].Priority
		})
	}
	for i := range cols {
		cols[i].cards = buckets[i]
	}
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
