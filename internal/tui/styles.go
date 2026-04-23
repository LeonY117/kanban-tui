package tui

import (
	"github.com/charmbracelet/lipgloss"
	"github.com/leon/kanban/internal/model"
)

// Colors are ANSI 16 palette entries — the terminal resolves them from its
// own theme, so kanban tracks dark/light toggles live without restarting.
// Empty-string colors fall through to the terminal's default foreground.
var (
	green = lipgloss.Color("2")
	blue  = lipgloss.Color("4")
	peach = lipgloss.Color("3") // yellow slot — stands in for "in progress"
	mauve = lipgloss.Color("5")
	cyan  = lipgloss.Color("6")

	// Shades of gray all map to bright-black — visible on both light and
	// dark backgrounds in every sensible terminal theme.
	dimGray = lipgloss.Color("8")
	midGray = lipgloss.Color("8")
	subtle  = lipgloss.Color("8")

	// Content text: use the terminal's default foreground so it always
	// contrasts with the terminal's own background.
	softWhite = lipgloss.Color("")
	white     = lipgloss.Color("")

	// Per-column accent colors
	columnColors = map[model.Status]lipgloss.Color{
		model.StatusBacklog: cyan,
		model.StatusTodo:    blue,
		model.StatusDoing:   peach,
		model.StatusDone:    green,
		model.StatusHold:    mauve,
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

	// Sprint indicator (bottom-left of screen when a sprint is active)
	sprintBadgeStyle = lipgloss.NewStyle().
				Foreground(cyan).
				Bold(true).
				Padding(0, 1)

	// Title bar — bold, default fg.
	titleBar = lipgloss.NewStyle().
			Bold(true).
			Padding(0, 1)

	// Detail label
	detailLabel = lipgloss.NewStyle().
			Foreground(midGray).
			Width(12)

	// Detail value — default fg.
	detailValue = lipgloss.NewStyle()

	// Detail title — bold, default fg.
	detailTitle = lipgloss.NewStyle().
			Bold(true).
			Padding(0, 0, 1, 0)

	// Detail separator
	detailSep = lipgloss.NewStyle().
			Foreground(dimGray)

	// Highlight for the currently selected meta field. Reverse(true) swaps
	// the terminal's fg/bg so it adapts to any theme without picking colors.
	selectedFieldStyle = lipgloss.NewStyle().
				Reverse(true).
				Bold(true).
				Padding(0, 1)
)

// columnColor returns the accent color for a given status column.
func columnColor(status model.Status) lipgloss.Color {
	if c, ok := columnColors[status]; ok {
		return c
	}
	return softWhite
}
