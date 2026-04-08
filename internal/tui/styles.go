package tui

import (
	"github.com/charmbracelet/lipgloss"
	"github.com/leon/kanban/internal/model"
)

var (
	// Colors
	green     = lipgloss.Color("#A6E3A1")
	blue      = lipgloss.Color("#89B4FA")
	peach     = lipgloss.Color("#FAB387")
	mauve     = lipgloss.Color("#CBA6F7")
	dimGray   = lipgloss.Color("#383838")
	midGray   = lipgloss.Color("#777777")
	softWhite = lipgloss.Color("#CDD6F4")
	white     = lipgloss.Color("#FAFAFA")
	red       = lipgloss.Color("#FF4444")
	orange    = lipgloss.Color("#FF8800")
	yellow    = lipgloss.Color("#FFCC00")
	cyan      = lipgloss.Color("#00CCCC")
	subtle    = lipgloss.Color("#555555")
	highlight = lipgloss.Color("#7D56F4")

	// Per-column accent colors
	columnColors = map[model.Status]lipgloss.Color{
		model.StatusTodo:  blue,
		model.StatusDoing: peach,
		model.StatusDone:  green,
		model.StatusHold:  mauve,
	}

	// Focused panel border
	focusedBorder = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(green)

	// Unfocused panel border
	unfocusedBorder = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(dimGray)

	// Column header
	columnHeader = lipgloss.NewStyle().
			Bold(true).
			Padding(0, 1)

	// Selected item marker
	selectedMarker = lipgloss.NewStyle().
			Foreground(green).
			Bold(true)

	// Unselected item
	normalItem = lipgloss.NewStyle().
			Padding(0, 1)

	// Tag style
	tagStyle = lipgloss.NewStyle().
			Foreground(midGray)

	// Assignee indicator
	assigneeStyle = lipgloss.NewStyle().
			Foreground(cyan)

	// Help bar at bottom
	helpStyle = lipgloss.NewStyle().
			Foreground(midGray).
			Padding(0, 1)

	// Title bar
	titleBar = lipgloss.NewStyle().
			Bold(true).
			Foreground(white).
			Padding(0, 1)

	// Detail label
	detailLabel = lipgloss.NewStyle().
			Foreground(midGray).
			Width(12)

	// Detail value
	detailValue = lipgloss.NewStyle().
			Foreground(white)

	// Detail title
	detailTitle = lipgloss.NewStyle().
			Bold(true).
			Foreground(white).
			Padding(0, 0, 1, 0)

	// Detail separator
	detailSep = lipgloss.NewStyle().
			Foreground(dimGray)
)

// columnColor returns the accent color for a given status column.
func columnColor(status model.Status) lipgloss.Color {
	if c, ok := columnColors[status]; ok {
		return c
	}
	return softWhite
}
