# Kanban

Terminal kanban board and task tracker. Single binary, single JSON file (`~/.kanban/board.json`).

## Build & run

```bash
go build -o kanban .
go build -o ~/.local/bin/kanban .   # install to PATH
./kanban          # launches TUI
./kanban list     # CLI mode
```

## CLI

`kanban --help` documents the tool — data model, every command and flag, storage layout — and
each subcommand carries its own `--help`. Keep it that way: a fact about *how the tool
behaves* belongs in the cobra help text, not here.

The inverse also holds. Workflow conventions — who claims what, title style, when a sprint is
worth making — are the caller's policy, not the tool's. They stay in the consuming project's
instruction file so they can be versioned and varied per project; don't compile them into the
binary's help text.

## TUI behaviour

Not reachable from `--help`, so it lives here:

- Pinned sprints sit above a divider in the picker; `p` toggles a pin, `J`/`K` reorder the pinned block. Main is implicitly pinned and holds the top slot. **Archiving a pinned sprint is refused** — unpin first.
- `r` in the picker renames a sprint and/or its ticket-id prefix. A prefix change rewrites board **and** archive short ids keeping their numbers (`KA7` → `KB7`), refused per-id if another board already issued one. Both are refused on archived sprints.
- A missing `--sprint` prompts `[y/N]` on TUI launch but hard-errors on CLI subcommands — no silent creation, no hanging prompts for agents.

## Where work lives

Leon's kanban-tool tickets are on the `kanban` sprint, not the main board. Default to
`kanban --sprint kanban list --json` when asked about "open tickets" / "what should I pick up"
from inside this repo. The main board is personal/Kepler/work tickets, unrelated to this code.

## Architecture

- `internal/model/` — Ticket struct, status/priority types, filtering
- `internal/store/` — JSON persistence with flock, archive
- `internal/tui/` — Bubble Tea TUI (board, column, detail views)
- `cmd/` — Cobra CLI commands
- `skills/` — Claude Code skills (populate-kanban, kanban-summary), symlinked into other projects

## Storage

- Ticket ids: sequential per prefix — bare numbers on main (`42`), `<PREFIX><n>` on sprints (`KA7`). Prefix defaults to the first two letters of the board name; boards may share one and then share its number line.
- Board: `~/.kanban/board.json`
- Archive: `~/.kanban/archive.json`
- Lock: `~/.kanban/.board.lock`
- Id counters: `~/.kanban/counters.json`
- Pinned sprints: `~/.kanban/pins.json` (ordered list of sprint names)
- Sprints: `~/.kanban/sprints/<name>/{board,archive}.json` + `.board.lock`
- Override: `KANBAN_FILE` env var redirects the main board path; sprints live under `$(dirname $KANBAN_FILE)/sprints/`

## Skills distribution

`skills/` holds `populate-kanban` and `kanban-summary`, symlinked into consuming projects:
```bash
ln -sf ~/dev/projects/tools/kanban/skills/*.md <project>/.claude/skills/
```
No project currently links them (the previous consumer, openclaw-surgeon, is gone).
