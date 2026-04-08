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

# Archive done tickets
kanban archive
kanban archive --before 2026-04-07
```

## Agent workflow

1. At start of session: `kanban list --json` to see current board state
2. Before starting work: `kanban update <id> --status DOING --assigned-to <name>`
3. When done: `kanban update <id> --status DONE`
4. If creating new work: `kanban add "Title" --tag <tag>`

## Statuses

BACKLOG → TODO → DOING → DONE (or HOLD)

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
- Override: `KANBAN_FILE` env var

## Skills distribution

Skills are symlinked from this repo into other projects:
```bash
ln -sf ~/dev/projects/kanban/skills/*.md <project>/.claude/skills/
```
Currently set up for: `~/dev/projects/openclaw-surgeon/`
