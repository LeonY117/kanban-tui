# Kanban

Terminal kanban board and task tracker. Single binary, single JSON file (`~/.kanban/board.json`).

## Build & run

```bash
go build -o kanban .
go build -o ~/.local/bin/kanban .   # install to PATH
./kanban          # launches TUI
./kanban list     # CLI mode
```

## CLI reference (for agents)

```bash
# Create a ticket
kanban add "Title here" --desc "Details" --tag backend --status TODO --assigned-to claude

# Update a ticket (use short ID)
kanban update <id> --status DOING --assigned-to claude

# List tickets (use --json for structured output)
kanban list --json
kanban list --status DOING
kanban list --assigned-to claude

# Show ticket detail
kanban show <id> --json

# Archive a single ticket (any status)
kanban archive <id>

# Archive all done tickets
kanban archive
kanban archive --before 2026-04-07
```

## Sprint boards

A sprint board is an isolated second board (its own tickets, archive, and lock), stored at `~/.kanban/sprints/<name>/`. The main board is untouched.

```bash
# Launch the TUI on a sprint (prompts y/N if the sprint doesn't exist yet)
kanban --sprint Demo_Apr

# Any CLI command can be scoped to a sprint
kanban --sprint Demo_Apr list --json
kanban --sprint Demo_Apr add "Fix login bug" --tag backend

# Sprint management
kanban sprints                 # list sprints + ticket counts
kanban sprints new Demo_Apr    # create a sprint without launching TUI
kanban sprints rm Demo_Apr     # delete a sprint (prompts; --force skips)
```

Notes:
- Sprint names: `[A-Za-z0-9_-]`, 1–64 chars.
- On **CLI subcommands**, a missing sprint hard-errors (no silent creation, no hanging prompts for agents).
- On **TUI launch**, a missing sprint prompts `[y/N]` before creating. Answering no aborts cleanly.
- `--sprint` composes with `KANBAN_FILE`: sprints live at `$(dirname $KANBAN_FILE)/sprints/<name>/` when the env var is set, so both main and sprint boards share the same root.

## Agent workflow

1. At start of session: `kanban list --json` to see current board state
2. Before starting work: `kanban update <id> --status DOING --assigned-to <name>`
3. When done: `kanban update <id> --status DONE`
4. If creating new work: `kanban add "Title" --tag <tag>`

## Statuses

TODO → DOING → DONE (or HOLD)

Note: BACKLOG status exists in data but is hidden from TUI.

## Architecture

- `internal/model/` — Ticket struct, status/priority types, filtering
- `internal/store/` — JSON persistence with flock, archive
- `internal/tui/` — Bubble Tea TUI (board, column, detail views)
- `cmd/` — Cobra CLI commands
- `skills/` — Claude Code skills (populate-kanban, kanban-summary), symlinked into other projects

## Storage

- Board: `~/.kanban/board.json`
- Archive: `~/.kanban/archive.json`
- Lock: `~/.kanban/.board.lock`
- Sprints: `~/.kanban/sprints/<name>/{board,archive}.json` + `.board.lock`
- Override: `KANBAN_FILE` env var redirects the main board path; sprints live under `$(dirname $KANBAN_FILE)/sprints/`

## Skills distribution

Skills are symlinked from this repo into other projects:
```bash
ln -sf ~/dev/projects/kanban/skills/*.md <project>/.claude/skills/
```
Currently set up for: `~/dev/projects/openclaw-surgeon/`
