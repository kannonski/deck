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
