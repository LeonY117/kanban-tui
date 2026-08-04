package tui

import (
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
	rows []tagRow
	idx  int
}

func (m *Model) enterTagPicker() (tea.Model, tea.Cmd) {
	// The pool follows the active scope, so under a global search this lists
	// every board's tags — matching what picking one would then show.
	m.tags.rows = m.buildTagRows()
	m.tags.idx = 0

	// Land on whatever the board is filtered by, so the list doubles as a
	// reminder of what is applied.
	for i, r := range m.tags.rows {
		if r.query == m.search.query {
			m.tags.idx = i
		}
	}

	// A tag picker nested over the board picker must keep the board picker's
	// return view. popupReturnView holds one value, so overwriting it here would
	// leave no board to restore when the tag picker exits.
	if m.view != pickerView {
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
	case key.Matches(msg, keys.Esc), key.Matches(msg, keys.TagPicker),
		key.Matches(msg, keys.BoardPicker):
		// Every way out leaves for the board, not for the list this was opened
		// from. One press, one exit (Leon, 2026-08-03) — stepping back to the
		// board picker made esc mean "up one level" in the only place it means
		// that, and put two presses between the tag list and the board it
		// filters.
		m.closeTagPicker()
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
	m.closeTagPicker()
	m.refreshSearchSelection()
	return m, nil
}

// closeTagPicker leaves the popup for the board, whether a tag was picked or
// not. The point of the list is the board it filters, so every exit goes there
// rather than back through the board picker it was opened from.
func (m *Model) closeTagPicker() {
	m.restorePopupView(tagView)
}

// tagPickerWindow is the slice of rows on screen. It centres on the cursor and
// keeps no scroll state, exactly as the board list does — the two popups are
// the same object and scrolling them differently was the most visible way they
// had drifted apart.
func (m *Model) tagPickerWindow(height int) (start, rows int) {
	rows = height - 2
	if rows < 1 {
		rows = 1
	}
	if rows > len(m.tags.rows) {
		rows = len(m.tags.rows)
	}
	if len(m.tags.rows) > rows {
		start = m.tags.idx - rows/2
		if start < 0 {
			start = 0
		}
		if start+rows > len(m.tags.rows) {
			start = len(m.tags.rows) - rows
		}
	}
	return start, rows
}

func (m *Model) viewTagPicker() string {
	title := "Tags"
	if m.search.global {
		title += " · all boards"
	}

	// Sized by the same rule as the board list: the widest row, where a row is
	// the label plus the same right-aligned counts block every row carries.
	widest := 0
	for _, r := range m.tags.rows {
		if w := listRowWidth(r.label, r.counts); w > widest {
			widest = w
		}
	}
	width, height := m.listPopupSize(widest, len(m.tags.rows))
	start, rows := m.tagPickerWindow(height)

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
