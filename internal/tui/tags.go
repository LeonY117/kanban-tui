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
type tagPickerState struct {
	tags  []model.TagCount
	idx   int
	start int // first row rendered; the window slides to keep idx visible
}

// tagPickerMaxRows caps the popup so a board with forty tags doesn't produce a
// list taller than the terminal.
const tagPickerMaxRows = 12

func (m *Model) enterTagPicker() (tea.Model, tea.Cmd) {
	// The pool follows the active scope, so under a global search this lists
	// every board's tags — matching what picking one would then show.
	m.tags.tags = model.TagCandidates(m.searchPool(), "")
	m.tags.idx, m.tags.start = 0, 0

	// Land on the tag already being filtered, if the query is exactly one.
	if current, ok := m.activeTagFilter(); ok {
		for i, t := range m.tags.tags {
			if strings.EqualFold(t.Tag, current) {
				m.tags.idx = i
			}
		}
	}

	m.popupReturnView = m.view
	m.view = tagView
	return m, nil
}

// activeTagFilter is the tag the board is filtered by, when the whole query is
// a single tag term. Anything more complex has no one tag to point at.
func (m *Model) activeTagFilter() (string, bool) {
	tokens, _ := model.Tokenize(m.search.query)
	if len(tokens) != 1 || !tokens[0].Tagged || tokens[0].Text == "" {
		return "", false
	}
	return tokens[0].Text, true
}

func (m *Model) updateTagPicker(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch {
	case key.Matches(msg, keys.Quit):
		return m, tea.Quit
	case key.Matches(msg, keys.Esc), key.Matches(msg, keys.TagPicker):
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
	if next < 0 || next >= len(m.tags.tags) {
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
	if m.tags.idx < 0 || m.tags.idx >= len(m.tags.tags) {
		return m, nil
	}
	tag := m.tags.tags[m.tags.idx].Tag

	query := model.QuoteTag(tag)
	m.setQuery(query)
	m.search.input.SetValue(query)
	m.search.open = false
	m.restorePopupView(tagView)
	m.refreshSearchSelection()
	return m, nil
}

// tagPickerWindow is the slice of rows on screen, sliding to keep the cursor
// in view. Returns the start index and how many rows fit.
func (m *Model) tagPickerWindow() (start, rows int) {
	rows = len(m.tags.tags)
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
	if max := len(m.tags.tags) - rows; start > max {
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

	width := lipgloss.Width(title) + 8
	for _, t := range m.tags.tags {
		if w := lipgloss.Width(model.QuoteTag(t.Tag)) + 14; w > width {
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
	if len(m.tags.tags) > rows {
		height++ // the "N more" line
	}
	if len(m.tags.tags) == 0 {
		height = 3
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

	if len(m.tags.tags) == 0 {
		empty := dimStyle.Render("(no tags on this board)")
		content := lipgloss.NewStyle().PaddingLeft(1).Render(empty)
		return renderPanel(title, content, width, height, green, true)
	}

	current, hasCurrent := m.activeTagFilter()

	var lines []string
	for i := start; i < len(m.tags.tags) && len(lines) < rows; i++ {
		m.addZone(hitZone{kind: zoneTagRow, x: rowX, y: rowY + len(lines), w: innerWidth, h: 1, idx: i})
		t := m.tags.tags[i]
		lines = append(lines, m.renderTagRow(t, innerWidth, i == m.tags.idx,
			hasCurrent && strings.EqualFold(t.Tag, current)))
	}
	if hidden := len(m.tags.tags) - (start + len(lines)); hidden > 0 {
		lines = append(lines, dimStyle.Render(fmt.Sprintf("  ↓ %d more", hidden)))
	}

	content := lipgloss.NewStyle().PaddingLeft(1).Render(strings.Join(lines, "\n"))
	return renderPanel(title, content, width, height, green, true)
}

// renderTagRow lays out one tag with its count pushed to the right edge, so
// the counts form a column the eye can scan rather than trailing each name.
func (m *Model) renderTagRow(t model.TagCount, width int, selected, current bool) string {
	marker := "  "
	if selected {
		marker = selectedMarker.Render("* ")
	}

	name := model.QuoteTag(t.Tag)
	count := fmt.Sprintf("%d", t.Count)
	suffix := ""
	if current {
		suffix = " ·"
	}

	gap := width - lipgloss.Width(marker) - lipgloss.Width(name) - lipgloss.Width(count) - lipgloss.Width(suffix)
	if gap < 1 {
		room := width - lipgloss.Width(marker) - lipgloss.Width(count) - lipgloss.Width(suffix) - 1
		if room < 1 {
			room = 1
		}
		name = ansi.Truncate(name, room, "…")
		gap = 1
	}

	return marker + tagStyle.Render(name) + strings.Repeat(" ", gap) +
		dimStyle.Render(count) + selectedMarker.Render(suffix)
}
