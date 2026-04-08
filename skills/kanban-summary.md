# kanban-summary

Export the current kanban board state as a markdown summary document.

## Usage

```
/kanban-summary [output-path]
```

If no output path is given, print to stdout.

## Instructions

1. Run `kanban list --json` to get all tickets.

2. Generate a markdown document grouped by status, in this order:
   - **Done** — completed work (celebrate it)
   - **Doing** — in-progress work with assignees
   - **Todo** — upcoming work
   - **Hold** — paused items
   - **Backlog** — low-priority / future items

3. Format:

   ```markdown
   # Kanban Summary — 2026-04-08

   ## Done (3)
   - [x] Implement auth middleware (`a3f2b1`) — P1, backend
   - [x] Fix login page layout (`b7c3d2`) — P2, frontend

   ## In Progress (2)
   - [ ] Add rate limiting (`c8d4e3`) — P1, backend — assigned to claude
   - [ ] Design settings page (`d9e5f4`) — P2, frontend — assigned to leon

   ## Todo (4)
   - [ ] Write API docs (`e0f6a5`) — P2, docs
   ...

   ## Hold (1)
   - [ ] Multi-user support (`f1a7b6`) — P2

   ## Backlog (2)
   - [ ] Investigate caching (`a2b8c7`) — P3, research
   ...

   ---
   *Generated from kanban board*
   ```

4. Each ticket line includes:
   - Checkbox: `[x]` for done, `[ ]` for everything else
   - Title
   - Short ID in backticks
   - Priority and tags
   - Assignee if set

5. If an output path was given, write the file there. Otherwise, output the markdown directly.

6. Show a brief summary: "Exported N tickets (X done, Y in progress, Z todo)"
