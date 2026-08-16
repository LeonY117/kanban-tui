package tui

import (
	"fmt"
	"os"
	"strings"

	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/LeonY117/kanban-tui/internal/model"
	"github.com/LeonY117/kanban-tui/internal/store"
)

// The board popup: what the ticket detail is to a ticket, this is to a board.
// Three stacked panels — the ticket-id prefix, the board's name, its
// description — with j/k between them and enter to edit the focused one, so
// editing a board is the same motion as editing a card.
//
// Read-first: it lands on the description, and the common case is remembering
// what a sprint covers rather than rewriting it. `r` from the picker is the
// exception, arriving with the name already open for typing.
//
// It describes whichever board it was opened over: the current one from the
// board view, the highlighted one from the picker, so a sprint's context can be
// read before switching into it.

// infoField values — also the j/k order, and the zone idx a click carries.
const (
	infoFieldMeta = iota // the metadata line: the ticket-id prefix
	infoFieldName        // the board's name
	infoFieldDesc        // the description
)

// The name panel holds one line inside a border; the description takes whatever
// the frame has left, down to a single line.
const (
	infoNameHeight = 3
	infoMinDesc    = 3
)

// boardExists reports whether a board's file is still on disk. Load can't
// answer this: a missing board reads as an empty one, which is what lets a
// removed sprint be written back into existence.
func boardExists(s *store.Store) bool {
	_, err := os.Stat(s.BoardPath())
	return err == nil
}

// enterInfo opens the board popup. name is a sprint name, or "" for the main
// board.
func (m *Model) enterInfo(name string) {
	board := m.board
	if name != m.sprintName {
		// Another board isn't in memory — pickerEntry carries only what
		// ListSprints returns, and that's the description's first line.
		s, err := boardStore(name)
		if err != nil {
			m.notice = "can't read that board: " + err.Error()
			return
		}
		if board, err = s.Load(); err != nil {
			m.notice = "can't read that board: " + err.Error()
			return
		}
	}

	// Its own return slot rather than popupReturnView: opened over the board
	// picker this popup has to close back onto the picker, while the picker
	// still needs popupReturnView to hold the view *it* was opened from. One
	// shared slot can't answer both, and overwriting it here left the picker
	// with no board to restore.
	if m.view != infoView {
		m.infoReturn = m.view
	}
	m.view = infoView
	m.infoBoard = name
	m.readInfoBoard(board)
	m.infoScroll = 0
	m.infoEditing = false
	// The description, not the prefix: this popup is opened to read far more
	// often than to rename, and enter should land where the reading is.
	m.infoField = infoFieldDesc
}

// readInfoBoard snapshots the fields the popup draws from a loaded board.
func (m *Model) readInfoBoard(board *model.Board) {
	m.infoText = board.Description
	m.infoName = boardDisplayName(m.infoBoard)
	m.infoPrefix = store.EffectivePrefix(board, m.infoBoard)
	m.infoCounts = store.CountByStatus(board)
}

// infoRenamable reports whether this board's name and prefix are editable at
// all. Main's are not: its directory is the root rather than a name, and its
// ids are bare numbers. Its two upper panels render dim and j/k skips them.
func (m *Model) infoRenamable() bool { return m.infoBoard != "" }

// infoFirstField is where j/k stops going up — the description alone on main.
func (m *Model) infoFirstField() int {
	if m.infoRenamable() {
		return infoFieldMeta
	}
	return infoFieldDesc
}

func (m *Model) moveInfoField(dir int) {
	m.infoField = min(infoFieldDesc, max(m.infoFirstField(), m.infoField+dir))
}

// startInfoEdit opens the editor for the focused panel.
func (m *Model) startInfoEdit() (tea.Model, tea.Cmd) {
	if m.infoBoard != "" && store.IsSprintArchived(m.infoBoard) {
		m.notice = "sprint " + m.infoBoard + " is archived — unarchive it to edit"
		return m, nil
	}
	switch m.infoField {
	case infoFieldMeta:
		m.infoPrefixIn = newInfoInput(m.infoPrefix, 4, 4)
		m.infoPrefixIn.Focus()
	case infoFieldName:
		m.infoNameIn = newInfoInput(m.infoName, 64, 28)
		m.infoNameIn.Focus()
	default:
		m.infoDesc = newDescArea(m.infoText)
		m.infoDesc.Focus()
	}
	m.infoEditing = true
	return m, textinput.Blink
}

// newInfoInput is one field of the popup, seeded with what it is replacing.
// width is capped well under the popup so a 64-character name can't push the
// panel border past the edge it registered its click zone at.
func newInfoInput(value string, charLimit, width int) textinput.Model {
	in := textinput.New()
	in.Prompt = ""
	in.CharLimit = charLimit
	in.Width = width
	in.SetValue(value)
	in.CursorEnd()
	return in
}

// startBoardRename opens the popup on a board with its name ready to type —
// what `r` means. Kept as its own entry point because the refusals are about
// renaming specifically: the popup itself opens happily on main and on archived
// sprints, which is how you read them.
func (m *Model) startBoardRename(name string, archived bool) (tea.Model, tea.Cmd) {
	if name == "" {
		m.notice = "main board can't be renamed"
		return m, nil
	}
	if archived {
		m.notice = "archived sprints are read-only — press u to unarchive"
		return m, nil
	}
	m.enterInfo(name)
	if m.view != infoView {
		return m, nil // enterInfo refused; it has already set the notice
	}
	m.infoField = infoFieldName
	return m.startInfoEdit()
}

func (m *Model) cancelInfoEdit() {
	m.infoEditing = false // discard, keep the popup open on what was saved
	m.infoNameIn.Blur()
	m.infoPrefixIn.Blur()
	m.infoDesc.Blur()
}

// saveInfoEdit writes the focused field back to its own board, which may not be
// the one this Model is sitting on.
func (m *Model) saveInfoEdit() {
	switch m.infoField {
	case infoFieldMeta:
		m.saveInfoPrefix()
	case infoFieldName:
		m.saveInfoName()
	default:
		m.saveInfoDesc()
	}
}

// saveInfoName renames the board — which changes what this popup is describing,
// and possibly which board the Model is sitting on.
func (m *Model) saveInfoName() {
	target := m.infoBoard
	newName := strings.TrimSpace(m.infoNameIn.Value())
	if newName == "" || newName == target {
		m.cancelInfoEdit()
		return
	}
	// An empty prefix means "leave it alone", which is UpdateSprint's own
	// reading of it — this field renames and nothing more.
	if err := store.UpdateSprint(target, newName, ""); err != nil {
		// Stay open on what was typed, reason in the footer: retyping a 40-char
		// name to fix one character would be the worse trade.
		m.notice = err.Error()
		return
	}
	m.followBoardRename(target, newName)
	m.notice = fmt.Sprintf("renamed %q to %q", target, newName)
}

// saveInfoPrefix retags the board's ticket ids. UpdateSprint refuses the whole
// change if any id it would mint is already issued, so a rejection leaves the
// board exactly as it was and the field open on what was typed.
func (m *Model) saveInfoPrefix() {
	next := strings.TrimSpace(m.infoPrefixIn.Value())
	if next == "" || strings.EqualFold(next, m.infoPrefix) {
		m.cancelInfoEdit()
		return
	}
	if err := store.UpdateSprint(m.infoBoard, m.infoBoard, next); err != nil {
		m.notice = err.Error()
		return
	}
	m.cancelInfoEdit()
	m.refreshInfoBoard()
	// Every short id on the board just changed, so anything drawing them is
	// stale — the cards behind the popup included.
	if m.infoBoard == m.sprintName {
		m.reload()
	}
	m.reloadPickerEntriesOn(m.infoBoard)
	m.notice = fmt.Sprintf("%q ids now carry %s", m.infoBoard, m.infoPrefix)
}

// followBoardRename re-points everything that knew the board by its old name:
// this popup's own subject, the live model when it was the board that moved,
// and the board list behind the popup.
func (m *Model) followBoardRename(oldName, newName string) {
	m.cancelInfoEdit()
	m.infoBoard = newName
	if m.sprintName == oldName {
		if err := m.switchBoard(newName); err != nil {
			m.err = err
			return
		}
	}
	m.refreshInfoBoard()
	m.reloadPickerEntriesOn(newName)
}

// saveInfoDesc writes the description back.
func (m *Model) saveInfoDesc() {
	text := m.infoDesc.Value()
	s, err := boardStore(m.infoBoard)
	if err != nil {
		m.notice = "couldn't save: " + err.Error()
		return
	}

	// The popup holds a snapshot, and a board this Model isn't sitting on can
	// be renamed, removed or rewritten by an agent while it is open. Re-read
	// before writing: Load reports a missing board as an empty one and a write
	// recreates its directory, so without this a save could resurrect a deleted
	// sprint as a board with a description and no tickets, or quietly overwrite
	// an agent's edit with text that predates it.
	if m.infoBoard != "" && !boardExists(s) {
		m.notice = boardDisplayName(m.infoBoard) + " no longer exists — esc to close"
		return
	}
	current, err := s.Load()
	if err != nil {
		m.notice = "couldn't save: " + err.Error()
		return
	}
	if current.Description != m.infoText {
		m.notice = boardDisplayName(m.infoBoard) + " changed underneath — esc and reopen"
		return
	}

	if err := s.SetDescription(text); err != nil {
		// The cap and the archived freeze both land here, and both are worth
		// reading rather than swallowing — the edit stays open so the text
		// isn't lost.
		m.notice = err.Error()
		return
	}
	m.infoText = strings.TrimSpace(text)
	m.cancelInfoEdit()
	if m.infoBoard == m.sprintName {
		m.board.Description = m.infoText
	}
	m.notice = "description saved"
}

func (m *Model) closeInfo() {
	m.view = m.infoReturn
	m.cancelInfoEdit()
}

func (m *Model) updateInfo(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if m.infoEditing {
		switch {
		case key.Matches(msg, keys.Esc):
			m.cancelInfoEdit()
			return m, nil
		case key.Matches(msg, keys.Enter):
			m.saveInfoEdit()
			return m, nil
		case key.Matches(msg, keys.Emoji):
			// Only the description takes one: a sprint name is letters, digits,
			// '_' and '-', and a prefix is four letters at most.
			if m.infoField == infoFieldDesc {
				return m.openEmojiPicker(emojiToInfoDesc)
			}
			return m, nil
		}
		var cmd tea.Cmd
		switch m.infoField {
		case infoFieldMeta:
			m.infoPrefixIn, cmd = m.infoPrefixIn.Update(msg)
		case infoFieldName:
			m.infoNameIn, cmd = m.infoNameIn.Update(msg)
		default:
			m.infoDesc, cmd = m.infoDesc.Update(msg)
		}
		return m, cmd
	}

	switch {
	case key.Matches(msg, keys.Quit):
		return m, tea.Quit
	case key.Matches(msg, keys.Esc), key.Matches(msg, keys.Info):
		m.closeInfo()
	case key.Matches(msg, keys.Enter), key.Matches(msg, keys.Edit):
		return m.startInfoEdit()
	case key.Matches(msg, keys.Down):
		m.moveInfoField(1)
	case key.Matches(msg, keys.Up):
		m.moveInfoField(-1)
	}
	return m, nil
}

// ─── Render ──────────────────────────────────────────────────────────

func (m *Model) viewInfo() string {
	popupWidth, popupHeight := m.infoPopupSize()
	backdrop := m.popupBackdrop(m.infoReturn)
	m.resetZones()
	origin := m.popupOrigin(popupWidth, popupHeight)
	popup := m.renderInfoPopup(popupWidth, popupHeight, origin)
	return overlayAt(backdrop, popup, origin.x, origin.y)
}

// infoPopupSize is the new-ticket form's size, because this is the same object
// filled with different fields — see formPopupSize.
func (m *Model) infoPopupSize() (width, height int) { return m.formPopupSize() }

// infoInnerWidth is the width the popup's inner panels render at.
func (m *Model) infoInnerWidth() int {
	w, _ := m.infoPopupSize()
	return max(10, w-4)
}

// renderInfoPopup lays the board out the way the new-ticket form lays a ticket
// out: a bare meta line, a one-line field, a description taking everything
// left, a help line, all inside one frame titled with what is being edited.
func (m *Model) renderInfoPopup(width, height int, origin point) string {
	innerWidth := m.infoInnerWidth()
	// Content starts one row below the frame's top border and one column in
	// from its left, plus the left pad every line below carries.
	rowX, rowY := origin.x+2, origin.y+1

	meta := m.renderInfoMeta(point{x: rowX, y: rowY})

	nameColor := softWhite
	if m.infoField == infoFieldName {
		nameColor = cyan
	}
	namePanel := renderPanel("Name", m.renderInfoName(innerWidth), innerWidth, infoNameHeight,
		m.inertOr(infoFieldName, nameColor), m.infoField == infoFieldName && m.infoRenamable())
	if m.infoRenamable() {
		m.addZone(hitZone{kind: zoneInfoField, x: rowX, y: rowY + 1, w: innerWidth, h: infoNameHeight, idx: infoFieldName})
	}

	descColor := softWhite
	if m.infoField == infoFieldDesc {
		descColor = cyan
	}
	// Frame borders (2) + meta (1) + name panel (3) + help (1) = 7. The
	// description takes the remainder, which is the point of a fixed-size box:
	// a board's context gets room to be read rather than a line and a half.
	descHeight := max(infoMinDesc, height-7)
	descPanel := renderPanel("Description", m.renderInfoDesc(innerWidth-2, descHeight-2),
		innerWidth, descHeight, descColor, m.infoField == infoFieldDesc)
	m.addZone(hitZone{kind: zoneInfoField, x: rowX, y: rowY + 4, w: innerWidth, h: descHeight, idx: infoFieldDesc})

	// lipgloss PaddingLeft on a multi-line block pads every line, so sub-panel
	// borders don't collide with the frame's left border.
	pad := lipgloss.NewStyle().PaddingLeft(1)
	content := strings.Join([]string{
		pad.Render(meta),
		pad.Render(namePanel),
		pad.Render(descPanel),
		pad.Render(m.infoHelpLine()),
	}, "\n")

	return renderPanel(m.infoFrameTitle(), content, width, height, cyan, true)
}

// infoFrameTitle names the board being edited, the way the new-ticket popup's
// frame says "New ticket". The archived tag rides here because it explains
// every refusal the fields below can give.
func (m *Model) infoFrameTitle() string {
	title := boardDisplayName(m.infoBoard)
	if m.infoBoard != "" && store.IsSprintArchived(m.infoBoard) {
		title += " [archived]"
	}
	return title
}

// inertOr dims a field main has nothing to put in it. Its directory is the root
// rather than a name and its ids are bare numbers, so neither the cursor nor
// the mouse is allowed to land there.
func (m *Model) inertOr(field int, color lipgloss.Color) lipgloss.Color {
	if field != infoFieldDesc && !m.infoRenamable() {
		return dimGray
	}
	return color
}

// renderInfoMeta is the popup's metadata line — the prefix new ticket ids
// carry, and the board's shape at a glance. It is unbordered and reverse-
// highlights when selected, the way the new-ticket form's assignee and tags do.
// While the prefix is being edited the counts give way to what the change would
// do to existing ids, the part that isn't obvious from typing two letters.
func (m *Model) renderInfoMeta(origin point) string {
	label := prefixLabel(m.infoPrefix)
	var prefix string
	switch {
	case !m.infoRenamable():
		prefix = dimStyle.Render(label)
	case m.infoEditing && m.infoField == infoFieldMeta:
		// The widget's own View, not a styled copy of its value: stacking a
		// style on it mangles the cursor, and the cursor is what separates
		// "typing here" from "selected".
		return m.infoPrefixIn.View() + m.infoIDHint()
	case m.infoField == infoFieldMeta:
		prefix = selectedFieldStyle.Render(label)
	default:
		prefix = lipgloss.NewStyle().Foreground(white).Bold(true).Render(label)
	}
	if m.infoRenamable() {
		m.addZone(hitZone{kind: zoneInfoField, x: origin.x, y: origin.y, w: lipgloss.Width(prefix), h: 1, idx: infoFieldMeta})
	}
	return prefix + "  " + formatCounts(m.infoCounts)
}

// infoHelpLine is the popup's own hint line, and where a refusal lands — the
// footer says the same thing, but a rename rejected mid-edit should say why
// next to the field that rejected it.
func (m *Model) infoHelpLine() string {
	if m.notice != "" {
		return lipgloss.NewStyle().Foreground(peach).Render(m.notice)
	}
	var parts []string
	switch {
	case m.infoEditing && m.infoField == infoFieldDesc:
		parts = []string{"enter: save", "shift+enter: new line", "esc: discard"}
	case m.infoEditing:
		parts = []string{"enter: save", "esc: discard"}
	default:
		parts = []string{"j/k: field", "enter: edit", "esc: close"}
	}
	// helpStyle pads a cell either side, so the budget is two short of the
	// interior.
	return helpStyle.Render(fitHints(strings.Join(parts, bulletSep), bulletSep, m.infoInnerWidth()-2))
}

// infoIDHint spells out what a changed prefix does to existing ids. Silent
// while the prefix is untouched.
func (m *Model) infoIDHint() string {
	next := strings.ToUpper(strings.TrimSpace(m.infoPrefixIn.Value()))
	if next == "" || next == strings.ToUpper(m.infoPrefix) {
		return ""
	}
	return dimStyle.Render(fmt.Sprintf("  %s1 → %s1", prefixLabel(m.infoPrefix), next))
}

func (m *Model) renderInfoName(width int) string {
	if m.infoEditing && m.infoField == infoFieldName {
		m.infoNameIn.Width = max(4, width-2)
		return m.infoNameIn.View()
	}
	return lipgloss.NewStyle().Bold(true).Foreground(white).Render(m.infoName)
}

func (m *Model) renderInfoDesc(width, height int) string {
	if m.infoEditing && m.infoField == infoFieldDesc {
		setDescWidth(&m.infoDesc, width)
		m.infoDesc.SetHeight(height)
		return m.infoDesc.View()
	}
	return m.renderInfoBody(width, height)
}

// renderInfoBody wraps and vertically clips the description, tracking its own
// scroll offset rather than borrowing the ticket detail's — the two can be open
// over each other and must not fight for one cursor.
func (m *Model) renderInfoBody(width, height int) string {
	if m.infoText == "" {
		return lipgloss.NewStyle().Foreground(subtle).
			Render("context about this board")
	}

	lines := strings.Split(wrapDesc(m.infoText, width), "\n")
	m.infoScrollMax = max(0, len(lines)-height)
	if m.infoScroll > m.infoScrollMax {
		m.infoScroll = m.infoScrollMax
	}
	lines = lines[m.infoScroll:]
	if len(lines) > height {
		lines = lines[:height]
	}
	return lipgloss.NewStyle().Foreground(softWhite).Render(strings.Join(lines, "\n"))
}

// scrollInfo walks the description under the wheel. j/k belong to the field
// cursor here, the way they do in the ticket detail.
func (m *Model) scrollInfo(dir int) {
	if m.infoEditing {
		return
	}
	m.infoScroll = min(m.infoScrollMax, max(0, m.infoScroll+dir))
}

// refreshInfoBoard re-reads the open popup's board. The board being described
// may not be the one this Model watches, so it is read directly.
func (m *Model) refreshInfoBoard() {
	s, err := boardStore(m.infoBoard)
	if err != nil {
		return
	}
	if m.infoBoard != "" && !boardExists(s) {
		return
	}
	if board, err := s.Load(); err == nil {
		m.readInfoBoard(board)
	}
}

// refreshInfoText re-reads the open popup on the board tick, so an agent's edit
// shows up the way a ticket's does rather than leaving a snapshot on screen.
//
// Never while editing: the text on screen is then the user's, and replacing it
// under the cursor would lose what they typed. A save from stale text is caught
// by saveInfoDesc's re-read instead.
func (m *Model) refreshInfoText() {
	if m.view != infoView || m.infoEditing {
		return
	}
	m.refreshInfoBoard()
}
