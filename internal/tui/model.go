package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/leon/kanban/internal/model"
	"github.com/leon/kanban/internal/store"
)

type viewMode int

const (
	boardView viewMode = iota
	columnView
	detailView
)

// inputMode tracks what the user is typing into.
type inputMode int

const (
	inputNone inputMode = iota
	inputAdd
	inputAssign
	inputSelect // for status picker
)

// statusDisplay maps internal status to sentence-case display name.
var statusDisplay = map[model.Status]string{
	model.StatusBacklog: "Backlog",
	model.StatusTodo:    "Todo",
	model.StatusDoing:   "Doing",
	model.StatusDone:    "Done",
	model.StatusHold:    "Hold",
}

type Model struct {
	store      *store.Store
	board      *model.Board
	width      int
	height     int
	ready      bool
	view       viewMode
	focusedCol int // index into model.ColumnOrder
	cursors    [5]int // selected item index per column
	input      textinput.Model
	inputMode  inputMode
	err        error

	// Selection picker state (for status)
	selectOptions []string
	selectIdx     int
	selectLabel   string
	onSelect      func(string) // called when user picks an option
}

func NewModel(s *store.Store) (*Model, error) {
	board, err := s.Load()
	if err != nil {
		return nil, err
	}

	ti := textinput.New()
	ti.CharLimit = 200

	return &Model{
		store: s,
		board: board,
		input: ti,
	}, nil
}

func (m *Model) Init() tea.Cmd {
	return nil
}

func (m *Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.ready = true
		return m, nil

	case tea.KeyMsg:
		// If in select mode, handle picker
		if m.inputMode == inputSelect {
			return m.updateSelect(msg)
		}
		// If in input mode, handle text input
		if m.inputMode != inputNone {
			return m.updateInput(msg)
		}

		switch m.view {
		case boardView:
			return m.updateBoard(msg)
		case columnView:
			return m.updateColumn(msg)
		case detailView:
			return m.updateDetail(msg)
		}
	}
	return m, nil
}

func (m *Model) View() string {
	if !m.ready {
		return "Loading..."
	}

	var content string
	switch m.view {
	case boardView:
		content = m.viewBoard()
	case columnView:
		content = m.viewColumn()
	case detailView:
		content = m.viewDetail()
	}

	// Add input bar or picker if active
	if m.inputMode == inputSelect {
		content = lipgloss.JoinVertical(lipgloss.Left, content, m.viewSelect())
	} else if m.inputMode != inputNone {
		content = lipgloss.JoinVertical(lipgloss.Left, content, m.viewInput())
	}

	return content
}

func (m *Model) reload() {
	board, err := m.store.Load()
	if err != nil {
		m.err = err
		return
	}
	m.board = board
}

func (m *Model) selectedTicket() *model.Ticket {
	status := model.ColumnOrder[m.focusedCol]
	tickets := m.board.ByStatus(status)
	idx := m.cursors[m.focusedCol]
	if idx >= len(tickets) {
		return nil
	}
	return &tickets[idx]
}

func (m *Model) clampCursors() {
	for i, status := range model.ColumnOrder {
		count := len(m.board.ByStatus(status))
		if m.cursors[i] >= count && count > 0 {
			m.cursors[i] = count - 1
		}
		if count == 0 {
			m.cursors[i] = 0
		}
	}
}

// updateBoard handles keys in board view.
func (m *Model) updateBoard(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch {
	case key.Matches(msg, keys.Quit):
		return m, tea.Quit
	case key.Matches(msg, keys.Left):
		if m.focusedCol > 0 {
			m.focusedCol--
		}
	case key.Matches(msg, keys.Right):
		if m.focusedCol < len(model.ColumnOrder)-1 {
			m.focusedCol++
		}
	case key.Matches(msg, keys.Up):
		if m.cursors[m.focusedCol] > 0 {
			m.cursors[m.focusedCol]--
		}
	case key.Matches(msg, keys.Down):
		status := model.ColumnOrder[m.focusedCol]
		count := len(m.board.ByStatus(status))
		if m.cursors[m.focusedCol] < count-1 {
			m.cursors[m.focusedCol]++
		}
	case key.Matches(msg, keys.Enter):
		if m.selectedTicket() != nil {
			m.view = detailView
		}
	case key.Matches(msg, keys.Tab):
		m.view = columnView
	case key.Matches(msg, keys.Add):
		m.startInput(inputAdd, "New ticket: ")
		return m, textinput.Blink
	case key.Matches(msg, keys.One):
		m.focusedCol = 0
	case key.Matches(msg, keys.Two):
		m.focusedCol = 1
	case key.Matches(msg, keys.Three):
		m.focusedCol = 2
	case key.Matches(msg, keys.Four):
		m.focusedCol = 3
	case key.Matches(msg, keys.Five):
		m.focusedCol = 4
	case key.Matches(msg, keys.MoveLeft):
		m.moveTicket(-1)
	case key.Matches(msg, keys.MoveRight):
		m.moveTicket(1)
	}
	return m, nil
}

// updateColumn handles keys in column view.
func (m *Model) updateColumn(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch {
	case key.Matches(msg, keys.Quit):
		return m, tea.Quit
	case key.Matches(msg, keys.Esc):
		m.view = boardView
	case key.Matches(msg, keys.Tab):
		m.focusedCol = (m.focusedCol + 1) % len(model.ColumnOrder)
	case key.Matches(msg, keys.Up):
		if m.cursors[m.focusedCol] > 0 {
			m.cursors[m.focusedCol]--
		}
	case key.Matches(msg, keys.Down):
		status := model.ColumnOrder[m.focusedCol]
		count := len(m.board.ByStatus(status))
		if m.cursors[m.focusedCol] < count-1 {
			m.cursors[m.focusedCol]++
		}
	case key.Matches(msg, keys.Enter):
		if m.selectedTicket() != nil {
			m.view = detailView
		}
	case key.Matches(msg, keys.Add):
		m.startInput(inputAdd, "New ticket: ")
		return m, textinput.Blink
	case key.Matches(msg, keys.One):
		m.focusedCol = 0
	case key.Matches(msg, keys.Two):
		m.focusedCol = 1
	case key.Matches(msg, keys.Three):
		m.focusedCol = 2
	case key.Matches(msg, keys.Four):
		m.focusedCol = 3
	case key.Matches(msg, keys.Five):
		m.focusedCol = 4
	case key.Matches(msg, keys.MoveLeft):
		m.moveTicket(-1)
	case key.Matches(msg, keys.MoveRight):
		m.moveTicket(1)
	}
	return m, nil
}

// updateDetail handles keys in detail view.
func (m *Model) updateDetail(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch {
	case key.Matches(msg, keys.Quit):
		return m, tea.Quit
	case key.Matches(msg, keys.Esc):
		m.view = boardView
	case key.Matches(msg, keys.Status):
		m.startSelect("Status", []string{"Backlog", "Todo", "Doing", "Done", "Hold"}, func(val string) {
			t := m.selectedTicket()
			if t == nil {
				return
			}
			status, err := model.ParseStatus(val)
			if err != nil {
				return
			}
			m.store.Update(t.ID, func(ticket *model.Ticket) {
				ticket.Status = status
			})
			m.reload()
			m.clampCursors()
		})
		return m, nil
	case key.Matches(msg, keys.Assign):
		m.startInput(inputAssign, "Assign to: ")
		return m, textinput.Blink
	case key.Matches(msg, keys.Delete):
		m.deleteTicket()
		m.view = boardView
	case key.Matches(msg, keys.MoveLeft):
		m.moveTicket(-1)
	case key.Matches(msg, keys.MoveRight):
		m.moveTicket(1)
	}
	return m, nil
}

func (m *Model) startInput(mode inputMode, prompt string) {
	m.inputMode = mode
	m.input.Placeholder = ""
	m.input.Prompt = prompt
	m.input.SetValue("")
	m.input.Focus()
}

func (m *Model) updateInput(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "enter":
		m.submitInput()
		return m, nil
	case "esc":
		m.inputMode = inputNone
		m.input.Blur()
		return m, nil
	}

	var cmd tea.Cmd
	m.input, cmd = m.input.Update(msg)
	return m, cmd
}

func (m *Model) submitInput() {
	value := strings.TrimSpace(m.input.Value())
	prompt := m.input.Prompt
	m.input.Blur()
	m.inputMode = inputNone

	if value == "" {
		return
	}

	switch {
	case strings.HasPrefix(prompt, "New ticket"):
		status := model.ColumnOrder[m.focusedCol]
		_, err := m.store.Add(value, "", status, nil, "", "tui")
		if err != nil {
			m.err = err
			return
		}
		m.reload()
		m.clampCursors()

	case strings.HasPrefix(prompt, "Assign"):
		t := m.selectedTicket()
		if t == nil {
			return
		}
		m.store.Update(t.ID, func(ticket *model.Ticket) {
			ticket.AssignedTo = value
		})
		m.reload()
	}
}

func (m *Model) startSelect(label string, options []string, onSelect func(string)) {
	m.inputMode = inputSelect
	m.selectLabel = label
	m.selectOptions = options
	m.selectIdx = 0
	m.onSelect = onSelect
}

func (m *Model) updateSelect(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "j", "down":
		if m.selectIdx < len(m.selectOptions)-1 {
			m.selectIdx++
		}
	case "k", "up":
		if m.selectIdx > 0 {
			m.selectIdx--
		}
	case "enter":
		if m.onSelect != nil {
			m.onSelect(m.selectOptions[m.selectIdx])
		}
		m.inputMode = inputNone
	case "esc":
		m.inputMode = inputNone
	}
	return m, nil
}

func (m *Model) viewSelect() string {
	var parts []string
	parts = append(parts, helpStyle.Render(m.selectLabel+":"))
	for i, opt := range m.selectOptions {
		if i == m.selectIdx {
			parts = append(parts, selectedMarker.Render(" * "+opt))
		} else {
			parts = append(parts, helpStyle.Render("   "+opt))
		}
	}
	return lipgloss.JoinHorizontal(lipgloss.Top, parts...)
}

func (m *Model) moveTicket(dir int) {
	t := m.selectedTicket()
	if t == nil {
		return
	}
	ticketID := t.ID
	colIdx := m.focusedCol + dir
	if colIdx < 0 || colIdx >= len(model.ColumnOrder) {
		return
	}
	newStatus := model.ColumnOrder[colIdx]
	m.store.Update(ticketID, func(ticket *model.Ticket) {
		ticket.Status = newStatus
	})
	m.focusedCol = colIdx
	m.reload()
	// Find the moved ticket in the new column and select it
	newColTickets := m.board.ByStatus(newStatus)
	for i, nt := range newColTickets {
		if nt.ID == ticketID {
			m.cursors[colIdx] = i
			break
		}
	}
	m.clampCursors()
}

func (m *Model) deleteTicket() {
	t := m.selectedTicket()
	if t == nil {
		return
	}
	m.store.WithLock(func() error {
		board, err := m.store.Load()
		if err != nil {
			return err
		}
		_, idx := board.FindByID(t.ID)
		if idx >= 0 {
			board.Tickets = append(board.Tickets[:idx], board.Tickets[idx+1:]...)
		}
		return m.store.Save(board)
	})
	m.reload()
	m.clampCursors()
}

func (m *Model) viewInput() string {
	return m.input.View()
}

// helpText returns context-sensitive help for the current view.
func (m *Model) helpText() string {
	switch m.view {
	case boardView:
		return "←/h →/l col  ↑/k ↓/j item  H/L move  enter detail  tab column  a add  1-5 jump  q quit"
	case columnView:
		return "tab next  1-5 jump  ↑/k ↓/j select  H/L move  enter detail  esc board  a add  q quit"
	case detailView:
		return "s status  A assign  H/L move  d delete  esc back  q quit"
	}
	return ""
}

// renderPanel draws a bordered panel with the title embedded in the top border (lazygit style).
// Example: ╭─[1] Backlog────────────╮
func renderPanel(title string, content string, width, height int, borderColor lipgloss.Color) string {
	// Border characters (rounded)
	tl, tr, bl, br := "╭", "╮", "╰", "╯"
	h, v := "─", "│"

	innerWidth := width - 2 // subtract left+right border
	if innerWidth < 1 {
		innerWidth = 1
	}

	// Top border with embedded title
	titleStr := fmt.Sprintf("%s%s", h, title)
	remaining := innerWidth - len([]rune(titleStr))
	if remaining < 0 {
		// Title too long, truncate
		runes := []rune(titleStr)
		if innerWidth > 1 {
			titleStr = string(runes[:innerWidth-1]) + "…"
		}
		remaining = 0
	}
	topBorder := tl + titleStr + strings.Repeat(h, remaining) + tr

	// Bottom border
	bottomBorder := bl + strings.Repeat(h, innerWidth) + br

	// Style the borders
	borderStyle := lipgloss.NewStyle().Foreground(borderColor)

	// Split content into lines and pad/truncate to fit
	contentLines := strings.Split(content, "\n")
	var bodyLines []string
	innerHeight := height - 2 // subtract top+bottom border
	if innerHeight < 0 {
		innerHeight = 0
	}
	for i := 0; i < innerHeight; i++ {
		line := ""
		if i < len(contentLines) {
			line = contentLines[i]
		}
		// Pad line to inner width using a fixed-width style
		paddedLine := lipgloss.NewStyle().Width(innerWidth).Render(line)
		bodyLines = append(bodyLines, borderStyle.Render(v)+paddedLine+borderStyle.Render(v))
	}

	result := borderStyle.Render(topBorder) + "\n"
	result += strings.Join(bodyLines, "\n") + "\n"
	result += borderStyle.Render(bottomBorder)

	return result
}

// viewBoard renders the board view with all columns.
func (m *Model) viewBoard() string {
	availHeight := m.height - 2 // title bar + help bar
	availWidth := m.width

	numCols := len(model.ColumnOrder)

	// Distribute widths evenly, giving focused column more space on small terminals
	colWidths := make([]int, numCols)
	if availWidth < 120 && numCols > 1 {
		focusedWidth := availWidth * 40 / 100
		remaining := availWidth - focusedWidth
		unfocusedWidth := remaining / (numCols - 1)
		for i := range colWidths {
			if i == m.focusedCol {
				colWidths[i] = focusedWidth
			} else {
				colWidths[i] = unfocusedWidth
			}
		}
	} else {
		baseWidth := availWidth / numCols
		for i := range colWidths {
			colWidths[i] = baseWidth
		}
	}
	// Give remainder to last column so total = availWidth
	total := 0
	for _, w := range colWidths {
		total += w
	}
	colWidths[numCols-1] += availWidth - total

	columns := make([]string, numCols)
	for i, status := range model.ColumnOrder {
		columns[i] = m.renderColumn(i, status, colWidths[i], availHeight, i == m.focusedCol)
	}

	board := lipgloss.JoinHorizontal(lipgloss.Top, columns...)

	title := titleBar.Render(fmt.Sprintf("kanban — %d tickets", len(m.board.Tickets)))
	help := helpStyle.Render(m.helpText())

	return lipgloss.JoinVertical(lipgloss.Left, title, board, help)
}

// renderColumn renders a single column panel with lazygit-style title in border.
func (m *Model) renderColumn(colIdx int, status model.Status, width, height int, focused bool) string {
	tickets := m.board.ByStatus(status)
	title := fmt.Sprintf("[%d] %s", colIdx+1, statusDisplay[status])

	borderColor := dimGray
	if focused {
		borderColor = green
	}

	innerWidth := width - 2
	if innerWidth < 3 {
		innerWidth = 3
	}

	// Build content lines
	var lines []string
	cursor := m.cursors[colIdx]
	for i, t := range tickets {
		if len(lines) >= height-2 {
			break
		}
		line := m.renderTicketLine(t, i == cursor && focused, innerWidth)
		lines = append(lines, line)
	}

	content := strings.Join(lines, "\n")
	return renderPanel(title, content, width, height, borderColor)
}

// renderTicketLine renders a single ticket in a column.
func (m *Model) renderTicketLine(t model.Ticket, selected bool, width int) string {
	title := t.Title
	// Reserve space for " * " prefix (3 chars) + possible "●" suffix (2 chars)
	maxTitle := width - 3
	if t.AssignedTo != "" && selected {
		maxTitle -= 2
	}
	if maxTitle < 3 {
		maxTitle = 3
	}
	if len(title) > maxTitle {
		title = title[:maxTitle-1] + "…"
	}

	if selected {
		marker := selectedMarker.Render(" * ")
		titleRendered := lipgloss.NewStyle().Bold(true).Foreground(white).Render(title)
		line := marker + titleRendered
		if t.AssignedTo != "" {
			line += " " + assigneeStyle.Render("●")
		}
		return line
	}

	return lipgloss.NewStyle().Foreground(midGray).PaddingLeft(3).Render(title)
}

// viewColumn renders the expanded single-column view.
func (m *Model) viewColumn() string {
	status := model.ColumnOrder[m.focusedCol]
	tickets := m.board.ByStatus(status)
	availHeight := m.height - 2

	title := fmt.Sprintf("[%d] %s (%d)", m.focusedCol+1, statusDisplay[status], len(tickets))

	innerWidth := m.width - 2

	var lines []string
	cursor := m.cursors[m.focusedCol]
	for i, t := range tickets {
		if len(lines) >= availHeight-2 {
			break
		}

		titleText := t.Title
		marker := "   "
		titleStyle := lipgloss.NewStyle()
		if i == cursor {
			marker = selectedMarker.Render(" * ")
			titleStyle = titleStyle.Bold(true).Foreground(white)
		} else {
			titleStyle = titleStyle.Foreground(lipgloss.Color("#CCCCCC"))
		}

		line := marker + titleStyle.Render(titleText)

		// Tags
		if len(t.Tags) > 0 {
			tags := tagStyle.Render(" #" + strings.Join(t.Tags, " #"))
			line += tags
		}
		if t.AssignedTo != "" {
			line += " " + assigneeStyle.Render("● "+t.AssignedTo)
		}

		lines = append(lines, line)

		// Show description on next line for selected item
		if i == cursor && t.Description != "" {
			desc := t.Description
			if len(desc) > innerWidth-6 {
				desc = desc[:innerWidth-7] + "…"
			}
			lines = append(lines, lipgloss.NewStyle().
				Foreground(midGray).
				PaddingLeft(4).
				Render(desc))
		}
	}

	content := strings.Join(lines, "\n")
	panel := renderPanel(title, content, m.width, availHeight, green)

	header := titleBar.Render("kanban")
	help := helpStyle.Render(m.helpText())

	return lipgloss.JoinVertical(lipgloss.Left, header, panel, help)
}

// viewDetail renders the ticket detail view.
func (m *Model) viewDetail() string {
	t := m.selectedTicket()
	if t == nil {
		m.view = boardView
		return m.viewBoard()
	}

	header := titleBar.Render("kanban — Detail")
	availHeight := m.height - 2

	innerWidth := m.width - 4
	sep := detailSep.Render(strings.Repeat("─", innerWidth))

	var lines []string
	lines = append(lines, "")
	lines = append(lines, detailTitle.Render(t.Title))
	lines = append(lines, sep)
	lines = append(lines, fmt.Sprintf("%s%s", detailLabel.Render("ID:"), detailValue.Render(t.ShortID)))
	lines = append(lines, fmt.Sprintf("%s%s", detailLabel.Render("Status:"),
		lipgloss.NewStyle().Foreground(green).Bold(true).Render(statusDisplay[t.Status])))
	if len(t.Tags) > 0 {
		lines = append(lines, fmt.Sprintf("%s%s", detailLabel.Render("Tags:"),
			tagStyle.Render(strings.Join(t.Tags, ", "))))
	}
	if t.AssignedTo != "" {
		lines = append(lines, fmt.Sprintf("%s%s", detailLabel.Render("Assigned:"),
			assigneeStyle.Render(t.AssignedTo)))
	}
	if t.CreatedBy != "" {
		lines = append(lines, fmt.Sprintf("%s%s", detailLabel.Render("Created by:"),
			detailValue.Render(t.CreatedBy)))
	}
	lines = append(lines, fmt.Sprintf("%s%s", detailLabel.Render("Created:"),
		detailValue.Render(t.CreatedAt.Format("2006-01-02 15:04"))))
	lines = append(lines, fmt.Sprintf("%s%s", detailLabel.Render("Updated:"),
		detailValue.Render(t.UpdatedAt.Format("2006-01-02 15:04"))))

	if t.Description != "" {
		lines = append(lines, sep)
		// Wrap description
		descLines := strings.Split(t.Description, "\n")
		for _, dl := range descLines {
			lines = append(lines, detailValue.Render(dl))
		}
	}
	lines = append(lines, "")

	content := strings.Join(lines, "\n")
	panel := renderPanel("Detail", content, m.width, availHeight, green)
	help := helpStyle.Render(m.helpText())

	return lipgloss.JoinVertical(lipgloss.Left, header, panel, help)
}
