package tui

import (
	tea "github.com/charmbracelet/bubbletea"
)

// point is a top-left origin in terminal cells.
type point struct{ x, y int }

type zoneKind int

const (
	zoneTicket zoneKind = iota // a ticket row/card inside a column
	zoneColumn                 // a column panel (its empty space)
	zoneField                  // a detail field panel: 0 meta, 1 title, 2 desc
	zoneArchiveRow
	zoneArchiveDetail
	zonePickerRow
	zoneMoveRow
)

// hitZone is a rectangle registered during render so mouse events can be
// mapped back to whatever was drawn there. Zones are rebuilt every frame and
// tested newest-first, so a popup's zones win over the backdrop it covers.
type hitZone struct {
	kind       zoneKind
	x, y, w, h int
	col        int // column index, for zoneTicket / zoneColumn
	idx        int // ticket index, field index or row index
}

func (m *Model) resetZones() {
	m.zones = m.zones[:0]
}

func (m *Model) addZone(z hitZone) {
	if z.w <= 0 || z.h <= 0 {
		return
	}
	m.zones = append(m.zones, z)
}

func (m *Model) addTicketZone(col, idx, x, y, w, h int) {
	m.addZone(hitZone{kind: zoneTicket, x: x, y: y, w: w, h: h, col: col, idx: idx})
}

// zoneAt returns the most recently registered zone covering (x, y).
func (m *Model) zoneAt(x, y int) *hitZone {
	for i := len(m.zones) - 1; i >= 0; i-- {
		z := &m.zones[i]
		if x >= z.x && x < z.x+z.w && y >= z.y && y < z.y+z.h {
			return z
		}
	}
	return nil
}

func (m *Model) updateMouse(msg tea.MouseMsg) (tea.Model, tea.Cmd) {
	switch msg.Button {
	case tea.MouseButtonWheelUp:
		return m.mouseScroll(msg, -1)
	case tea.MouseButtonWheelDown:
		return m.mouseScroll(msg, 1)
	case tea.MouseButtonLeft:
		if msg.Action == tea.MouseActionPress {
			return m.mouseClick(msg)
		}
	}
	return m, nil
}

// mouseScroll routes wheel input to the content under the pointer.
func (m *Model) mouseScroll(msg tea.MouseMsg, dir int) (tea.Model, tea.Cmd) {
	z := m.zoneAt(msg.X, msg.Y)
	if z == nil {
		// Outside any registered zone: fall back to the view's main list.
		if m.view == detailView {
			m.scrollDescription(dir)
		}
		return m, nil
	}

	switch z.kind {
	case zoneTicket, zoneColumn:
		m.scrollColumn(z.col, dir)
	case zoneField:
		if z.idx == 2 {
			m.scrollDescription(dir)
		}
	case zoneArchiveRow:
		m.moveArchiveCursor(dir)
		m.descScroll = 0
	case zoneArchiveDetail:
		m.scrollDescription(dir)
	case zonePickerRow:
		m.movePickerCursor(dir)
	case zoneMoveRow:
		m.moveMoveCursor(dir)
	}
	return m, nil
}

// wheelNotchesPerTicket is how many wheel events one ticket step costs. A
// ticket is several lines tall, so matching the one-line-per-notch feel of
// description scrolling means banking a few notches first.
const wheelNotchesPerTicket = 3

// scrollColumn advances the cursor of a column by one ticket, once enough
// wheel notches have banked. Columns other than the focused one take focus
// first, so the wheel acts where the pointer is.
func (m *Model) scrollColumn(col, dir int) {
	if col < 0 || col > 4 {
		return
	}
	if col != m.focusedCol && m.view == boardView {
		m.focusedCol = col
	}
	if col != m.focusedCol {
		return
	}

	// A change of direction starts the count over, so a flick back up
	// responds immediately instead of spending banked notches.
	if m.wheelAccum != 0 && (m.wheelAccum > 0) != (dir > 0) {
		m.wheelAccum = 0
	}
	m.wheelAccum += dir
	if m.wheelAccum > -wheelNotchesPerTicket && m.wheelAccum < wheelNotchesPerTicket {
		return
	}
	m.wheelAccum = 0

	m.moveCursor(dir)
	if m.view == splitView || m.view == detailView {
		m.refreshDetailEditors()
	}
}

// scrollDescription scrolls the description body by one line. While the
// description is being edited the textarea owns the viewport, so the wheel
// walks its cursor instead.
func (m *Model) scrollDescription(dir int) {
	if m.editDesc.Focused() {
		if dir < 0 {
			m.editDesc.CursorUp()
		} else {
			m.editDesc.CursorDown()
		}
		return
	}
	m.descScroll += dir
	if m.descScroll > m.descScrollMax {
		m.descScroll = m.descScrollMax
	}
	if m.descScroll < 0 {
		m.descScroll = 0
	}
}

func (m *Model) mouseClick(msg tea.MouseMsg) (tea.Model, tea.Cmd) {
	m.notice = ""
	z := m.zoneAt(msg.X, msg.Y)
	if z == nil {
		return m, nil
	}

	// Clicking away from a field in progress commits it rather than losing it.
	if m.editTitle.Focused() || m.editDesc.Focused() {
		m.editTitle.Blur()
		m.editDesc.Blur()
		m.saveEdit()
	}

	switch z.kind {
	case zoneTicket:
		m.focusedCol = z.col
		m.cursors[z.col] = z.idx
		m.descScroll = 0
		if m.view == splitView {
			m.splitFocus = 0
			m.refreshDetailEditors()
		}
	case zoneColumn:
		m.focusedCol = z.col
		if m.view == splitView {
			m.splitFocus = 0
			m.refreshDetailEditors()
		}
	case zoneField:
		if m.view == splitView {
			m.splitFocus = 1
		}
		m.editField = z.idx
	case zoneArchiveRow:
		m.archiveCursor = z.idx
		m.descScroll = 0
	case zonePickerRow:
		m.pickerIdx = z.idx
		return m.pickerActivate()
	case zoneMoveRow:
		m.moveIdx = z.idx
		return m.moveActivate()
	}
	return m, nil
}
