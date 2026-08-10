package tui

import (
	"github.com/charmbracelet/bubbles/textarea"
	"github.com/charmbracelet/bubbles/textinput"
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
	zoneSettingsRow
	zoneSettingsTab
	zoneTagRow
	zoneMetaField   // one field inside a detail Info panel: 0 status, 1 assign, 2 tags
	zoneAddField    // one field of the new-ticket popup, an addFocus* value
	zoneRenameField // one input of the sprint rename form, a renameFocus* value
	zoneBoardBadge  // the board name in the footer — opens the board description
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

// clickTarget is what a click landed on, stripped of where it was drawn — the
// same row keeps its identity when the list scrolls under it.
type clickTarget struct {
	kind zoneKind
	col  int
	idx  int
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
		// The wheel acts on the pane it is over, which means focusing that pane
		// first — scrolling one list while the cursor walks the other one is
		// the kind of thing that looks like a rendering bug.
		m.move.pane = movePane(z.col)
		m.moveMoveCursor(dir)
	case zoneTagRow:
		m.moveTagPickerCursor(dir)
	case zoneSettingsRow, zoneSettingsTab:
		m.moveSettingsCursor(dir)
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

// mouseClick routes a left click to whatever was drawn under it.
//
// Every list here selects on the first click and acts on a second click of the
// row already under the cursor (Leon, 2026-08-06). One click that both moved
// the selection and committed meant the mouse could never be used to look
// around: a misjudged row switched board, moved a ticket or started capturing
// keys before you could see what you had picked. The settings list already
// worked this way; the rest were the exception.
func (m *Model) mouseClick(msg tea.MouseMsg) (tea.Model, tea.Cmd) {
	m.notice = ""
	z := m.zoneAt(msg.X, msg.Y)
	if z == nil {
		return m, nil
	}

	// "A second click of the same row" has to mean a second *click*, not a
	// click on whatever the surface happened to open with selected. The move
	// popup opens with the ticket's own column under the cursor, so reading the
	// selection alone made the very first click commit the move.
	target := clickTarget{kind: z.kind, col: z.col, idx: z.idx}
	repeat := target == m.lastClick
	m.lastClick = target

	// A click inside the editor already open is a no-op. Falling through would
	// blur it, save, and reopen it — the same text, but with the cursor thrown
	// back to where the editor puts it rather than where it was left.
	if m.editTitle.Focused() || m.editDesc.Focused() {
		if z.kind == zoneField && z.idx == m.editField {
			return m, nil
		}
		// Clicking away from a field in progress commits it rather than losing it.
		m.editTitle.Blur()
		m.editDesc.Blur()
		m.saveEdit()
	}

	switch z.kind {
	case zoneTicket:
		// In the split, a card counts as already selected only while the list
		// holds the focus: clicking the list back from the detail pane is the
		// click that selects, not the one that opens.
		selected := repeat && m.focusedCol == z.col && m.cursors[z.col] == z.idx &&
			(m.view != splitView || m.splitFocus == 0)
		m.focusedCol = z.col
		m.cursors[z.col] = z.idx
		m.descScroll = 0
		if m.view == splitView {
			m.splitFocus = 0
		}
		m.refreshDetailIfOpen()
		if selected {
			return m.openSelectedTicket()
		}
	case zoneColumn:
		m.focusedCol = z.col
		if m.view == splitView {
			m.splitFocus = 0
			m.refreshDetailEditors()
		}
	case zoneField:
		// In the split, a panel counts as already selected only while the
		// detail side holds the focus — clicking it back from the list is the
		// click that selects.
		already := repeat && m.editField == z.idx && (m.view != splitView || m.splitFocus == 1)
		if m.view == splitView {
			m.splitFocus = 1
		}
		m.editField = z.idx
		if already {
			return m.editFocusedField()
		}
	case zoneMetaField:
		already := repeat && m.editField == 0 && m.metaIdx == z.idx &&
			(m.view != splitView || m.splitFocus == 1)
		if m.view == splitView {
			m.splitFocus = 1
		}
		m.editField = 0
		m.metaIdx = z.idx
		if already {
			return m.editMetaField()
		}
	case zoneArchiveRow:
		already := repeat && m.archiveCursor == z.idx
		m.archiveCursor = z.idx
		m.descScroll = 0
		if already {
			// enter's job here: a row borrowed from another board's archive
			// follows itself home. A local row has nothing to open, so this
			// does nothing — which is the same answer enter gives.
			m.jumpToForeignArchive()
		}
	case zoneBoardBadge:
		m.enterInfo(m.sprintName)
	case zonePickerRow:
		if repeat && m.pickerIdx == z.idx {
			return m.pickerActivate()
		}
		m.pickerIdx = z.idx
	case zoneMoveRow:
		return m.moveClick(z, repeat)
	case zoneTagRow:
		if repeat && m.tags.idx == z.idx {
			return m.tagPickerActivate()
		}
		m.tags.idx = z.idx
	case zoneAddField:
		// The description is the one field with two states — selected, then
		// being typed into — so a second click on it opens the editor, which is
		// what enter does there.
		if repeat && m.addFocusIdx == z.idx && z.idx == addFocusDesc {
			m.addDesc.Focus()
			m.addDescEditing = true
			return m, textarea.Blink
		}
		m.focusAddField(z.idx)
	case zoneRenameField:
		m.setRenameFocus(z.idx)
	case zoneSettingsRow:
		// Where this rule started: a single click that began key capture
		// swallowed the next thing the user pressed.
		if repeat && m.settings.idx == z.idx {
			return m.settingsActivate()
		}
		m.settings.idx = z.idx
		m.settings.notice = ""
	case zoneSettingsTab:
		m.setSettingsSection(settingsSection(z.idx))
	}
	return m, nil
}

// editFocusedField opens the editor for the detail panel the cursor is on —
// what enter and `e` do there. The Info panel is left out: its three fields
// carry their own zones, so a click that means "edit the status" lands on the
// status rather than on the panel around it.
func (m *Model) editFocusedField() (tea.Model, tea.Cmd) {
	if !m.guardMutate() {
		return m, nil
	}
	switch m.editField {
	case 1:
		m.editTitle.Focus()
		return m, textinput.Blink
	case 2:
		m.editDesc.Focus()
		return m, textarea.Blink
	}
	return m, nil
}

// openSelectedTicket is the second click on an already-selected card, and does
// what enter does on the surface it was clicked on: the board opens the split,
// the split's list hands over to its detail pane, and the zoomed column opens
// the full detail view. A borrowed card follows itself home first, exactly as
// enter would.
func (m *Model) openSelectedTicket() (tea.Model, tea.Cmd) {
	if m.jumpToForeign() {
		return m, nil
	}
	if m.selectedTicket() == nil {
		return m, nil
	}
	switch m.view {
	case boardView:
		m.enterSplit()
	case splitView:
		m.splitFocus = 1
		m.refreshDetailEditors()
	case columnView:
		return m.enterDetail()
	}
	return m, nil
}
