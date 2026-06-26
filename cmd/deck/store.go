package main

import (
	"os"
	"strings"
	"time"

	dstask "github.com/naggie/dstask"
)

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

// silenced runs fn with stdout/stderr redirected to /dev/null. dstask's git calls
// inherit os.Stdout/os.Stderr (RunCmd), and that git output would otherwise scroll
// and corrupt the alt-screen. Bubble Tea renders via its own captured writer, so the
// screen is unaffected. (Writes run synchronously in Update — no concurrent output.)
func silenced(fn func() error) error {
	null, err := os.OpenFile(os.DevNull, os.O_WRONLY, 0)
	if err != nil {
		return fn()
	}
	defer null.Close()
	so, se := os.Stdout, os.Stderr
	os.Stdout, os.Stderr = null, null
	defer func() { os.Stdout, os.Stderr = so, se }()
	return fn()
}

// mutate loads the store, applies fn to task #id, then saves + commits.
func mutate(id int, msg string, fn func(t *dstask.Task)) error {
	return silenced(func() error {
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
	})
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

// poolHidePriority is the hide_priority of the actionable pool column ("" if none).
func poolHidePriority() string {
	for _, c := range cfg.Columns {
		if c.Pool {
			return c.HidePriority
		}
	}
	return ""
}

// raisePriority lifts a priority one level (P3→P2, …), floored at P0.
func raisePriority(p string) string {
	switch p {
	case "P3":
		return "P2"
	case "P2":
		return "P1"
	case "P1":
		return "P0"
	}
	return p
}

// dropToPool removes column tags and — if the task would then fall into the pool at the
// pool's hidden priority — raises it one level so it stays visible. Without this, taking a
// P3 task off waiting/today (or dragging it to NEXT) would silently vanish it. One commit.
func dropToPool(id int, remove []string) error {
	hide := poolHidePriority()
	return mutate(id, "Modified", func(t *dstask.Task) {
		for _, r := range remove {
			removeTag(t, r)
		}
		if hide == "" || t.Priority != hide {
			return
		}
		for _, tg := range columnTags() { // still carries a column tag → it won't hit the pool
			if dstask.StrSliceContains(t.Tags, tg) {
				return
			}
		}
		t.Priority = raisePriority(hide)
	})
}

// addTask captures a new task, parsing +tags / project: / Pn from the text.
func addTask(text string) error {
	return silenced(func() error {
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
	})
}

// undoLast reverts the most recent change — every mutation is its own git commit.
func undoLast() error {
	return silenced(func() error {
		c := dstask.NewConfig()
		return dstask.RunGitCmd(c.Repo, "revert", "--no-edit", "HEAD")
	})
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

func setNote(id int, content string) error {
	return mutate(id, "Note on", func(t *dstask.Task) { t.Notes = content })
}

// modifyTask applies dstask-style modifiers to a task: +tag / -tag / Pn / project:x.
// (Fields are set directly rather than via Task.Modify, which appends to Notes.)
func modifyTask(id int, query string) error {
	q := dstask.ParseQuery(strings.Fields(query)...)
	return mutate(id, "Modified", func(t *dstask.Task) {
		for _, tag := range q.Tags {
			addTag(t, tag)
		}
		for _, tag := range q.AntiTags {
			removeTag(t, tag)
		}
		if q.Priority != "" {
			t.Priority = q.Priority
		}
		if q.Project != "" {
			t.Project = q.Project
		}
		for _, p := range q.AntiProjects {
			if t.Project == p {
				t.Project = ""
			}
		}
	})
}
