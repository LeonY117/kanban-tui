package tui

import (
	"os"
	"strings"

	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/LeonY117/kanban-tui/internal/store"
)

// The board-description popup. Read-first: `i` opens it to show what a board is
// for, and `e` inside it switches to editing — the common case is remembering
// what a sprint covers, not rewriting it, and a read-only landing means a
// stray keystroke can't touch 2000 characters.
//
// It describes whichever board it was opened over: the current one from the
// board view, the highlighted one from the picker, so a sprint's context can be
// read before switching into it.

// boardExists reports whether a board's file is still on disk. Load can't
// answer this: a missing board reads as an empty one, which is what lets a
// removed sprint be written back into existence.
func boardExists(s *store.Store) bool {
	_, err := os.Stat(s.BoardPath())
	return err == nil
}

// enterInfo opens the description popup for a board. name is a sprint name, or
// "" for the main board.
func (m *Model) enterInfo(name string) {
	desc := m.board.Description
	if name != m.sprintName {
		// Another board's description isn't in memory — pickerEntry carries
		// only what ListSprints returns, and that's the first line.
		s, err := boardStore(name)
		if err != nil {
			m.notice = "can't read that board: " + err.Error()
			return
		}
		board, err := s.Load()
		if err != nil {
			m.notice = "can't read that board: " + err.Error()
			return
		}
		desc = board.Description
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
	m.infoText = desc
	m.infoScroll = 0
	m.infoEditing = false
}

// startInfoEdit swaps the popup from reading to editing.
func (m *Model) startInfoEdit() {
	if archived := m.infoBoard != "" && store.IsSprintArchived(m.infoBoard); archived {
		m.notice = "sprint " + m.infoBoard + " is archived — unarchive it to edit"
		return
	}
	m.infoDesc = newDescArea(m.infoText)
	m.infoDesc.Focus()
	m.infoEditing = true
}

// saveInfoEdit writes the edited description back to its own board, which may
// not be the one this Model is sitting on.
func (m *Model) saveInfoEdit() {
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
	m.infoEditing = false
	if m.infoBoard == m.sprintName {
		m.board.Description = m.infoText
	}
	m.notice = "description saved"
}

func (m *Model) closeInfo() {
	m.view = m.infoReturn
	m.infoEditing = false
}

func (m *Model) updateInfo(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if m.infoEditing {
		switch {
		case key.Matches(msg, keys.Esc):
			m.infoEditing = false // discard, keep the popup open on the saved text
			return m, nil
		case key.Matches(msg, keys.Enter):
			m.saveInfoEdit()
			return m, nil
		}
		var cmd tea.Cmd
		m.infoDesc, cmd = m.infoDesc.Update(msg)
		return m, cmd
	}

	switch {
	case key.Matches(msg, keys.Quit):
		return m, tea.Quit
	case key.Matches(msg, keys.Esc), key.Matches(msg, keys.Info):
		m.closeInfo()
	case key.Matches(msg, keys.Enter), key.Matches(msg, keys.Edit):
		m.startInfoEdit()
	case key.Matches(msg, keys.Down):
		if m.infoScroll < m.infoScrollMax {
			m.infoScroll++
		}
	case key.Matches(msg, keys.Up):
		if m.infoScroll > 0 {
			m.infoScroll--
		}
	}
	return m, nil
}

// ─── Render ──────────────────────────────────────────────────────────

func (m *Model) viewInfo() string {
	popupWidth, popupHeight := m.infoPopupSize()
	backdrop := m.popupBackdrop(m.infoReturn)
	m.resetZones()
	origin := m.popupOrigin(popupWidth, popupHeight)
	popup := m.renderInfoPopup(popupWidth, popupHeight)
	return overlayAt(backdrop, popup, origin.x, origin.y)
}

// infoPopupSize is wider than the board list — this holds prose, not names, and
// a 2000-character description at 40 columns is a column of confetti.
func (m *Model) infoPopupSize() (width, height int) {
	const (
		minWidth = 40
		maxWidth = 76
	)
	width = min(maxWidth, max(minWidth, m.width-8))
	if width > m.width-4 {
		width = m.width - 4
	}

	bodyLines := 1
	if m.infoText != "" {
		bodyLines = len(strings.Split(wrapDesc(m.infoText, width-4), "\n"))
	}
	height = bodyLines + 4 // border (2), a blank, the hint line
	if height > m.height-4 {
		height = m.height - 4
	}
	// A description is prose, and a box that hugs one short line reads as an
	// error message rather than a place to write.
	if height < 12 {
		height = 12
	}
	return width, height
}

func (m *Model) renderInfoPopup(width, height int) string {
	innerWidth := max(10, width-4)
	bodyHeight := max(1, height-4)

	// The board's name titles the box rather than heading its contents: the
	// badge style's padding would set a heading one column right of the prose
	// under it, and "which board is this" belongs on the frame anyway.
	title := boardDisplayName(m.infoBoard)
	if m.infoBoard != "" && store.IsSprintArchived(m.infoBoard) {
		title += " [archived]"
	}

	var body, hint string
	if m.infoEditing {
		setDescWidth(&m.infoDesc, innerWidth)
		m.infoDesc.SetHeight(bodyHeight)
		body = m.infoDesc.View()
		hint = dimStyle.Render("enter save · esc discard · shift+enter newline")
	} else {
		body = m.renderInfoBody(innerWidth, bodyHeight)
		hint = dimStyle.Render("enter edit · esc close")
	}

	// Pad the body out to its full height so the hint lands on the last line
	// inside the border rather than floating under the text.
	lines := strings.Split(body, "\n")
	for len(lines) < bodyHeight {
		lines = append(lines, "")
	}
	content := strings.Join(append(lines[:bodyHeight], "", hint), "\n")
	return renderPanel(title, content, width, height, cyan, true)
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

// refreshInfoText re-reads the open popup's description on the board tick, so
// an agent's edit shows up the way a ticket's does rather than leaving a
// snapshot on screen. The board being described may not be the one this Model
// watches, so it is read directly.
//
// Never while editing: the text on screen is then the user's, and replacing it
// under the cursor would lose what they typed. A save from stale text is caught
// by saveInfoEdit's re-read instead.
func (m *Model) refreshInfoText() {
	if m.view != infoView || m.infoEditing {
		return
	}
	s, err := boardStore(m.infoBoard)
	if err != nil {
		return
	}
	if m.infoBoard != "" && !boardExists(s) {
		return
	}
	if board, err := s.Load(); err == nil {
		m.infoText = board.Description
	}
}
