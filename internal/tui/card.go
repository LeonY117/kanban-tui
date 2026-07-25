package tui

import (
	"strings"

	"github.com/LeonY117/kanban-tui/internal/model"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

// ticketLayout controls how much of a ticket each list row shows. The three
// sizes form a ladder: condensed (one line), card (title + meta), large (title
// + description preview + meta).
type ticketLayout int

const (
	layoutCard ticketLayout = iota
	layoutLarge
	layoutCondensed
)

// next walks the size ladder: card → large → condensed → card.
func (l ticketLayout) next() ticketLayout {
	switch l {
	case layoutCard:
		return layoutLarge
	case layoutLarge:
		return layoutCondensed
	default:
		return layoutCard
	}
}

func (l ticketLayout) label() string {
	switch l {
	case layoutCard:
		return "cards"
	case layoutLarge:
		return "large"
	default:
		return "condensed"
	}
}

// Title and description line caps per layout.
const (
	cardTitleMaxLines  = 2
	largeTitleMaxLines = 3
	largeDescMaxLines  = 3
)

// renderTicketList renders a column's tickets into `height` rows, scrolling so
// the cursor stays visible. cursor < 0 means nothing is selected and the list
// renders from the top. The returned block is a plain "\n"-joined string, ready
// to hand to renderPanel.
//
// Every ticket it emits is registered as a click target via m.addTicketZone, so
// mouse hit-testing stays in lockstep with what's actually on screen.
func (m *Model) renderTicketList(tickets []model.Ticket, colIdx, width, height, cursor int, accent lipgloss.Color, origin point) string {
	if height < 1 {
		height = 1
	}
	if m.layout == layoutCondensed {
		return m.renderTicketLines(tickets, colIdx, width, height, cursor, accent, origin)
	}
	return m.renderCardStack(tickets, colIdx, width, height, cursor, accent, origin)
}

// renderTicketLines is the condensed layout: one line per ticket.
func (m *Model) renderTicketLines(tickets []model.Ticket, colIdx, width, height, cursor int, accent lipgloss.Color, origin point) string {
	start := 0
	if cursor >= height {
		start = cursor - height + 1
	}
	var lines []string
	for i := start; i < len(tickets) && len(lines) < height; i++ {
		m.addTicketZone(colIdx, i, origin.x, origin.y+len(lines), width, 1)
		lines = append(lines, m.renderTicketLine(tickets[i], i == cursor, width, accent))
	}
	return strings.Join(lines, "\n")
}

// renderCardStack stacks ticket blocks. Card and condensed-adjacent sizes
// separate them with rules — one above each ticket, plus a closing rule under
// the last, so every block is bracketed. The large size gives each ticket its
// own box instead, since at that size the extra air reads better.
//
// The ticket under the cursor is picked out by an accent bar down its left
// edge and heavy accented rules bracketing it (in the large size, by its box
// border going accent). Nothing about the geometry depends on the selection,
// so moving the cursor shifts nothing.
func (m *Model) renderCardStack(tickets []model.Ticket, colIdx, width, height, cursor int, accent lipgloss.Color, origin point) string {
	if width < 4 {
		width = 4
	}
	inner := width - 2 // 1 col of gutter (or border) on each side
	boxed := m.layout == layoutLarge

	chrome := 1 // the rule above each block
	if boxed {
		chrome = 2 // top and bottom border
	}

	contents := make([][]string, len(tickets))
	costs := make([]int, len(tickets))
	for i, t := range tickets {
		contents[i] = m.cardContent(t, i == cursor, inner, accent)
		costs[i] = len(contents[i]) + chrome
	}

	avail := height
	if !boxed {
		avail-- // reserve the rule that closes the last block
	}
	if avail < 1 {
		avail = 1
	}
	start := 0
	if cursor >= 0 && cursor < len(tickets) {
		start = fitScrollStart(costs, cursor, avail)
	}

	pad := func(l string) string {
		n := inner - lipgloss.Width(l)
		if n < 0 {
			n = 0
		}
		return l + strings.Repeat(" ", n)
	}

	// A rule goes heavy and accented when it brackets the selected block.
	rule := func(touchesSelection bool) string {
		if touchesSelection {
			return lipgloss.NewStyle().Foreground(accent).Bold(true).Render(strings.Repeat("━", width))
		}
		return lipgloss.NewStyle().Foreground(dimGray).Render(strings.Repeat("─", width))
	}

	// Blocks are emitted line by line up to the panel's height, so the ticket
	// that straddles the bottom edge shows as much of itself as fits rather
	// than leaving a gap. The cursor's block is always whole: fitScrollStart
	// guarantees it ends within avail.
	var lines []string
	full := -1 // last block that fitted in its entirety
	for i := start; i < len(tickets) && len(lines) < height; i++ {
		block := renderTicketBlock(contents[i], boxed, i == cursor, i-1 == cursor, inner, pad, rule, accent)
		room := height - len(lines)
		if len(block) > room {
			block = block[:room]
		} else {
			full = i
		}
		m.addTicketZone(colIdx, i, origin.x, origin.y+len(lines), width, len(block))
		lines = append(lines, block...)
	}
	if !boxed && len(lines) > 0 && len(lines) < height {
		lines = append(lines, rule(full == cursor))
	}
	return strings.Join(lines, "\n")
}

// renderTicketBlock frames one ticket's content lines — a box in the large
// size, a rule plus body otherwise.
func renderTicketBlock(content []string, boxed, selected, afterSelected bool, inner int, pad func(string) string, rule func(bool) string, accent lipgloss.Color) []string {
	if boxed {
		border := lipgloss.NewStyle().Foreground(dimGray)
		if selected {
			border = lipgloss.NewStyle().Foreground(accent).Bold(true)
		}
		block := []string{border.Render("╭" + strings.Repeat("─", inner) + "╮")}
		for _, l := range content {
			block = append(block, border.Render("│")+pad(l)+border.Render("│"))
		}
		return append(block, border.Render("╰"+strings.Repeat("─", inner)+"╯"))
	}

	block := []string{rule(selected || afterSelected)}
	for _, l := range content {
		block = append(block, renderCardLine(l, pad, selected, accent))
	}
	return block
}

// renderCardLine frames one content line: a gutter column on each side, which
// on the selected block's left edge carries the accent bar.
func renderCardLine(line string, pad func(string) string, selected bool, accent lipgloss.Color) string {
	if selected {
		bar := lipgloss.NewStyle().Foreground(accent).Bold(true).Render("▌")
		return bar + pad(line) + " "
	}
	return " " + pad(line) + " "
}

// fitScrollStart returns the first index to render so that the cursor's block
// is fully visible, packing as many earlier blocks above it as fit.
func fitScrollStart(costs []int, cursor, avail int) int {
	start := cursor
	used := costs[cursor]
	for start > 0 && used+costs[start-1] <= avail {
		start--
		used += costs[start]
	}
	return start
}

// cardContent is the styled body of one card — the lines between its borders.
func (m *Model) cardContent(t model.Ticket, selected bool, width int, accent lipgloss.Color) []string {
	titleStyle := lipgloss.NewStyle().Foreground(softWhite)
	if selected {
		titleStyle = lipgloss.NewStyle().Foreground(white).Bold(true)
	}

	titleMax := cardTitleMaxLines
	if m.layout == layoutLarge {
		titleMax = largeTitleMaxLines
	}

	var lines []string
	for _, l := range wrapLines(t.Title, width, titleMax) {
		lines = append(lines, titleStyle.Render(l))
	}

	if m.layout == layoutLarge && strings.TrimSpace(t.Description) != "" {
		descStyle := lipgloss.NewStyle().Foreground(midGray)
		for _, l := range wrapLines(t.Description, width, largeDescMaxLines) {
			lines = append(lines, descStyle.Render(l))
		}
	}

	return append(lines, cardMetaLine(t, width, m.layout == layoutLarge, selected, accent))
}

// cardMetaLine builds a card's bottom line: short id, tags, assignee, and — in
// the large layout — when it last changed. Pieces that don't fit are dropped,
// rightmost first. The selected ticket's id takes the column's accent so the
// id you'd type into the CLI is the one that stands out.
func cardMetaLine(t model.Ticket, width int, withDate, selected bool, accent lipgloss.Color) string {
	idStyle := dimStyle
	if selected {
		idStyle = lipgloss.NewStyle().Foreground(accent).Bold(true)
	}
	parts := []string{idStyle.Render(t.ShortID)}
	used := lipgloss.Width(t.ShortID)

	add := func(text, styled string) {
		if used+2+lipgloss.Width(text) <= width {
			parts = append(parts, styled)
			used += 2 + lipgloss.Width(text)
		}
	}

	if len(t.Tags) > 0 {
		tags := "#" + strings.Join(t.Tags, " #")
		add(tags, tagStyle.Render(tags))
	}
	if t.AssignedTo != "" {
		assign := "● " + t.AssignedTo
		add(assign, assigneeStyle.Render(assign))
	}
	if withDate {
		date := t.UpdatedAt.Format("2006-01-02")
		add(date, dimStyle.Render(date))
	}
	return strings.Join(parts, "  ")
}

// wrapLines word-wraps text to width, capping at maxLines and marking a
// truncated tail with an ellipsis.
func wrapLines(text string, width, maxLines int) []string {
	if width < 1 {
		width = 1
	}
	wrapped := lipgloss.NewStyle().Width(width).Render(text)
	lines := strings.Split(strings.TrimRight(wrapped, "\n"), "\n")
	for i := range lines {
		lines[i] = strings.TrimRight(lines[i], " ")
	}
	if maxLines > 0 && len(lines) > maxLines {
		lines = lines[:maxLines]
		last := lines[maxLines-1]
		if lipgloss.Width(last)+1 <= width {
			lines[maxLines-1] = last + "…"
		} else {
			lines[maxLines-1] = ansi.Truncate(last, width, "…")
		}
	}
	if len(lines) == 0 {
		lines = []string{""}
	}
	return lines
}
