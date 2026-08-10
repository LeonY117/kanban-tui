package tui

import "github.com/LeonY117/kanban-tui/internal/model"

// PROTOTYPE SCAFFOLDING (KA3) — delete this file and the `z` key with it once
// the choice is made.
//
// Two ways to reach a board's description are on trial, because which one feels
// right isn't an argument you can win on paper:
//
//   - `i` opens the popup from anywhere. Explicit, costs a key, invisible until
//     you read the hint line.
//   - `j` past the last card of a column moves focus to the board name in the
//     footer, and enter opens it from there. Discoverable by pure exploration,
//     but it makes a repeated key change what's focused, which is the risk.
//
// `z` flips between them at runtime so both can be felt on a real board.

// navFallthroughHint is the notice shown when the mode changes, so it's obvious
// which one is live.
func (m *Model) toggleNavFallthrough() {
	m.navFallthrough = !m.navFallthrough
	if !m.navFallthrough {
		m.footerFocus = false
		m.notice = "nav: i opens the board description · z to switch"
		return
	}
	m.notice = "nav: j past the last card reaches the board name · z to switch"
}

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
	if !m.navFallthrough || m.footerFocus || !m.atColumnBottom() {
		return false
	}
	m.footerFocus = true
	return true
}

// footerFocusUp handles `k` while the footer holds focus, returning to the
// column the fall-through came from — focusedCol never changed, so there is no
// origin to remember.
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
