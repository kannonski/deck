# dskan

A kanban TUI for [dstask](https://github.com/naggie/dstask). It reads and writes
your `~/.dstask` store directly via the dstask library (no subprocess), so it stays
in sync with the `dstask` CLI.

Columns: **TODAY** (`+now`) · **NEXT** (the actionable pool, `P3` hidden) ·
**WAITING** (`+waiting`) · **DONE** (resolved today). A bottom pane shows the
selected task's source link, its notes, and — if configured — a detail card.

## Install

```sh
go install ./...        # or: go build -o dskan .
```

Requires Go 1.21+ and an existing dstask repo (`~/.dstask`, or `$DSTASK_GIT_REPO`).

## Keys

| Key | Action |
|-----|--------|
| `h`/`l`/`j`/`k` | move cursor (columns / cards) · `g`/`G` top/bottom |
| `H`/`L` | drag the selected card across columns (retags / resolves) |
| `a` | capture a task (parses `+tags`, `project:`, `Pn`) |
| `N` | jot a note on the selected task (appended to its dstask note) |
| `/` | live filter by area / state / summary |
| `d` | resolve · `n` toggle today (`+now`) · `s` start↔stop |
| `o` | open the task's source link in the browser |
| `r` | reload · `q` quit |

## Optional integrations (env-configured)

dskan is fully usable on its own. These hooks add extra keys only when the env var
is set (otherwise the key is hidden):

| Env var | Enables | Receives | Example |
|---------|---------|----------|---------|
| `DSKAN_OPEN_CMD` | `enter` — open a workspace | the source URL | `myscript open` |
| `DSKAN_ENRICH_CMD` | `e` — generate a detail card | the task id | `myscript enrich` |
| `DSKAN_INGEST_CMD` | `I` — pull in new tasks | (none) | `myscript ingest` |
| `DSKAN_CARD_DIR` | detail-pane card | — | reads `<dir>/<ref>.md` |

The command in each var is split on spaces and run with the argument appended.
`enter` suspends the TUI while its command runs (so it can show its own picker).
