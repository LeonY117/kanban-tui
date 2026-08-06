package tui

import (
	"fmt"
	"strings"

	"github.com/LeonY117/kanban-tui/internal/model"
	"github.com/LeonY117/kanban-tui/internal/store"
	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

// The move popup is two lists side by side: every board on the left, the
// highlighted board's columns on the right. It replaced a three-stage funnel
// (this board's columns → "other board…" → that board's columns) where the
// destination board was a mode you were in rather than something on screen:
// picking the wrong one meant walking back out through esc, and the column list
// never said whose columns it was showing.
//
// The board you are standing on is in the left list like any other, so a plain
// column move costs the same two keys it always did — the popup opens on the
// columns pane with that board already highlighted.

type movePane int

const (
	movePaneBoards movePane = iota
	movePaneColumns
)

// moveSep separates the two panes on every row. Its width is part of the popup
// sizing, so it is named rather than inlined.
const moveSep = " │ "

// moveCurrentTag marks the column the ticket is in now, on the board it is on.
const moveCurrentTag = "(current)"

type moveState struct {
	ticketID string
	shortID  string
	status   model.Status // the column the ticket is in now

	boards   []pickerEntry
	boardIdx int
	colIdx   int // index into model.ColumnOrder
	pane     movePane
}

// board is the destination currently highlighted on the left.
func (s *moveState) board() (pickerEntry, bool) {
	if s.boardIdx < 0 || s.boardIdx >= len(s.boards) {
		return pickerEntry{}, false
	}
	return s.boards[s.boardIdx], true
}

func (m *Model) enterMovePopup() (tea.Model, tea.Cmd) {
	if !m.guardMutate() {
		return m, nil
	}
	t := m.selectedTicket()
	if t == nil {
		return m, nil
	}

	// Archived sprints stay out: they are hidden from the picker until asked
	// for, and a board the TUI refuses to write to is not a destination.
	boards, err := loadPickerEntries(false)
	if err != nil {
		m.err = err
		m.notice = err.Error()
		return m, nil
	}

	m.move = moveState{
		ticketID: t.ID,
		shortID:  t.ShortID,
		status:   t.Status,
		boards:   boards,
		// The columns pane holds the keyboard from the start. Most moves are to
		// another column of the board you are already on, and that shouldn't
		// cost a keystroke spent walking past a list you aren't using.
		pane: movePaneColumns,
	}
	for i, e := range boards {
		if e.name == m.sprintName {
			m.move.boardIdx = i
		}
	}
	for i, s := range model.ColumnOrder {
		if s == t.Status {
			m.move.colIdx = i
		}
	}

	m.popupReturnView = m.view
	m.view = moveView
	return m, nil
}

// moveMoveCursor walks the focused pane. The other pane keeps its own cursor —
// stepping through columns must not disturb the board they were picked for.
func (m *Model) moveMoveCursor(dir int) {
	if m.move.pane == movePaneBoards {
		if next := m.move.boardIdx + dir; next >= 0 && next < len(m.move.boards) {
			m.move.boardIdx = next
		}
		return
	}
	if next := m.move.colIdx + dir; next >= 0 && next < len(model.ColumnOrder) {
		m.move.colIdx = next
	}
}

func (m *Model) updateMove(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch {
	case key.Matches(msg, keys.Quit):
		return m, tea.Quit
	case key.Matches(msg, keys.Esc):
		// esc steps back to the boards before it closes, so a wrong turn costs
		// one key rather than reopening the popup.
		if m.move.pane == movePaneColumns {
			m.move.pane = movePaneBoards
			return m, nil
		}
		m.restorePopupView(moveView)
	case key.Matches(msg, keys.Move):
		m.restorePopupView(moveView)
	case key.Matches(msg, keys.Up):
		m.moveMoveCursor(-1)
	case key.Matches(msg, keys.Down):
		m.moveMoveCursor(1)
	case key.Matches(msg, keys.Left):
		m.move.pane = movePaneBoards
	case key.Matches(msg, keys.Right), key.Matches(msg, keys.Enter):
		return m.moveActivate()
	}
	return m, nil
}

// moveActivate is enter, and the second click on an already-selected row: on
// the boards pane it hands over to that board's columns, on the columns pane it
// performs the move.
func (m *Model) moveActivate() (tea.Model, tea.Cmd) {
	if m.move.pane == movePaneBoards {
		m.move.pane = movePaneColumns
		return m, nil
	}
	e, ok := m.move.board()
	if !ok || m.move.colIdx < 0 || m.move.colIdx >= len(model.ColumnOrder) {
		return m, nil
	}
	m.commitMove(e.name, model.ColumnOrder[m.move.colIdx])
	return m, nil
}

// moveClick selects what was clicked and activates it on a second click of the
// row already under the cursor — the rule every list in the TUI follows.
func (m *Model) moveClick(z *hitZone) (tea.Model, tea.Cmd) {
	pane := movePane(z.col)
	var already bool
	if pane == movePaneBoards {
		already = m.move.pane == pane && m.move.boardIdx == z.idx
		m.move.boardIdx = z.idx
	} else {
		already = m.move.pane == pane && m.move.colIdx == z.idx
		m.move.colIdx = z.idx
	}
	m.move.pane = pane
	if already {
		return m.moveActivate()
	}
	return m, nil
}

// commitMove performs the move and closes the popup.
func (m *Model) commitMove(targetBoard string, status model.Status) {
	dst := m.store
	if targetBoard != m.sprintName {
		s, err := boardStore(targetBoard)
		if err != nil {
			m.err = err
			m.notice = err.Error()
			return
		}
		dst = s
	}

	if err := store.MoveTicket(m.store, dst, m.move.ticketID, &status); err != nil {
		m.err = err
		m.notice = err.Error()
		return
	}

	m.reload()
	m.restorePopupView(moveView)

	if targetBoard == m.sprintName {
		// Same board: follow the ticket into its new column.
		for i, s := range model.ColumnOrder {
			if s == status {
				m.focusedCol = i
			}
		}
		for i, t := range m.visibleTickets(status) {
			if t.ID == m.move.ticketID {
				m.cursors[m.focusedCol] = i
			}
		}
		m.notice = fmt.Sprintf("moved to %s", statusDisplay[status])
	} else {
		m.notice = fmt.Sprintf("moved to %s / %s", boardDisplayName(targetBoard), statusDisplay[status])
	}

	m.clampCursors()
	if m.view == splitView || m.view == detailView {
		m.refreshDetailEditors()
	}
}

// boardStore opens the store for a board by sprint name ("" = main).
func boardStore(sprintName string) (*store.Store, error) {
	if sprintName == "" {
		return store.New(""), nil
	}
	return store.NewSprint(sprintName)
}

// ─── Render ──────────────────────────────────────────────────────────

func (m *Model) viewMove() string {
	title := "Move " + m.move.shortID

	leftWidth, rightWidth := m.movePaneWidths()
	width := leftWidth + lipgloss.Width(moveSep) + rightWidth + 4
	if w := lipgloss.Width(title) + 6; w > width {
		width = w
	}
	if width > m.width-4 {
		width = m.width - 4
	}

	// One header row above the two lists; the taller pane sets the rest.
	height := 3 + max(len(m.moveBoardLines()), len(model.ColumnOrder))
	if height > m.height-4 {
		height = m.height - 4
	}
	if height < 6 {
		height = 6
	}

	backdrop := m.popupBackdrop(m.popupReturnView)
	m.resetZones()

	origin := m.popupOrigin(width, height)
	popup := m.renderMovePopup(title, width, height, origin)
	return overlayAt(backdrop, popup, origin.x, origin.y)
}

// movePaneWidths sizes the two lists: the widest board name on the left, and on
// the right the widest column label with room for the count that sits off the
// same edge on every row.
func (m *Model) movePaneWidths() (left, right int) {
	const marker = 2
	left = lipgloss.Width("Boards")
	for _, e := range m.move.boards {
		if w := lipgloss.Width(boardDisplayName(e.name)) + marker; w > left {
			left = w
		}
	}

	for _, s := range model.ColumnOrder {
		if w := lipgloss.Width(statusDisplay[s]); w > right {
			right = w
		}
	}
	// marker + label + gap + "(current)" + gap + up to a three-digit count.
	right += marker + 2 + lipgloss.Width(moveCurrentTag) + 2 + 3
	if w := lipgloss.Width(m.moveColumnsHeader()); w > right {
		right = w
	}
	return left, right
}

func (m *Model) moveColumnsHeader() string {
	if e, ok := m.move.board(); ok {
		return "Columns · " + boardDisplayName(e.name)
	}
	return "Columns"
}

// moveBoardLines lays the left pane out through the same rule as the board
// picker: pinned boards, a divider, then the rest.
func (m *Model) moveBoardLines() []pickerLine {
	return boardLines(m.move.boards)
}

func (m *Model) renderMovePopup(title string, width, height int, origin point) string {
	innerWidth := width - 4
	if innerWidth < 10 {
		innerWidth = 10
	}
	leftWidth, rightWidth := m.movePaneWidths()
	// A terminal too narrow for both panes takes it out of the boards, whose
	// names the eye can finish from a few letters; a column row carries its
	// count on the same line and goes unreadable first.
	if over := leftWidth + lipgloss.Width(moveSep) + rightWidth - innerWidth; over > 0 {
		leftWidth = max(leftWidth-over, 6)
		rightWidth = max(innerWidth-leftWidth-lipgloss.Width(moveSep), 6)
	}

	rowX, rowY := origin.x+2, origin.y+1
	// renderPanel clips content to its inner height, so a zone registered for a
	// row it drops would sit over the popup's bottom border and the backdrop
	// below it. Both panes render at most this many rows.
	body := height - 3
	if body < 1 {
		body = 1
	}

	left := m.renderMoveBoards(leftWidth, body, point{x: rowX, y: rowY + 1})
	right := m.renderMoveColumns(rightWidth, body, point{x: rowX + leftWidth + lipgloss.Width(moveSep), y: rowY + 1})

	lines := []string{
		padTo(paneHeader("Boards", leftWidth, m.move.pane == movePaneBoards), leftWidth) +
			strings.Repeat(" ", lipgloss.Width(moveSep)) +
			paneHeader(m.moveColumnsHeader(), rightWidth, m.move.pane == movePaneColumns),
	}
	for i := 0; i < body; i++ {
		l, r := "", ""
		if i < len(left) {
			l = left[i]
		}
		if i < len(right) {
			r = right[i]
		}
		lines = append(lines, padTo(l, leftWidth)+dimStyle.Render(moveSep)+r)
	}

	content := lipgloss.NewStyle().PaddingLeft(1).Render(strings.Join(lines, "\n"))
	return renderPanel(title, content, width, height, green, true)
}

// paneHeader names a list and says whether it holds the keyboard — with the
// cursor markers, the popup's focus cue.
func paneHeader(label string, width int, focused bool) string {
	style := dimStyle
	if focused {
		style = lipgloss.NewStyle().Foreground(green).Bold(true)
	}
	return style.Render(ansi.Truncate(label, max(width, 1), "…"))
}

// padTo right-fills a rendered string to width, measuring what it draws rather
// than the escape sequences it carries.
func padTo(s string, width int) string {
	if gap := width - lipgloss.Width(s); gap > 0 {
		return s + strings.Repeat(" ", gap)
	}
	return s
}

// renderMoveBoards draws the left pane, windowed on the cursor the way the
// board picker windows its own list, and registers a zone per board row.
func (m *Model) renderMoveBoards(width, height int, origin point) []string {
	lines := m.moveBoardLines()
	start := 0
	if len(lines) > height {
		start = pickerLineOf(lines, m.move.boardIdx) - height/2
		if start < 0 {
			start = 0
		}
		if start+height > len(lines) {
			start = len(lines) - height
		}
	}

	focused := m.move.pane == movePaneBoards
	var out []string
	for i := start; i < len(lines) && len(out) < height; i++ {
		if lines[i].boardIdx < 0 {
			out = append(out, dimStyle.Render(strings.Repeat("─", width)))
			continue
		}
		e := m.move.boards[lines[i].boardIdx]
		m.addZone(hitZone{
			kind: zoneMoveRow,
			x:    origin.x,
			y:    origin.y + len(out),
			w:    width,
			h:    1,
			col:  int(movePaneBoards),
			idx:  lines[i].boardIdx,
		})

		style := lipgloss.NewStyle()
		if e.name == m.sprintName {
			// The board the ticket is on now, marked the way the picker marks
			// the board you are standing on.
			style = style.Foreground(green).Bold(true)
		}
		name := ansi.Truncate(boardDisplayName(e.name), max(width-2, 1), "…")
		out = append(out, moveMarker(lines[i].boardIdx == m.move.boardIdx, focused)+style.Render(name))
	}
	return out
}

// renderMoveColumns draws the right pane: the highlighted board's columns, each
// with the number of cards already sitting in it.
func (m *Model) renderMoveColumns(width, height int, origin point) []string {
	e, _ := m.move.board()
	focused := m.move.pane == movePaneColumns

	var out []string
	for i, s := range model.ColumnOrder {
		if len(out) >= height {
			break
		}
		m.addZone(hitZone{
			kind: zoneMoveRow,
			x:    origin.x,
			y:    origin.y + i,
			w:    width,
			h:    1,
			col:  int(movePaneColumns),
			idx:  i,
		})

		label := lipgloss.NewStyle().Foreground(columnColor(s)).Render(statusDisplay[s])
		if e.name == m.sprintName && s == m.move.status {
			label += dimStyle.Render("  " + moveCurrentTag)
		}
		count, countStyle := fmt.Sprintf("%d", e.counts[s]), dimStyle
		if e.counts[s] > 0 {
			countStyle = statusCountStyles[s]
		}

		left := moveMarker(i == m.move.colIdx, focused) + label
		gap := width - lipgloss.Width(left) - lipgloss.Width(count)
		if gap < 1 {
			gap = 1
		}
		out = append(out, left+strings.Repeat(" ", gap)+countStyle.Render(count))
	}
	return out
}

// moveMarker is the cursor. Both panes keep one, so the popup never loses track
// of what a click would land on, but only the focused pane's is lit.
func moveMarker(selected, focused bool) string {
	switch {
	case selected && focused:
		return selectedMarker.Render("* ")
	case selected:
		return dimStyle.Render("* ")
	default:
		return "  "
	}
}
