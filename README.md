# kanban-tui

A terminal kanban board built for **human + agent collaboration**. The human drives a TUI; agents drive the same board through a CLI. Both surfaces read and write the same single JSON file, live.

## Why

A TUI alone is already a nice way for terminal-native people to track tasks across days, weeks, multiple projects. The reason kanban-tui exists is what happens when **agents can read and write tickets too**:

- **A living doc of what you're working on.** Any agent (Claude Code, custom CLI agents, scripts) can pick up a ticket with its full context — description, tags, status, history.
- **Plans you can parallelise.** When you break a big task into chunks, you don't need a markdown file that everyone fights over. Multiple agents can spawn, each seeing what's `DOING` vs `TODO`, and claim their slice.
- **A real archive.** Done tickets move out of view but stay searchable, so you can answer "what did I ship last month" without scrolling through git.

The whole thing is local — JSON files in `~/.kanban/`, no daemon, no server, no cloud. Vim-style keys throughout. The TUI updates within seconds of any CLI change.

## Install

```bash
git clone https://github.com/LeonY117/kanban-tui.git
cd kanban-tui
go build -o kanban .
mv kanban ~/.local/bin/   # or anywhere on your $PATH
```

Requires Go 1.22+.

## TUI

```bash
kanban
```

Columns: **Backlog**, **Todo**, **Doing**, **Done**, **Hold**. Backlog is hidden by default — press `0` to jump to it.

### Keys

| Key | Action |
|-----|--------|
| `h` `l` | Move between columns |
| `j` `k` | Move between tickets |
| `H` `L` | Move selected ticket to adjacent column |
| `J` `K` | Reorder selected ticket up/down within its column |
| `m` | Move ticket — pick a column, or another board and its column |
| `c` | Copy the selected ticket's short id to the clipboard |
| `a` | Add a new ticket (floating popup) |
| `e` / `enter` | Edit selected field |
| `enter` (while editing) | Save and stop editing — in the add popup, create the ticket |
| `shift+enter` | New line inside a description |
| `esc` | Stop editing / step back (the add popup asks before discarding) |
| `0`-`4` | Jump to column (Backlog / Todo / Doing / Done / Hold) |
| `+` / `-` | Zoom in / out (board → split → column / detail) |
| `[` / `]` | Switch panels in split view |
| `v` | Ticket size — cards (default) → large → condensed |
| `V` | Toggle columns / rows layout |
| `x` | Archive selected ticket |
| `X` | Open archive browser |
| `u` | Unarchive (in archive browser) |
| `tab` | Open board picker (main + sprints) |
| `d` | Delete ticket (in detail view) |
| `?` | Help |
| `q` | Quit |

`shift+enter` only reaches the TUI if your terminal sends an escape for it —
in Ghostty, `keybind = shift+enter=text:\x1b\r`. `ctrl+j` works everywhere as
a fallback.

### Ticket sizes

`v` cycles three densities. **Cards** (default) show the title and a
`shortid #tags ● assignee` line. **Large** adds a three-line description
preview and the last-edited date. **Condensed** is one line per ticket.

At card and condensed size, tickets are separated by a rule above each block
with a closing rule under the last. Large gives each ticket its own box —
at that size the extra air reads better.

Columns scroll stickily: the cursor travels inside the visible window and only
pushes it once it reaches an edge, so scrolling back up starts exactly where
scrolling down did. The mouse wheel banks a few notches per ticket step, to
match the one-line-per-notch feel of scrolling a description.

The ticket under the cursor gets an accent bar down its left edge, heavy
accented rules bracketing it, and its short id in the column colour — in the
large size, the box border carries the accent instead. None of that changes a
block's height, so moving the cursor never shifts the list. The ticket that
straddles the bottom of a column is cropped mid-block rather than dropped, so
the column always reads as continuing past the fold.

### Mouse

The mouse is live: click a ticket to select it, click a column to focus it,
click a field in the detail pane to jump to it. The wheel scrolls one line at
a time — tickets in a list, or the description body under the pointer. Hold
`shift` to select text as usual.

### Views

- **Board** — overview of all columns.
- **Split** (`+`) — ticket list on the left, editable detail on the right.
- **Column** (`+` from split list) — full-width single column.
- **Detail** (`+` from split detail) — full-screen ticket editor.

## CLI

The CLI is the agent surface. Anything an agent needs to read or write goes through here. All commands accept `--json` for structured output.

```bash
# Add
kanban add "Fix login bug" --desc "Users can't log in with SSO" --tag backend --status TODO --assigned-to alice

# Update (use the short ID prefix shown in `list`)
kanban update abc123 --status DOING --assigned-to claude

# List
kanban list
kanban list --status DOING --json
kanban list --assigned-to claude

# Show one ticket
kanban show abc123 --json

# Archive (one ticket, or all DONE at once)
kanban archive abc123
kanban archive
kanban archive --before 2026-04-07
```

## Sprint boards

A sprint board is an isolated second board with its own tickets, archive, and lock — useful for time-boxed pushes or parallel tracks you want to keep off the main board.

```bash
kanban --sprint demo-april                     # launch TUI on the sprint
kanban --sprint demo-april list --json         # CLI on the sprint
kanban --sprint demo-april add "Fix login bug" # add to the sprint

kanban sprints                                 # list active sprints + ticket counts
kanban sprints new demo-april                  # create
kanban sprints archive demo-april              # hide + freeze (reads still work)
kanban sprints unarchive demo-april            # restore
kanban sprints rm demo-april                   # delete
```

Sprint names: `[A-Za-z0-9_-]`, 1–64 chars. From the TUI, `tab` opens a board picker that switches between main and any active sprint.

## Storage

Everything lives in `~/.kanban/`:

- `board.json` — your tickets.
- `archive.json` — archived tickets.
- `.board.lock` — file lock for concurrent access (so the TUI and a CLI-driven agent don't trample each other).
- `sprints/<name>/` — same layout, per sprint.

Override the root with the `KANBAN_FILE` env var.

## Live reload

The TUI polls `board.json` every couple of seconds. Changes from the CLI, agents, or another shell appear automatically — no restart, no refresh keystroke.

## License

TBD — see issue tracker.
