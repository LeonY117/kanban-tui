package tui

import (
	"sort"
	"strconv"
	"strings"

	"github.com/LeonY117/kanban-tui/internal/model"
	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// untaggedFilter is the sentinel tagFilter value for "cards with no tag".
// Parentheses keep it out of the plausible real-tag namespace.
const untaggedFilter = "(untagged)"

// tagEntry is one row in the tag picker — the all-tickets row, a tag, or the
// untagged row.
type tagEntry struct {
	value string // "" = all tickets, untaggedFilter = no tag, else a tag name
	count int    // open (non-DONE) cards carrying this value
}

// ticketsFor returns the column's tickets with the active tag filter applied.
// All TUI reads go through here instead of board.ByStatus so a filtered board
// stays consistent across renders, cursors, and ticket moves.
func (m *Model) ticketsFor(status model.Status) []model.Ticket {
	tickets := m.board.ByStatus(status)
	if m.tagFilter == "" {
		return tickets
	}
	out := make([]model.Ticket, 0, len(tickets))
	for _, t := range tickets {
		if m.matchesTagFilter(&t) {
			out = append(out, t)
		}
	}
	return out
}

func (m *Model) matchesTagFilter(t *model.Ticket) bool {
	switch m.tagFilter {
	case "":
		return true
	case untaggedFilter:
		return len(t.Tags) == 0
	default:
		return t.HasTag(m.tagFilter)
	}
}

// enterTagPicker opens the tag filter popup. Tags and counts come from the
// board's open cards only — DONE cards don't vote (and archived cards live
// off-board already), so a tag that survives only on finished work doesn't
// clutter the list.
func (m *Model) enterTagPicker() (tea.Model, tea.Cmd) {
	m.tagEntries = buildTagEntries(m.board)
	m.tagIdx = 0
	for i, e := range m.tagEntries {
		if e.value == m.tagFilter {
			m.tagIdx = i
			break
		}
	}
	m.popupReturnView = m.view
	m.view = tagPickerView
	return m, nil
}

// buildTagEntries lists "all tickets" first, then each tag alphabetically,
// then an untagged row when any open card has no tag. Counts exclude DONE.
func buildTagEntries(board *model.Board) []tagEntry {
	counts := map[string]int{}
	total, untagged := 0, 0
	for _, t := range board.Tickets {
		if t.Status == model.StatusDone {
			continue
		}
		total++
		if len(t.Tags) == 0 {
			untagged++
			continue
		}
		for _, tag := range t.Tags {
			counts[strings.ToLower(tag)]++
		}
	}

	names := make([]string, 0, len(counts))
	for name := range counts {
		names = append(names, name)
	}
	sort.Strings(names)

	entries := []tagEntry{{value: "", count: total}}
	for _, name := range names {
		entries = append(entries, tagEntry{value: name, count: counts[name]})
	}
	if untagged > 0 {
		entries = append(entries, tagEntry{value: untaggedFilter, count: untagged})
	}
	return entries
}

func (m *Model) updateTagPicker(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch {
	case key.Matches(msg, keys.Quit):
		return m, tea.Quit
	case key.Matches(msg, keys.Esc), key.Matches(msg, keys.TagPicker):
		m.closeTagPicker()
	case key.Matches(msg, keys.Up):
		if m.tagIdx > 0 {
			m.tagIdx--
		}
	case key.Matches(msg, keys.Down):
		if m.tagIdx < len(m.tagEntries)-1 {
			m.tagIdx++
		}
	case key.Matches(msg, keys.Enter):
		if m.tagIdx >= 0 && m.tagIdx < len(m.tagEntries) {
			m.tagFilter = m.tagEntries[m.tagIdx].value
			m.clampCursors()
		}
		m.closeTagPicker()
	}
	return m, nil
}

// closeTagPicker restores the source view; the filter may have moved the
// cursor onto a different ticket, so split/detail editors need a refresh
// (same contract as closeAddPopup).
func (m *Model) closeTagPicker() {
	m.restorePopupView(tagPickerView)
	if m.view == splitView || m.view == detailView {
		m.refreshDetailEditors()
	}
}

// tagDisplayName resolves the sentinel values for rendering.
func tagDisplayName(value string) string {
	switch value {
	case "":
		return "all tickets"
	case untaggedFilter:
		return untaggedFilter
	default:
		return "#" + value
	}
}

func (m *Model) viewTagPicker() string {
	rowCount := len(m.tagEntries)
	if rowCount < 1 {
		rowCount = 1
	}
	popupHeight := rowCount + 2
	if popupHeight > m.height-4 {
		popupHeight = m.height - 4
	}
	if popupHeight < 5 {
		popupHeight = 5
	}

	popupWidth := tagPickerPopupWidth(m.tagEntries)
	if popupWidth > m.width-4 {
		popupWidth = m.width - 4
	}

	popup := m.renderTagPickerPopup(popupWidth, popupHeight)
	return m.centerOverPopup(popup, m.popupBackdrop(m.popupReturnView), popupWidth, popupHeight)
}

// tagPickerPopupWidth sizes the popup to fit the widest row (name + count).
func tagPickerPopupWidth(entries []tagEntry) int {
	const (
		minWidth = 30
		maxWidth = 60
	)
	widest := 0
	for _, e := range entries {
		w := lipgloss.Width(tagDisplayName(e.value)) + 2 + len(formatTagCount(e.count))
		if w > widest {
			widest = w
		}
	}
	// +6: marker (2) + outer border (2) + inner padding (2)
	width := widest + 6
	if width < minWidth {
		width = minWidth
	}
	if width > maxWidth {
		width = maxWidth
	}
	return width
}

func (m *Model) renderTagPickerPopup(width, height int) string {
	innerWidth := width - 4
	if innerWidth < 10 {
		innerWidth = 10
	}

	var rows []string
	for i, e := range m.tagEntries {
		rows = append(rows, renderTagRow(e, innerWidth, i == m.tagIdx, e.value == m.tagFilter))
	}

	visible := height - 2
	if visible < 1 {
		visible = 1
	}
	if len(rows) > visible {
		start := m.tagIdx - visible/2
		if start < 0 {
			start = 0
		}
		if start+visible > len(rows) {
			start = len(rows) - visible
		}
		rows = rows[start : start+visible]
	}

	content := lipgloss.NewStyle().PaddingLeft(1).Render(strings.Join(rows, "\n"))
	return renderPanel("Tags", content, width, height, green, true)
}

func renderTagRow(e tagEntry, width int, selected, current bool) string {
	marker := "  "
	if selected {
		marker = selectedMarker.Render("* ")
	}
	nameStyle := lipgloss.NewStyle()
	if e.value == untaggedFilter {
		nameStyle = nameStyle.Foreground(dimGray)
	}
	if current {
		nameStyle = nameStyle.Foreground(green).Bold(true)
	}
	count := dimStyle.Render(formatTagCount(e.count))

	// Fill the space between name and count so the count right-aligns.
	left := marker + nameStyle.Render(tagDisplayName(e.value))
	leftWidth := lipgloss.Width(left)
	rightWidth := lipgloss.Width(count)
	gap := width - leftWidth - rightWidth
	if gap < 1 {
		gap = 1
	}
	return left + strings.Repeat(" ", gap) + count
}

func formatTagCount(n int) string {
	if n == 1 {
		return "1 card"
	}
	return strconv.Itoa(n) + " cards"
}
