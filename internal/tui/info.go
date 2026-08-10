package tui

import (
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

	if m.view != infoView {
		m.popupReturnView = m.view
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
	m.view = m.popupReturnView
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
	case key.Matches(msg, keys.Esc), key.Matches(msg, keys.Info), key.Matches(msg, keys.Enter):
		m.closeInfo()
	case key.Matches(msg, keys.Edit):
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
	backdrop := m.popupBackdrop(m.popupReturnView)
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
	if height < 7 {
		height = 7
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
		hint = dimStyle.Render("e edit · j/k scroll · esc close")
	}

	content := strings.Join([]string{body, "", hint}, "\n")
	return renderPanel(title, content, width, height, cyan, true)
}

// renderInfoBody wraps and vertically clips the description, tracking its own
// scroll offset rather than borrowing the ticket detail's — the two can be open
// over each other and must not fight for one cursor.
func (m *Model) renderInfoBody(width, height int) string {
	if m.infoText == "" {
		return lipgloss.NewStyle().Foreground(subtle).
			Render("(no description — press e to write one)")
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
