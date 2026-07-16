package tui

import (
	"fmt"
	"strings"

	"github.com/LeonY117/kanban-tui/internal/model"
	"github.com/LeonY117/kanban-tui/internal/store"
	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// The move popup is a three-stage funnel: pick a column on this board, or
// step out to pick another board and then a column on it.
type moveStage int

const (
	moveStageColumn moveStage = iota
	moveStageBoard
	moveStageTargetColumn
)

// moveRow is one selectable line in the move popup.
type moveRow struct {
	label    string
	status   model.Status // set for column rows
	board    string       // sprint name for board rows ("" = main board)
	isStatus bool
	isBoard  bool
	isOther  bool // "other board…" — advances to the board list
	current  bool // the ticket's current column / the board we're on
}

func (m *Model) enterMovePopup() (tea.Model, tea.Cmd) {
	if !m.guardMutate() {
		return m, nil
	}
	t := m.selectedTicket()
	if t == nil {
		return m, nil
	}
	m.moveTicketID = t.ID
	m.moveTicketStatus = t.Status
	m.moveStage = moveStageColumn
	m.moveTargetBoard = ""
	m.buildMoveRows()
	m.popupReturnView = m.view
	m.view = moveView
	return m, nil
}

func (m *Model) buildMoveRows() {
	switch m.moveStage {
	case moveStageColumn, moveStageTargetColumn:
		m.moveRows = nil
		for _, s := range model.ColumnOrder {
			row := moveRow{
				label:    statusDisplay[s],
				status:   s,
				isStatus: true,
			}
			if m.moveStage == moveStageColumn {
				row.current = s == m.moveTicketStatus
			}
			m.moveRows = append(m.moveRows, row)
		}
		if m.moveStage == moveStageColumn {
			m.moveRows = append(m.moveRows, moveRow{label: "Other board…", isOther: true})
		}
		m.moveIdx = 0
		for i, r := range m.moveRows {
			if r.current {
				m.moveIdx = i
			}
		}

	case moveStageBoard:
		entries, err := loadPickerEntries(false)
		if err != nil {
			m.err = err
			return
		}
		m.moveRows = nil
		for _, e := range entries {
			if e.name == m.sprintName {
				continue // moving to the board you're already on is the stage-0 path
			}
			m.moveRows = append(m.moveRows, moveRow{
				label:   boardDisplayName(e.name),
				board:   e.name,
				isBoard: true,
			})
		}
		m.moveIdx = 0
	}
}

func (m *Model) moveMoveCursor(dir int) {
	next := m.moveIdx + dir
	if next < 0 || next >= len(m.moveRows) {
		return
	}
	m.moveIdx = next
}

func (m *Model) updateMove(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch {
	case key.Matches(msg, keys.Quit):
		return m, tea.Quit
	case key.Matches(msg, keys.Esc):
		// esc walks back a stage before closing the popup.
		switch m.moveStage {
		case moveStageTargetColumn:
			m.moveStage = moveStageBoard
			m.buildMoveRows()
		case moveStageBoard:
			m.moveStage = moveStageColumn
			m.buildMoveRows()
		default:
			m.restorePopupView(moveView)
		}
	case key.Matches(msg, keys.Move):
		m.restorePopupView(moveView)
	case key.Matches(msg, keys.Up):
		m.moveMoveCursor(-1)
	case key.Matches(msg, keys.Down):
		m.moveMoveCursor(1)
	case key.Matches(msg, keys.Enter), key.Matches(msg, keys.Right):
		return m.moveActivate()
	case key.Matches(msg, keys.Left):
		if m.moveStage != moveStageColumn {
			m.moveStage--
			m.buildMoveRows()
		}
	}
	return m, nil
}

// moveActivate applies the highlighted row: a column move commits, a board
// row advances to that board's column list.
func (m *Model) moveActivate() (tea.Model, tea.Cmd) {
	if m.moveIdx < 0 || m.moveIdx >= len(m.moveRows) {
		return m, nil
	}
	row := m.moveRows[m.moveIdx]

	switch {
	case row.isOther:
		m.moveStage = moveStageBoard
		m.buildMoveRows()
		if len(m.moveRows) == 0 {
			m.moveStage = moveStageColumn
			m.buildMoveRows()
			m.notice = "no other boards — create one with `kanban sprints new <name>`"
		}
	case row.isBoard:
		m.moveTargetBoard = row.board
		m.moveStage = moveStageTargetColumn
		m.buildMoveRows()
	case row.isStatus:
		targetBoard := m.moveTargetBoard
		if m.moveStage == moveStageColumn {
			targetBoard = m.sprintName
		}
		m.commitMove(targetBoard, row.status)
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

	if err := store.MoveTicket(m.store, dst, m.moveTicketID, status); err != nil {
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
		for i, t := range m.ticketsFor(status) {
			if t.ID == m.moveTicketID {
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

func (m *Model) viewMove() string {
	title, rows := m.moveTitle(), m.moveRows

	width := 34
	for _, r := range rows {
		if w := lipgloss.Width(r.label) + 8; w > width {
			width = w
		}
	}
	if w := lipgloss.Width(title) + 6; w > width {
		width = w
	}
	if width > m.width-4 {
		width = m.width - 4
	}
	height := len(rows) + 2
	if m.moveStage == moveStageColumn {
		height++ // separator line above "Other board…"
	}
	if height > m.height-4 {
		height = m.height - 4
	}
	if height < 5 {
		height = 5
	}

	backdrop := m.popupBackdrop(m.popupReturnView)
	m.resetZones()

	origin := m.popupOrigin(width, height)
	popup := m.renderMovePopup(width, height, origin)
	return overlayAt(backdrop, popup, origin.x, origin.y)
}

func (m *Model) moveTitle() string {
	switch m.moveStage {
	case moveStageBoard:
		return "Move to board"
	case moveStageTargetColumn:
		return "Move to " + boardDisplayName(m.moveTargetBoard)
	default:
		return "Move ticket"
	}
}

func (m *Model) renderMovePopup(width, height int, origin point) string {
	innerWidth := width - 4
	if innerWidth < 10 {
		innerWidth = 10
	}

	// Content starts one row below the top border and one col in from the
	// left border plus the block's own left padding.
	rowY := origin.y + 1
	rowX := origin.x + 2

	// renderPanel clips content to its inner height. Registering a zone for a
	// row it drops would put a click target over the popup's bottom border and
	// the backdrop below it, so clicking apparently empty space would pick a
	// board that was never shown.
	visible := height - 2
	if visible < 0 {
		visible = 0
	}

	var lines []string
	for i, r := range m.moveRows {
		if r.isOther && len(lines) > 0 && len(lines) < visible {
			lines = append(lines, dimStyle.Render(strings.Repeat("─", innerWidth)))
		}
		if len(lines) >= visible {
			break
		}
		m.addZone(hitZone{
			kind: zoneMoveRow,
			x:    rowX,
			y:    rowY + len(lines),
			w:    innerWidth,
			h:    1,
			idx:  i,
		})
		lines = append(lines, m.renderMoveRow(r, i == m.moveIdx))
	}
	if len(m.moveRows) == 0 {
		lines = append(lines, dimStyle.Render("(no other boards)"))
	}

	content := lipgloss.NewStyle().PaddingLeft(1).Render(strings.Join(lines, "\n"))
	return renderPanel(m.moveTitle(), content, width, height, green, true)
}

func (m *Model) renderMoveRow(r moveRow, selected bool) string {
	marker := "  "
	if selected {
		marker = selectedMarker.Render("* ")
	}

	style := lipgloss.NewStyle()
	switch {
	case r.isStatus:
		style = style.Foreground(columnColor(r.status))
	case r.isOther:
		style = style.Foreground(midGray)
	}
	rendered := style.Render(r.label)
	if r.current {
		rendered += dimStyle.Render("  (current)")
	}
	return marker + rendered
}
