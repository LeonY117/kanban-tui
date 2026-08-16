package tui

import (
	"fmt"
	"os"
	"strings"

	"github.com/charmbracelet/x/ansi"

	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/LeonY117/kanban-tui/internal/model"
	"github.com/LeonY117/kanban-tui/internal/store"
)

// The board popup: what the new-ticket form is to a ticket, this is to a board.
// The same frame and the same size, holding a metadata line, the board's name
// beside the prefix its ids carry, and its description. j/k walk the rows and
// h/l cross the name row, so editing a board is the same motion as editing a
// card.
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
	infoFieldPrefix = iota // the ticket-id prefix new cards here will carry
	infoFieldName          // the board's name
	infoFieldDesc          // the description
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
	// One click selects, a second acts — but lastClick outlives the popup, so
	// a reopened popup could match a click from the last visit and open an
	// editor on the very first press.
	m.lastClick = clickTarget{}
	m.infoBoard = name
	m.readInfoBoard(board)
	m.infoScroll = 0
	m.infoEditing = false
	// The description, not the prefix: this popup is opened to read far more
	// often than to rename, and enter should land where the reading is.
	m.infoField = infoFieldDesc
}

func (m *Model) readInfoBoard(board *model.Board) {
	m.infoText = board.Description
	m.infoName = boardDisplayName(m.infoBoard)
	m.infoPrefix = store.EffectivePrefix(board, m.infoBoard)
	m.infoCounts = store.CountByStatus(board)
}

// infoRenamable reports whether this board has a name and a prefix to change.
// Main doesn't: its directory is the root rather than a name, and its ids are
// bare numbers. Its two fields still take the cursor — they just draw dim and
// refuse the edit. Skipping them instead left j/k doing nothing at all on the
// board the TUI opens on, which reads as a broken popup rather than a fixed one.
func (m *Model) infoRenamable() bool { return m.infoBoard != "" }

func mainFieldRefusal(field int) string {
	if field == infoFieldPrefix {
		return "main's ids are bare numbers — only sprints carry a prefix"
	}
	return "main is the root board — only sprints can be renamed"
}

// moveInfoField walks j/k down the popup. The name and prefix share a row and
// so count as one stop, with h/l choosing between them.
//
// Past the last field, down keeps going into the description's own text. The
// popup is a fixed size now, so a description longer than the box is clipped —
// and with j/k spent on the fields there would otherwise be no key at all that
// reaches the rest of it, which on an archived sprint (where enter refuses)
// would put the tail out of reach entirely.
func (m *Model) moveInfoField(dir int) {
	if m.infoField == infoFieldDesc {
		if dir > 0 && m.infoScroll < m.infoScrollMax {
			m.infoScroll++
			return
		}
		if dir < 0 && m.infoScroll > 0 {
			m.infoScroll--
			return
		}
	}
	if dir > 0 {
		m.infoField = infoFieldDesc
		return
	}
	if m.infoField == infoFieldDesc {
		m.infoField = infoFieldName
	}
}

func (m *Model) moveInfoAcross(dir int) {
	if m.infoField == infoFieldDesc {
		return
	}
	if dir < 0 {
		m.infoField = infoFieldName
		return
	}
	m.infoField = infoFieldPrefix
}

func (m *Model) startInfoEdit() (tea.Model, tea.Cmd) {
	if m.infoBoard != "" && store.IsSprintArchived(m.infoBoard) {
		m.notice = "sprint " + m.infoBoard + " is archived — unarchive it to edit"
		return m, nil
	}
	if !m.infoRenamable() && m.infoField != infoFieldDesc {
		m.notice = mainFieldRefusal(m.infoField)
		return m, nil
	}
	switch m.infoField {
	case infoFieldPrefix:
		m.infoPrefixInput = newInfoInput(m.infoPrefix, 4, 4)
		m.infoPrefixInput.Focus()
	case infoFieldName:
		m.infoNameInput = newInfoInput(m.infoName, 64, 28)
		m.infoNameInput.Focus()
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

func (m *Model) cancelInfoEdit() {
	m.infoEditing = false // discard, keep the popup open on what was saved
	m.infoNameInput.Blur()
	m.infoPrefixInput.Blur()
	m.infoDesc.Blur()
}

// saveInfoEdit writes the focused field back to its own board, which may not be
// the one this Model is sitting on.
func (m *Model) saveInfoEdit() {
	switch m.infoField {
	case infoFieldPrefix:
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
	oldName := m.infoBoard
	newName := strings.TrimSpace(m.infoNameInput.Value())
	if newName == "" || newName == oldName {
		m.cancelInfoEdit()
		return
	}
	// An empty prefix means "leave it alone", which is UpdateSprint's own
	// reading of it — this field renames and nothing more.
	if err := store.UpdateSprint(oldName, newName, ""); err != nil {
		// Stay open on what was typed, reason in the footer: retyping a 40-char
		// name to fix one character would be the worse trade.
		m.notice = err.Error()
		return
	}
	m.followBoardRename(oldName, newName)
	m.notice = fmt.Sprintf("renamed %q to %q", oldName, newName)
}

// saveInfoPrefix retags the board's ticket ids. UpdateSprint refuses the whole
// change if any id it would mint is already issued, so a rejection leaves the
// board exactly as it was and the field open on what was typed.
func (m *Model) saveInfoPrefix() {
	newPrefix := strings.TrimSpace(m.infoPrefixInput.Value())
	if newPrefix == "" || strings.EqualFold(newPrefix, m.infoPrefix) {
		m.cancelInfoEdit()
		return
	}
	if err := store.UpdateSprint(m.infoBoard, m.infoBoard, newPrefix); err != nil {
		m.notice = err.Error()
		return
	}
	m.cancelInfoEdit()
	m.refreshInfoBoard()
	// Every short id on the board just changed, so anything drawing them is
	// stale — the cards behind the popup included, and the archive browser's
	// rows, which hold ids this retag rewrote.
	if m.infoBoard == m.sprintName {
		m.reload()
		m.returnToBoardIfDerived()
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
		m.returnToBoardIfDerived()
	}
	m.refreshInfoBoard()
	m.reloadPickerEntriesOn(newName)
}

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

// returnToBoardIfDerived sends the popup home to the board when the write it
// just made invalidated what the view behind it was showing. switchBoard resets
// the cursors and drops the archive cache, and a retag rewrites every short id
// including the archived ones — so closing back into the split, detail, column
// or archive view lands on state captured before the write: a detail pane whose
// editors still point at the ticket that *was* selected, so the next save writes
// to a card nobody is looking at, or an archive rendered empty.
//
// The picker settled this the same way — pickerActivate lands on the board after
// a switch — and it is the honest answer: the board you were looking at moved.
// The picker itself is exempt; it is reloaded explicitly and closing back onto it
// is the point of opening the popup from there.
func (m *Model) returnToBoardIfDerived() {
	switch m.infoReturn {
	case splitView, detailView, columnView, archiveView:
		m.infoReturn = boardView
		// Every route back into an editing view re-seeds these, so this is
		// belt and braces — but a ticket id left pointing at a selection the
		// write just reset is exactly the state the next reader has to prove
		// harmless, and re-seeding costs a line.
		m.refreshDetailEditors()
	}
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
		case infoFieldPrefix:
			m.infoPrefixInput, cmd = m.infoPrefixInput.Update(msg)
		case infoFieldName:
			m.infoNameInput, cmd = m.infoNameInput.Update(msg)
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
	case key.Matches(msg, keys.Left):
		m.moveInfoAcross(-1)
	case key.Matches(msg, keys.Right):
		m.moveInfoAcross(1)
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

	meta := m.renderInfoMeta()
	nameRow := m.renderInfoNameRow(innerWidth, point{x: rowX, y: rowY + 1})

	descColor := softWhite
	if m.infoField == infoFieldDesc {
		descColor = cyan
	}
	// Frame borders (2) + meta (1) + name panel (3) + help (1) = 7. The
	// description takes the remainder, which is the point of a fixed-size box:
	// a board's context gets room to be read rather than a line and a half.
	descHeight := max(infoMinDesc, height-7)
	// Body before title: rendering it is what measures the overflow, and the
	// title is where the overflow gets announced. A fixed-size box clips a long
	// description silently otherwise — the text just stops, with nothing saying
	// there is more below.
	descBody := m.renderInfoDesc(innerWidth-2, descHeight-2)
	descPanel := renderPanel(m.descPanelTitle(), descBody,
		innerWidth, descHeight, descColor, m.infoField == infoFieldDesc)
	m.addZone(hitZone{kind: zoneInfoField, x: rowX, y: rowY + 4, w: innerWidth, h: descHeight, idx: infoFieldDesc})

	// lipgloss PaddingLeft on a multi-line block pads every line, so sub-panel
	// borders don't collide with the frame's left border.
	pad := lipgloss.NewStyle().PaddingLeft(1)
	content := strings.Join([]string{
		pad.Render(meta),
		pad.Render(nameRow),
		pad.Render(descPanel),
		pad.Render(m.infoHelpLine()),
	}, "\n")

	return renderPanel(m.infoFrameTitle(), content, width, height, cyan, true)
}

func (m *Model) renderInfoNameRow(innerWidth int, origin point) string {
	nameWidth, prefixWidth := infoRowSplit(innerWidth)
	return lipgloss.JoinHorizontal(lipgloss.Top,
		m.renderInfoPanelAt("Name", infoFieldName, m.renderInfoName(nameWidth), nameWidth, origin),
		m.renderInfoPanelAt("Prefix", infoFieldPrefix, m.renderInfoPrefixBox(), prefixWidth,
			point{x: origin.x + nameWidth, y: origin.y}),
	)
}

// infoRowSplit divides the name row roughly 70/30. The prefix is four
// characters at most, but its box still has to hold the word "Prefix", so it
// takes a floor rather than a strict share of a narrow popup.
func infoRowSplit(innerWidth int) (nameWidth, prefixWidth int) {
	prefixWidth = min(max(10, innerWidth*3/10), innerWidth/2)
	return innerWidth - prefixWidth, prefixWidth
}

// Focus is drawn even on main, whose fields hold nothing to change: the cursor
// still walks through them, and a highlight that refused to move would be the
// harder thing to explain. The dim value is what says there is nothing here.
func (m *Model) renderInfoPanelAt(title string, field int, content string, width int, origin point) string {
	focused := m.infoField == field
	color := softWhite
	if focused {
		color = cyan
	}
	m.addZone(hitZone{kind: zoneInfoField, x: origin.x, y: origin.y, w: width, h: infoNameHeight, idx: field})
	return renderPanel(title, content, width, infoNameHeight, color, focused)
}

// infoValueStyle is how a field's stored value is drawn: dim when the board has
// nothing to put there, so main reads as fixed rather than as empty.
func (m *Model) infoValueStyle() lipgloss.Style {
	if !m.infoRenamable() {
		return lipgloss.NewStyle().Foreground(dimGray)
	}
	return lipgloss.NewStyle().Bold(true).Foreground(white)
}

// renderInfoPrefixBox is the prefix field. The id preview lives on the meta
// line rather than in here: this box is four characters wide in spirit, and
// `KA1 → TL1` beside them would be the tail wagging the dog.
func (m *Model) renderInfoPrefixBox() string {
	if m.infoEditing && m.infoField == infoFieldPrefix {
		return m.infoPrefixInput.View()
	}
	return m.infoValueStyle().Render(prefixLabel(m.infoPrefix))
}

// descPanelTitle says when there is more text than the box shows. Only the
// panel's own frame can carry it: the body is the clipped text itself, and a
// line spent on a marker inside would be a line of description lost.
func (m *Model) descPanelTitle() string {
	if m.infoScrollMax > 0 && !m.infoEditing {
		return fmt.Sprintf("Description  %d/%d", m.infoScroll+1, m.infoScrollMax+1)
	}
	return "Description"
}

// The archived tag rides on the frame because it explains every refusal the
// fields below can give.
func (m *Model) infoFrameTitle() string {
	title := boardDisplayName(m.infoBoard)
	if m.infoBoard != "" && store.IsSprintArchived(m.infoBoard) {
		title += " [archived]"
	}
	return title
}

// While the prefix is being edited, the counts give way to what the change
// would do to existing ids — the part that isn't obvious from typing two
// letters, and more than fits beside a four-character box.
func (m *Model) renderInfoMeta() string {
	if m.infoEditing && m.infoField == infoFieldPrefix {
		if hint := m.infoIDHint(); hint != "" {
			return hint
		}
	}
	return formatCounts(m.infoCounts)
}

// infoHelpLine is the popup's own hint line, and where a refusal lands — the
// footer says the same thing, but a rename rejected mid-edit should say why
// next to the field that rejected it.
func (m *Model) infoHelpLine() string {
	if m.notice != "" {
		// Truncated, not clipped: renderPanel cuts at the panel edge with no
		// mark, and a store refusal puts its instruction last — "run `kanban
		// sprints unarchive alpha`" is exactly what falls off.
		return lipgloss.NewStyle().Foreground(peach).
			Render(ansi.Truncate(m.notice, m.infoInnerWidth()-2, "…"))
	}
	var parts []string
	switch {
	case m.infoEditing && m.infoField == infoFieldDesc:
		parts = []string{"enter: save", "shift+enter: new line", "esc: discard"}
	case m.infoEditing:
		parts = []string{"enter: save", "esc: discard"}
	default:
		parts = []string{"j/k/h/l: field", "enter: edit", "esc: close"}
	}
	// helpStyle pads a cell either side, so the budget is two short of the
	// interior.
	return helpStyle.Render(fitHints(strings.Join(parts, bulletSep), bulletSep, m.infoInnerWidth()-2))
}

// infoIDHint spells out what a changed prefix does to existing ids. Silent
// while the prefix is untouched.
func (m *Model) infoIDHint() string {
	newPrefix := strings.ToUpper(strings.TrimSpace(m.infoPrefixInput.Value()))
	if newPrefix == "" || newPrefix == strings.ToUpper(m.infoPrefix) {
		return ""
	}
	return dimStyle.Render(fmt.Sprintf("  %s1 → %s1", prefixLabel(m.infoPrefix), newPrefix))
}

func (m *Model) renderInfoName(width int) string {
	if m.infoEditing && m.infoField == infoFieldName {
		m.infoNameInput.Width = max(4, width-2)
		return m.infoNameInput.View()
	}
	return m.infoValueStyle().Render(m.infoName)
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
