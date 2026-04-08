package tui

import "github.com/charmbracelet/lipgloss"

var (
	// Colors
	green     = lipgloss.Color("#04B575")
	dimGray   = lipgloss.Color("#383838")
	midGray   = lipgloss.Color("#777777")
	white     = lipgloss.Color("#FAFAFA")
	red       = lipgloss.Color("#FF4444")
	orange    = lipgloss.Color("#FF8800")
	yellow    = lipgloss.Color("#FFCC00")
	cyan      = lipgloss.Color("#00CCCC")
	subtle    = lipgloss.Color("#555555")
	highlight = lipgloss.Color("#7D56F4")

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

