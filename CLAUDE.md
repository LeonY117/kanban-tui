# Kanban

Terminal kanban board and task tracker. Single binary, single JSON file (`~/.kanban/board.json`).

## Build & run

```bash
go build -o kanban .
./kanban          # launches TUI
./kanban list     # CLI mode
```

## CLI reference (for agents)

```bash
# Create a ticket
kanban add "Title here" --desc "Details" --priority p1 --tag backend --status TODO --assigned-to claude

# Update a ticket (use short ID)
kanban update <id> --status DOING --assigned-to claude

# List tickets (use --json for structured output)
kanban list --json
kanban list --status DOING
kanban list --assigned-to claude

# Show ticket detail
kanban show <id> --json
```

## Agent workflow

1. At start of session: `kanban list --json` to see current board state
2. Before starting work: `kanban update <id> --status DOING --assigned-to <name>`
3. When done: `kanban update <id> --status DONE`
4. If creating new work: `kanban add "Title" --priority <p0-p3> --tag <tag>`

## Statuses

BACKLOG → TODO → DOING → DONE (or HOLD)

## Priorities

P0 (critical), P1 (high), P2 (normal), P3 (low)
