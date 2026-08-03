package tui

import (
	"fmt"
	"strings"

	"github.com/LeonY117/kanban-tui/internal/model"
	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

// The tag picker is the search's missing half. `/` can filter by tag but only
// if you already know the tag's name; this enumerates them, which is the one
// thing a text input cannot do. Picking one writes the same query you could
// have typed, so there is one filter and one meaning of a tag, not two.

// tagRow is one line of the list. The two bookends are not tags — they are the
// states either side of "tagged with something": everything, and nothing. They
// still write a query like every other row, so there is exactly one filter.
type tagRow struct {
	label  string // what the row reads as
	query  string // the filter it applies; "" clears
	counts map[model.Status]int
}

type tagPickerState struct {
	rows  []tagRow
	idx   int
	start int // first row rendered; the window slides to keep idx visible

	// Opened from the board picker, which is the only way in. esc goes back
	// there rather than to the board, and the picker's own return view is left
	// alone — popupReturnView holds one value, so overwriting it here would
	// strand the picker with nowhere to close to.
	fromPicker bool
}

// tagPickerMaxRows caps the popup so a board with forty tags doesn't produce a
// list taller than the terminal.
const tagPickerMaxRows = 12

func (m *Model) enterTagPicker() (tea.Model, tea.Cmd) {
	// The pool follows the active scope, so under a global search this lists
	// every board's tags — matching what picking one would then show.
	m.tags.rows = m.buildTagRows()
	m.tags.idx, m.tags.start = 0, 0

	// Land on whatever the board is filtered by, so the list doubles as a
	// reminder of what is applied.
	for i, r := range m.tags.rows {
		if r.query == m.search.query {
			m.tags.idx = i
		}
	}

	m.tags.fromPicker = m.view == pickerView
	if !m.tags.fromPicker {
		m.popupReturnView = m.view
	}
	m.view = tagView
	return m, nil
}

// buildTagRows is "all tickets", then every tag by weight, then "no tags".
// The bookends sit at the ends rather than in the sort because they are not
// competing with the tags — they are the two ways of not picking one.
func (m *Model) buildTagRows() []tagRow {
	pool := m.searchPool()

	rows := []tagRow{{label: "all tickets", query: "", counts: countByStatus(pool)}}
	for _, t := range model.TagCandidates(pool, "") {
		rows = append(rows, tagRow{
			label:  model.QuoteTag(t.Tag),
			query:  model.QuoteTag(t.Tag),
			counts: t.Counts,
		})
	}
	untagged := model.ParseQuery(model.Untagged).MatchAll(pool)
	return append(rows, tagRow{
		label:  "no tags",
		query:  model.Untagged,
		counts: countByStatus(untagged),
	})
}

func countByStatus(tickets []model.Ticket) map[model.Status]int {
	out := make(map[model.Status]int, len(model.ColumnOrder))
	for _, t := range tickets {
		out[t.Status]++
	}
	return out
}

func (m *Model) updateTagPicker(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch {
	case key.Matches(msg, keys.Quit):
		return m, tea.Quit
	case key.Matches(msg, keys.Esc), key.Matches(msg, keys.TagPicker):
		// One level: back to the board list this was opened from.
		m.closeTagPicker(false)
	case key.Matches(msg, keys.BoardPicker):
		// tab toggles the whole picker, here as it does on the board list, so
		// it closes the stack rather than stepping back through it.
		m.tags.fromPicker = false
		m.restorePopupView(tagView)
	case key.Matches(msg, keys.Up):
		m.moveTagPickerCursor(-1)
	case key.Matches(msg, keys.Down):
		m.moveTagPickerCursor(1)
	case key.Matches(msg, keys.Enter), key.Matches(msg, keys.Right):
		return m.tagPickerActivate()
	}
	return m, nil
}

func (m *Model) moveTagPickerCursor(dir int) {
	next := m.tags.idx + dir
	if next < 0 || next >= len(m.tags.rows) {
		return
	}
	m.tags.idx = next
}

// tagPickerActivate filters the board by the highlighted tag and closes.
//
// It replaces the query rather than adding to it: the picker is a way of
// answering "what is on this board", so picking a second tag means looking at
// that one instead, not at the empty intersection of both. Typing the terms is
// still there for anyone who wants the intersection.
func (m *Model) tagPickerActivate() (tea.Model, tea.Cmd) {
	if m.tags.idx < 0 || m.tags.idx >= len(m.tags.rows) {
		return m, nil
	}
	query := m.tags.rows[m.tags.idx].query

	m.setQuery(query)
	m.search.input.SetValue(query)
	m.search.open = false
	m.closeTagPicker(true)
	m.refreshSearchSelection()
	return m, nil
}

// closeTagPicker leaves the popup. esc steps back to the board picker it was
// opened from; picking a tag goes all the way out, because the point of
// picking is to look at the board it just filtered.
func (m *Model) closeTagPicker(applied bool) {
	if m.tags.fromPicker && !applied {
		m.view = pickerView
		return
	}
	m.tags.fromPicker = false
	m.restorePopupView(tagView)
}

// tagPickerWindow is the slice of rows on screen, sliding to keep the cursor
// in view. Returns the start index and how many rows fit.
func (m *Model) tagPickerWindow() (start, rows int) {
	rows = len(m.tags.rows)
	if rows > tagPickerMaxRows {
		rows = tagPickerMaxRows
	}
	if avail := m.height - 6; rows > avail {
		rows = avail
	}
	if rows < 1 {
		rows = 1
	}

	start = m.tags.start
	if m.tags.idx < start {
		start = m.tags.idx
	}
	if m.tags.idx >= start+rows {
		start = m.tags.idx - rows + 1
	}
	if max := len(m.tags.rows) - rows; start > max {
		start = max
	}
	if start < 0 {
		start = 0
	}
	m.tags.start = start
	return start, rows
}

func (m *Model) viewTagPicker() string {
	start, rows := m.tagPickerWindow()

	title := "Tags"
	if m.search.global {
		title += " · all boards"
	}

	// Every row carries the same counts block, so the popup has to be wide
	// enough for the longest name plus that block — sizing off the name alone
	// truncated names down to an ellipsis.
	countsWidth := 0
	if len(m.tags.rows) > 0 {
		countsWidth = lipgloss.Width(formatCounts(m.tags.rows[0].counts))
	}
	width := lipgloss.Width(title) + 8
	for _, r := range m.tags.rows {
		if w := lipgloss.Width(r.label) + countsWidth + 8; w > width {
			width = w
		}
	}
	if width > m.width-4 {
		width = m.width - 4
	}
	if width < 24 {
		width = 24
	}

	height := rows + 2
	if len(m.tags.rows) > rows {
		height++ // the "N more" line
	}

	backdrop := m.popupBackdrop(m.popupReturnView)
	// Zones are registered while rendering the popup, so the backdrop has to
	// be drawn and its zones cleared first — zoneAt scans newest-first, and
	// the board's column zones would otherwise sit on top of every row.
	m.resetZones()

	origin := m.popupOrigin(width, height)
	popup := m.renderTagPopup(title, width, height, start, rows, origin)
	return overlayAt(backdrop, popup, origin.x, origin.y)
}

func (m *Model) renderTagPopup(title string, width, height, start, rows int, origin point) string {
	innerWidth := width - 4
	if innerWidth < 10 {
		innerWidth = 10
	}
	rowY, rowX := origin.y+1, origin.x+2

	var lines []string
	for i := start; i < len(m.tags.rows) && len(lines) < rows; i++ {
		m.addZone(hitZone{kind: zoneTagRow, x: rowX, y: rowY + len(lines), w: innerWidth, h: 1, idx: i})
		r := m.tags.rows[i]
		lines = append(lines, m.renderTagRow(r, innerWidth, i == m.tags.idx, r.query == m.search.query))
	}
	if hidden := len(m.tags.rows) - (start + len(lines)); hidden > 0 {
		lines = append(lines, dimStyle.Render(fmt.Sprintf("  ↓ %d more", hidden)))
	}

	content := lipgloss.NewStyle().PaddingLeft(1).Render(strings.Join(lines, "\n"))
	return renderPanel(title, content, width, height, green, true)
}

// renderTagRow lays a tag out exactly as the board picker lays a board out:
// name on the left in the default foreground, per-column counts right-aligned
// in the column colours. The two lists are siblings reached by the same key,
// so they read as one thing rather than two — and the breakdown says where a
// tag's work actually sits, which a single dim number never did.
func (m *Model) renderTagRow(r tagRow, width int, selected, current bool) string {
	marker := "  "
	if selected {
		marker = selectedMarker.Render("* ")
	}

	name := r.label
	nameStyle := lipgloss.NewStyle()
	switch {
	case current:
		nameStyle = nameStyle.Foreground(green).Bold(true)
	case r.query == "" || r.query == model.Untagged:
		// The bookends are states, not tags; dimming them keeps the tags
		// themselves the thing the eye lands on.
		nameStyle = nameStyle.Foreground(midGray)
	}
	counts := formatCounts(r.counts)

	left := marker + nameStyle.Render(name)
	gap := width - lipgloss.Width(left) - lipgloss.Width(counts)
	if gap < 1 {
		room := width - lipgloss.Width(marker) - lipgloss.Width(counts) - 1
		if room < 1 {
			room = 1
		}
		left = marker + nameStyle.Render(ansi.Truncate(name, room, "…"))
		gap = 1
	}
	return left + strings.Repeat(" ", gap) + counts
}
