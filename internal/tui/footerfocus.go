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
// style: while the footer holds focus, and while the description on screen is
// this board's, so the popup points back at the board it describes.
//
// Opened from the picker the popup usually describes a *different* board, and
// the badge always names the one the model is on — lighting it there would
// point the one "this is it" signal at the wrong board.
func (m *Model) badgeLit() bool {
	return m.footerHasFocus() || (m.view == infoView && m.infoBoard == m.sprintName)
}

// footerHasFocus is footerFocus scoped to the one view where it means
// anything. Zooming out of the board leaves the flag set, and the split and
// column views keep their own selection — a stale flag read there would blank
// a list that is being driven normally.
func (m *Model) footerHasFocus() bool {
	return m.footerFocus && m.view == boardView
}

// jumpToColumn handles the 0-4 keys. Like moving sideways, landing on a column
// is a return to the cards.
func (m *Model) jumpToColumn(col int) {
	m.focusedCol = col
	m.footerFocus = false
}
