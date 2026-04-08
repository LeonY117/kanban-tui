# Kanban TUI

Terminal-based kanban board and task tracker built for humans and AI agents.

## Background

Built for a persistent-agents startup. We need a task tracker that:
- Shows what's left to do, what's in progress, across sessions
- Works as a TUI for the human operator (lazygit-style panels, keyboard-driven)
- Works as a CLI for AI agents (structured input/output, stable IDs)
- Persists state in a git-friendly format

Inspired by [kanban.bash](https://github.com/coderofsalvation/kanban.bash) (CSV-based ASCII kanban board) but more robust.

## Stack

- **Go** — single binary, no runtime dependencies
- **Bubble Tea** (charmbracelet/bubbletea) — TUI framework, Elm-architecture
- **Lip Gloss** (charmbracelet/lipgloss) — styling/layout
- **Cobra** — CLI subcommands
- **Storage:** single JSON file with UUID-based task IDs

## Architecture

```
cmd/            — CLI entrypoint (cobra)
  tui/          — Bubble Tea TUI (board, detail, input panels)
  cli/          — Non-interactive commands (add, update, list, show)
internal/
  store/        — JSON file read/write, file locking
  model/        — Task struct, status transitions, tags, filters
```

## Two modes

1. **TUI mode:** `kanban` — launches interactive board with panels (lazygit-style)
2. **CLI mode:** `kanban add "task" --tag backend --status todo` — for agents, scripts, automation
3. **JSON output:** `kanban list --json` — structured output for agent consumption

## Design principles

- Single JSON file as source of truth (git-diffable, human-readable)
- Stable UUIDs for task IDs (no line-number fragility)
- Statuses are configurable (default: TODO, DOING, DONE, HOLD, BACKLOG)
- Tags are free-text labels
- Keyboard-first TUI with vim-style navigation
- Agent-friendly: all mutations available via CLI, all reads support --json

## References

- [Bubble Tea](https://github.com/charmbracelet/bubbletea) — TUI framework
- [Lip Gloss](https://github.com/charmbracelet/lipgloss) — styling
- [lazygit](https://github.com/jesseduffield/lazygit) — UX inspiration (panel layout, keyboard nav)
- [kanban.bash](https://github.com/coderofsalvation/kanban.bash) — original inspiration (simplicity, CSV board)
- [Beads](https://github.com/steveyegge/beads) — dependency-aware agent task tracker (reference for agent ergonomics)
