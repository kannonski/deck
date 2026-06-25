# deck

A kanban TUI for [dstask](https://github.com/naggie/dstask). It reads and writes
your `~/.dstask` store directly via the dstask library (no subprocess), so it stays
in sync with the `dstask` CLI.

Columns: **TODAY** (`+now`) · **NEXT** (the actionable pool, `P3` hidden) ·
**WAITING** (`+waiting`) · **DONE** (resolved today). A bottom pane shows the
selected task's source link, its notes, and — if configured — a generated card.

```
┌ ▸ ★ TODAY ─┐┌ NEXT ───────┐┌ WAITING ──┐┌ ✓ DONE ───┐
│ ▸ 12 ship …│ │   7 refactor│ │  3 wait …│ │  ✓ deploy │
│   ▶ active │ │   1 triage …│ │  …       │ │  …        │
│            │ │ ↓ 41 more   │ │          │ │           │
└────────────┘└─────────────┘└──────────┘└───────────┘
┌─ detail ──────────────────────────────────────────────┐
│ ship the thing   #12 · team · deep · ▶ active          │
│ ↗ https://gitlab.example/acme/app/-/work_items/12      │
│                                                        │
│ 📝 notes (1)                                           │
│   blocked on review                                    │
└────────────────────────────────────────────────────────┘
  ✓ 4 today · 🔥 6      a add · / filter · d done · f focus · q quit
```

## Install

```sh
go install .            # or: go build -o deck .
```

Requires Go 1.21+ and an existing dstask repo (`~/.dstask`, or `$DSTASK_GIT_REPO`).

## Keys

| Key | Action |
|-----|--------|
| `h` `l` `j` `k` | move cursor · `g`/`G` top/bottom of a column |
| `H` `L` | drag the selected card across columns (retags / resolves) |
| `J` `K` | scroll the detail pane (long notes / cards) |
| `a` | capture a task (parses `+tags`, `project:`, `Pn`) |
| `N` / `E` | jot a note / edit the whole note in `$EDITOR` |
| `/` | live filter by area / state / summary |
| `d` `n` `s` | resolve · toggle today (`+now`) · start↔stop |
| `f` | focus — a 25-min pomodoro on the card, with a live countdown |
| `u` | undo the last change (reverts the last commit) |
| `o` | open the task's source link in the browser |
| `r` `q` | reload · quit |

The footer shows `✓ N today · 🔥 streak`. Started tasks are marked `▶ active`.

## Optional integrations (env-configured)

deck is fully usable on its own. These hooks add extra keys **only when the env var
is set** (otherwise the key is hidden), so you can wire it to your own automation:

| Env var | Enables | Receives | Notes |
|---------|---------|----------|-------|
| `DECK_OPEN_CMD` | `enter` — open a workspace | source URL | runs in the foreground (can show a picker) |
| `DECK_AGENT_CMD` | `:` — instruct an agent on the card | task id + instruction | foreground; draft / comment / answer |
| `DECK_ENRICH_CMD` | `e` — generate a detail card | task id | async |
| `DECK_INGEST_CMD` | `I` — pull in new tasks | (none) | async, auto-reloads |
| `DECK_CARD_DIR` | detail-pane card | — | reads `<dir>/<ref>.md` |

The command in each var is split on spaces and run with the argument(s) appended
(e.g. `DECK_OPEN_CMD="mytool open"` runs `mytool open <url>`). `enter` and `:` run in
the foreground (the TUI suspends) so the command can prompt or show a picker.

## Demo

deck is interactive, so record a real session rather than shipping a faked cast:

```sh
asciinema rec -c deck deck.cast     # play back: asciinema play deck.cast
```

## License

[MIT](LICENSE).
