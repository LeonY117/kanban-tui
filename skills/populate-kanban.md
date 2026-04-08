# populate-kanban

Read a markdown todo/planning file and create kanban tickets from it.

## Usage

```
/populate-kanban <path-to-md-file>
```

## Instructions

1. Read the markdown file at the given path.
2. Identify **actionable items** — lines with `- [ ]` checkboxes or clear task descriptions. Skip:
   - Completed items (`- [x]`)
   - Context lines, notes, and commentary (e.g., "Context: came from a chat...")
   - Section headers themselves (use them for tagging, not as tickets)
   - Items that are clearly observations, not tasks

3. For each actionable item, infer:
   - **Title**: The core task, cleaned up (remove markdown formatting, checkbox syntax)
   - **Description**: Any sub-bullets, context notes, or "possible approach" lines under the item
   - **Tags**: Derive from the section header (e.g., "## Sales" → `sales`, "### Knowledge base" → `kb`)
   - **Priority**: Infer from structure:
     - Items under "## Priority" or "### [something] important" → P1
     - Items under "## Quick fixes" → P2
     - Items under "## Research" or "## Reading" → P3
     - Items under "## Carried forward" → P2
     - Default → P2
   - **Status**: Default to TODO unless context suggests otherwise (e.g., "still observing" → BACKLOG)

4. Before creating tickets, show the user a summary table of what you'll create:
   ```
   Title                              | Status | Priority | Tags
   -----------------------------------|--------|----------|------
   Message Andy Wang                  | TODO   | P1       | admin
   Fix Core/ section                  | TODO   | P1       | kb
   ...
   ```

5. Ask the user to confirm before proceeding.

6. For each confirmed ticket, run:
   ```bash
   kanban add "<title>" --desc "<description>" --status <status> --priority <priority> --tag <tag1> --tag <tag2>
   ```

7. After all tickets are created, run `kanban list` to show the final board state.

## Notes

- The markdown format is intentionally loose — use your judgment for ambiguous items
- When in doubt about whether something is a task, include it as P3/BACKLOG
- Deduplicate: if a ticket with a very similar title already exists (`kanban list --json`), skip it and note the duplicate
