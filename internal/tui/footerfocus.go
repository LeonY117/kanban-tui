package tui

import "github.com/LeonY117/kanban-tui/internal/model"

// The board name in the footer is a navigation target. `j` past the last card
// of a column moves focus to it and `enter` opens the board's description from
// there, so the description is reachable by exploration rather than only by
// knowing `i`.
//
// The cost this pays is that a repeated key changes what's focused. Two things
// keep that honest: the badge lights up, and the column deselects — so the
// board never claims a card is selected while a card key would miss it.

// atColumnBottom reports whether the cursor is on the last card of the focused
// column — including when the column is empty, where there is nothing below to
// move to either.
func (m *Model) atColumnBottom() bool {
	count := len(m.visibleTickets(model.ColumnOrder[m.focusedCol]))
	return count == 0 || m.cursors[m.focusedCol] >= count-1
}

// footerFocusDown handles `j`. Returns true when it consumed the key, i.e. the
// cursor was already at the bottom and focus moved to the footer instead.
func (m *Model) footerFocusDown() bool {
	if m.footerFocus || !m.atColumnBottom() {
		return false
	}
	m.footerFocus = true
	return true
}

// footerFocusUp handles `k` while the footer holds focus, returning to the
// column it came from — focusedCol never changed, so there is no origin to
// remember.
func (m *Model) footerFocusUp() bool {
	if !m.footerFocus {
		return false
	}
	m.footerFocus = false
	return true
}

// footerFocusEnter opens the description when the footer holds focus.
func (m *Model) footerFocusEnter() bool {
	if !m.footerFocus {
		return false
	}
	m.enterInfo(m.sprintName)
	return true
}

// badgeLit reports whether the board name in the footer draws in its focused
// style: while the footer holds focus, and while the description it opens is on
// screen, so the popup points back at the board it describes.
func (m *Model) badgeLit() bool {
	return m.footerFocus || m.view == infoView
}
