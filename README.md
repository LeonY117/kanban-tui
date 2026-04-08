# kanban

A terminal kanban board. Single binary, single JSON file.

Built with [Bubble Tea](https://github.com/charmbracelet/bubbletea) and designed to work alongside CLI agents (like Claude Code) that read and write the board via the command line while you watch the TUI update live.

## Install

Requires Go 1.22+.

```bash
go install github.com/leon/kanban@latest
```

Or build from source:

```bash
git clone https://github.com/leon/kanban.git
cd kanban
go build -o kanban .
```

## Usage

Run `kanban` to launch the TUI, or use the CLI for scripting and automation.

### TUI

```bash
kanban
```

The board shows your tickets across columns: **Todo**, **Doing**, **Done**, and **Hold**.

#### Navigation

| Key | Action |
|-----|--------|
| `h`/`l` | Move between columns |
| `j`/`k` | Move between tickets |
| `+` | Zoom in (board -> split -> column/detail) |
| `-` | Zoom out |
| `]`/`[` | Switch between list and detail panels (split view) |
| `Enter`/`e` | Edit selected field (split detail) |
| `Esc` | Stop editing / go back |
| `H`/`L` | Move ticket to adjacent column |
| `a` | Add a new ticket |
| `f` | Toggle focus mode (Todo + Doing only) |
| `1`-`4` | Jump to column |
| `d` | Delete ticket (in detail view) |
| `q` | Quit |

#### Views

- **Board** -- overview of all columns
- **Split** (`+`) -- ticket list on the left, editable detail on the right
- **Column** (`+` from split list) -- full-width single column with tags and assignees
- **Detail** (`+` from split detail) -- full-screen ticket editor

### CLI

```bash
# Add a ticket
kanban add "Fix login bug" --desc "Users can't log in with SSO" --tag backend --status TODO

# Update a ticket (use short ID prefix)
kanban update abc123 --status DOING --assigned-to alice

# List tickets
kanban list
kanban list --status DOING --json

# Show a ticket
kanban show abc123

# Archive completed tickets
kanban archive
```

## Storage

Everything lives in `~/.kanban/`:

- `board.json` -- your tickets
- `archive.json` -- archived (done) tickets
- `.board.lock` -- file lock for concurrent access

Set `KANBAN_FILE` to use a custom board path.

## Live reload

The TUI polls `board.json` every 2 seconds. Changes from the CLI or other processes appear automatically -- no restart needed.

## License

MIT
