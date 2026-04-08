package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/textarea"
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
	focusMode  bool // when true, only show Todo/Doing/Done

	// Selection picker state (for status)
	selectOptions []string
	selectIdx     int
	selectLabel   string
	onSelect      func(string) // called when user picks an option

	// Edit state within detail view
	editTitle    textinput.Model
	editDesc     textarea.Model
	editField    int    // 0 = metadata, 1 = title, 2 = description
	editTicketID string // ID of ticket being edited
	metaIdx      int    // selected sub-field within metadata (0=status, 1=tags, 2=assigned)
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

// visibleColumns returns the column indices currently shown.
func (m *Model) visibleColumns() []int {
	if m.focusMode {
		return []int{1, 2, 3} // Todo, Doing, Done
	}
	return []int{0, 1, 2, 3, 4}
}

// isColVisible returns whether a column index is currently visible.
func (m *Model) isColVisible(col int) bool {
	for _, c := range m.visibleColumns() {
		if c == col {
			return true
		}
	}
	return false
}

// clampFocusedCol ensures focusedCol is within visible columns.
func (m *Model) clampFocusedCol() {
	vis := m.visibleColumns()
	for _, c := range vis {
		if c == m.focusedCol {
			return
		}
	}
	// Not visible, snap to first visible
	m.focusedCol = vis[0]
}

// moveFocus moves focus left/right within visible columns.
func (m *Model) moveFocus(dir int) {
	vis := m.visibleColumns()
	curIdx := -1
	for i, c := range vis {
		if c == m.focusedCol {
			curIdx = i
			break
		}
	}
	if curIdx < 0 {
		m.focusedCol = vis[0]
		return
	}
	newIdx := curIdx + dir
	if newIdx >= 0 && newIdx < len(vis) {
		m.focusedCol = vis[newIdx]
	}
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
		m.moveFocus(-1)
	case key.Matches(msg, keys.Right):
		m.moveFocus(1)
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
			return m.enterDetail()
		}
	case key.Matches(msg, keys.Tab):
		m.view = columnView
	case key.Matches(msg, keys.Add):
		m.startInput(inputAdd, "New ticket: ")
		return m, textinput.Blink
	case key.Matches(msg, keys.Focus):
		m.focusMode = !m.focusMode
		m.clampFocusedCol()
	case key.Matches(msg, keys.One):
		if m.isColVisible(0) { m.focusedCol = 0 }
	case key.Matches(msg, keys.Two):
		if m.isColVisible(1) { m.focusedCol = 1 }
	case key.Matches(msg, keys.Three):
		if m.isColVisible(2) { m.focusedCol = 2 }
	case key.Matches(msg, keys.Four):
		if m.isColVisible(3) { m.focusedCol = 3 }
	case key.Matches(msg, keys.Five):
		if m.isColVisible(4) { m.focusedCol = 4 }
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
		m.moveFocus(1)
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
			return m.enterDetail()
		}
	case key.Matches(msg, keys.Add):
		m.startInput(inputAdd, "New ticket: ")
		return m, textinput.Blink
	case key.Matches(msg, keys.Focus):
		m.focusMode = !m.focusMode
		m.clampFocusedCol()
	case key.Matches(msg, keys.One):
		if m.isColVisible(0) { m.focusedCol = 0 }
	case key.Matches(msg, keys.Two):
		if m.isColVisible(1) { m.focusedCol = 1 }
	case key.Matches(msg, keys.Three):
		if m.isColVisible(2) { m.focusedCol = 2 }
	case key.Matches(msg, keys.Four):
		if m.isColVisible(3) { m.focusedCol = 3 }
	case key.Matches(msg, keys.Five):
		if m.isColVisible(4) { m.focusedCol = 4 }
	case key.Matches(msg, keys.MoveLeft):
		m.moveTicket(-1)
	case key.Matches(msg, keys.MoveRight):
		m.moveTicket(1)
	}
	return m, nil
}

// enterDetail sets up the detail/edit view for the selected ticket.
func (m *Model) enterDetail() (tea.Model, tea.Cmd) {
	t := m.selectedTicket()
	if t == nil {
		return m, nil
	}
	m.editTicketID = t.ID
	m.editField = 0 // start on metadata
	m.metaIdx = 0

	ti := textinput.New()
	ti.Prompt = ""
	ti.CharLimit = 200
	ti.SetValue(t.Title)
	ti.Blur()
	m.editTitle = ti

	ta := textarea.New()
	ta.Prompt = ""
	ta.SetValue(t.Description)
	ta.ShowLineNumbers = false
	ta.FocusedStyle.CursorLine = lipgloss.NewStyle()
	ta.FocusedStyle.Base = lipgloss.NewStyle()
	ta.Blur()
	m.editDesc = ta

	m.view = detailView
	return m, nil
}

// updateDetail handles keys in detail view.
// editField: 0 = metadata, 1 = title, 2 = description
func (m *Model) updateDetail(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch m.editField {
	case 0:
		return m.updateDetailMeta(msg)
	case 1:
		return m.updateDetailTitle(msg)
	case 2:
		return m.updateDetailDesc(msg)
	}
	return m, nil
}

// updateDetailMeta handles keys when metadata bar is focused.
func (m *Model) updateDetailMeta(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch {
	case key.Matches(msg, keys.Quit):
		return m, tea.Quit
	case key.Matches(msg, keys.Esc):
		m.saveEdit()
		m.view = boardView
	case key.Matches(msg, keys.Tab):
		m.editField = 1
		m.editTitle.Focus()
		return m, textinput.Blink
	case key.Matches(msg, keys.Left):
		if m.metaIdx > 0 {
			m.metaIdx--
		}
	case key.Matches(msg, keys.Right):
		if m.metaIdx < 2 {
			m.metaIdx++
		}
	case key.Matches(msg, keys.Enter):
		return m.editMetaField()
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

// editMetaField triggers inline edit for the selected metadata sub-field.
func (m *Model) editMetaField() (tea.Model, tea.Cmd) {
	switch m.metaIdx {
	case 0: // status
		m.startSelect("Status", []string{"Backlog", "Todo", "Doing", "Done", "Hold"}, func(val string) {
			status, err := model.ParseStatus(val)
			if err != nil {
				return
			}
			m.store.Update(m.editTicketID, func(ticket *model.Ticket) {
				ticket.Status = status
			})
			m.reload()
			m.clampCursors()
		})
	case 1: // tags
		t := m.selectedTicket()
		current := ""
		if t != nil && len(t.Tags) > 0 {
			current = strings.Join(t.Tags, ", ")
		}
		m.startInput(inputAssign, "Tags (comma separated): ")
		m.input.SetValue(current)
		// Override the submit to handle tags
		m.inputMode = inputAdd // reuse, we'll check prompt
		m.input.Prompt = "Tags: "
		return m, textinput.Blink
	case 2: // assigned
		m.startInput(inputAssign, "Assign to: ")
		t := m.selectedTicket()
		if t != nil {
			m.input.SetValue(t.AssignedTo)
		}
		return m, textinput.Blink
	}
	return m, nil
}

// updateDetailTitle handles keys when editing the title field.
func (m *Model) updateDetailTitle(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc":
		m.editTitle.Blur()
		m.editField = 0
		m.saveEdit()
		return m, nil
	case "tab":
		m.editField = 2
		m.editTitle.Blur()
		m.editDesc.Focus()
		return m, nil
	case "enter":
		m.saveEdit()
		m.view = boardView
		return m, nil
	}
	var cmd tea.Cmd
	m.editTitle, cmd = m.editTitle.Update(msg)
	return m, cmd
}

// updateDetailDesc handles keys when editing the description field.
func (m *Model) updateDetailDesc(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc":
		m.editDesc.Blur()
		m.editField = 0
		m.saveEdit()
		return m, nil
	case "tab":
		m.editField = 0
		m.editDesc.Blur()
		m.saveEdit()
		return m, nil
	}
	// Enter adds newline in description
	var cmd tea.Cmd
	m.editDesc, cmd = m.editDesc.Update(msg)
	return m, cmd
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

	case strings.HasPrefix(prompt, "Tags"):
		id := m.editTicketID
		if id == "" {
			return
		}
		var tags []string
		for _, t := range strings.Split(value, ",") {
			t = strings.TrimSpace(t)
			if t != "" {
				tags = append(tags, t)
			}
		}
		m.store.Update(id, func(ticket *model.Ticket) {
			ticket.Tags = tags
		})
		m.reload()

	case strings.HasPrefix(prompt, "Assign"):
		id := m.editTicketID
		if id == "" {
			t := m.selectedTicket()
			if t != nil {
				id = t.ID
			}
		}
		if id == "" {
			return
		}
		m.store.Update(id, func(ticket *model.Ticket) {
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

func (m *Model) saveEdit() {
	title := strings.TrimSpace(m.editTitle.Value())
	desc := m.editDesc.Value()

	if title == "" {
		return
	}

	m.store.Update(m.editTicketID, func(t *model.Ticket) {
		t.Title = title
		t.Description = desc
	})
	m.reload()
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
		focusLabel := "f focus"
		if m.focusMode {
			focusLabel = "f all"
		}
		return fmt.Sprintf("H/L move | enter detail | tab column | a add | %s | q quit", focusLabel)
	case columnView:
		focusLabel := "f focus"
		if m.focusMode {
			focusLabel = "f all"
		}
		return fmt.Sprintf("tab next | H/L move | enter detail | esc board | a add | %s | q quit", focusLabel)
	case detailView:
		switch m.editField {
		case 0:
			return "tab next field | h/l select | enter edit | H/L move | d delete | esc back | q quit"
		case 1:
			return "tab next field | enter save & back | esc done editing"
		case 2:
			return "tab next field | esc done editing"
		}
	}
	return ""
}

// renderPanel draws a bordered panel with the title embedded in the top border (lazygit style).
// Example: ╭─[1] Backlog────────────╮
func renderPanel(title string, content string, width, height int, borderColor lipgloss.Color, boldTitle bool) string {
	// Border characters (rounded)
	tl, tr, bl, br := "╭", "╮", "╰", "╯"
	h, v := "─", "│"

	innerWidth := width - 2 // subtract left+right border
	if innerWidth < 1 {
		innerWidth = 1
	}

	borderStyle := lipgloss.NewStyle().Foreground(borderColor)

	// Render title with optional bold
	titleStyle := lipgloss.NewStyle().Foreground(borderColor)
	if boldTitle {
		titleStyle = titleStyle.Bold(true)
	}
	renderedTitle := titleStyle.Render(title)

	// Calculate remaining border chars (use raw title length for spacing math)
	titleLen := len([]rune(title))
	remaining := innerWidth - 1 - titleLen // 1 for the leading ─
	if remaining < 0 {
		remaining = 0
	}
	topBorder := borderStyle.Render(tl+h) + renderedTitle + borderStyle.Render(strings.Repeat(h, remaining)+tr)

	// Bottom border
	bottomBorder := borderStyle.Render(bl + strings.Repeat(h, innerWidth) + br)

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

	result := topBorder + "\n"
	result += strings.Join(bodyLines, "\n") + "\n"
	result += bottomBorder

	return result
}

// viewBoard renders the board view with all columns.
func (m *Model) viewBoard() string {
	availHeight := m.height - 2 // title bar + help bar
	availWidth := m.width

	visCols := m.visibleColumns()
	numCols := len(visCols)

	// Distribute widths evenly, giving focused column more space on small terminals
	colWidths := make([]int, numCols)
	if availWidth < 120 && numCols > 1 {
		focusedIdx := -1
		for i, c := range visCols {
			if c == m.focusedCol {
				focusedIdx = i
				break
			}
		}
		focusedWidth := availWidth * 40 / 100
		remaining := availWidth - focusedWidth
		unfocusedWidth := remaining / (numCols - 1)
		for i := range colWidths {
			if i == focusedIdx {
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
	for i, colIdx := range visCols {
		status := model.ColumnOrder[colIdx]
		columns[i] = m.renderColumn(colIdx, status, colWidths[i], availHeight, colIdx == m.focusedCol)
	}

	board := lipgloss.JoinHorizontal(lipgloss.Top, columns...)

	title := titleBar.Render(fmt.Sprintf("kanban — %d tickets", len(m.board.Tickets)))
	help := helpStyle.Render(m.helpText())

	return lipgloss.JoinVertical(lipgloss.Left, title, board, help)
}

// renderColumn renders a single column panel with lazygit-style title in border.
func (m *Model) renderColumn(colIdx int, status model.Status, width, height int, focused bool) string {
	tickets := m.board.ByStatus(status)
	title := fmt.Sprintf("[%d] %s", colIdx, statusDisplay[status])

	borderColor := softWhite
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
	return renderPanel(title, content, width, height, borderColor, focused)
}

// renderTicketLine renders a single ticket in a column.
func (m *Model) renderTicketLine(t model.Ticket, selected bool, width int) string {
	title := t.Title
	// Reserve space for prefix: " * " (3 chars) if selected, " " (1 char) if not
	maxTitle := width - 1 // default for unselected
	if selected {
		maxTitle = width - 3
	}
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

	return lipgloss.NewStyle().Foreground(softWhite).PaddingLeft(1).Render(title)
}

// viewColumn renders the expanded single-column view.
func (m *Model) viewColumn() string {
	status := model.ColumnOrder[m.focusedCol]
	tickets := m.board.ByStatus(status)
	availHeight := m.height - 2

	title := fmt.Sprintf("[%d] %s (%d)", m.focusedCol, statusDisplay[status], len(tickets))

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
	panel := renderPanel(title, content, m.width, availHeight, green, true)

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

	innerWidth := m.width - 4

	// Metadata bar — navigable fields
	metaBorderColor := softWhite
	if m.editField == 0 {
		metaBorderColor = green
	}
	metaContent := m.renderMetaBar(t)
	metaPanel := renderPanel("Info", metaContent, innerWidth+2, 3, metaBorderColor, m.editField == 0)

	// Title field — 1 line tall
	titleBorderColor := softWhite
	if m.editField == 1 {
		titleBorderColor = green
	}
	m.editTitle.Width = innerWidth - 2
	titlePanel := renderPanel("Title", m.editTitle.View(), innerWidth+2, 3, titleBorderColor, m.editField == 1)

	// Description field — fills remaining space
	descBorderColor := softWhite
	if m.editField == 2 {
		descBorderColor = green
	}
	// Height: total - meta(3) - title(3) - help(1)
	descPanelHeight := m.height - 7
	if descPanelHeight < 4 {
		descPanelHeight = 4
	}
	m.editDesc.SetWidth(innerWidth - 2)
	m.editDesc.SetHeight(descPanelHeight - 2)
	descPanel := renderPanel("Description", m.editDesc.View(), innerWidth+2, descPanelHeight, descBorderColor, m.editField == 2)

	help := helpStyle.Render(m.helpText())

	return lipgloss.JoinVertical(lipgloss.Left,
		metaPanel,
		titlePanel,
		descPanel,
		help,
	)
}

// renderMetaBar renders the metadata fields with the selected one highlighted.
func (m *Model) renderMetaBar(t *model.Ticket) string {
	isMeta := m.editField == 0

	statusText := statusDisplay[t.Status]
	tagsText := "no tags"
	if len(t.Tags) > 0 {
		tagsText = "#" + strings.Join(t.Tags, " #")
	}
	assignText := "unassigned"
	if t.AssignedTo != "" {
		assignText = "● " + t.AssignedTo
	}

	fields := []struct {
		label string
		value string
		style lipgloss.Style
	}{
		{"status", statusText, lipgloss.NewStyle().Foreground(green).Bold(true)},
		{"tags", tagsText, tagStyle},
		{"assigned", assignText, assigneeStyle},
	}

	var parts []string
	for i, f := range fields {
		rendered := f.style.Render(f.value)
		if isMeta && i == m.metaIdx {
			// Highlight selected field
			rendered = lipgloss.NewStyle().
				Background(lipgloss.Color("#313244")).
				Bold(true).
				Foreground(white).
				Padding(0, 1).
				Render(f.value)
		}
		parts = append(parts, rendered)
	}

	// Add read-only info at the end
	parts = append(parts, lipgloss.NewStyle().Foreground(midGray).Render(t.ShortID))
	parts = append(parts, lipgloss.NewStyle().Foreground(midGray).Render(t.CreatedAt.Format("2006-01-02 15:04")))

	return strings.Join(parts, "  ")
}
