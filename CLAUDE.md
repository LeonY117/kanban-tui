# Kanban

Terminal kanban board and task tracker — and the tracker this repo's own tickets live on.

## Develop

```bash
go build -o /tmp/kanban . && /tmp/kanban --sprint demo   # run it from a scratch path
gofmt -l . && go vet ./... && go test -race ./...        # green here is what done means
```

Never build over `~/.local/bin/kanban`: that is Leon's live tool across ~14 boards, so an
unreviewed build there becomes his daily driver. If he asks for an install, `rm` the old file and
`mv` the new one in — `cp` and `go build -o` rewrite the inode, and macOS then SIGKILLs the binary
with a bare `zsh: killed`.

## Tests

The TUI has no other harness: behaviour is proved by driving `Model.Update` and reading
`Model.View`, never by launching it. Add cases to the existing `internal/tui/*_test.go` and reuse
these rather than writing new helpers:

- `testModel(t, "title", …)` or `boardWith(t, "title|TODO|tag,tag")` — a model over a board
- `withSprint(t, "demo", …)` — a second board
- `keyPress("j")` → `m.Update(…)` — a keystroke
- `m.View()` to register zones, then `zoneOf(t, m, kind, col, idx)` and `m.mouseClick(mouseAt(x, y))` — a click
- `sandboxRoot(t)` points `KANBAN_FILE` at a temp dir — `testModel` and `boardWith` call it for you

`~/.kanban` is a live board — never add, move or archive on it to try something out. A bulk
`kanban archive` during testing once swallowed 14 real tickets.

## Where facts belong

| Fact | Home |
|---|---|
| How the tool behaves — data model, commands, flags, storage | `kanban --help`, and each subcommand's own |
| How the TUI is driven — keys, views, mouse | `README.md`. Change a key, change the table |
| Why a decision was made | A comment beside the code, where the next person to simplify it away will look |
| Who claims what, title style, when a sprint is worth making | The *consuming* project's instruction file — caller policy, not the tool's |

Leon's tickets for this repo are on the `kanban` sprint: `kanban --sprint kanban list --json`.
