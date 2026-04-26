package tui

import (
	"fmt"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/textarea"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/leon/kanban/internal/model"
	"github.com/leon/kanban/internal/store"
)

type viewMode int

const (
	boardView   viewMode = iota
	splitView            // list + detail side by side
	columnView           // full-width single column
	detailView           // full-screen detail editor
	archiveView          // archive browser (split: list + read-only detail)
	addView              // floating popup for new ticket
	pickerView           // floating board picker (main + sprints)
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

// statusShort is the compact label used in the board picker count strip.
var statusShort = map[model.Status]string{
	model.StatusBacklog: "B",
	model.StatusTodo:    "T",
	model.StatusDoing:   "Do",
	model.StatusDone:    "Dn",
	model.StatusHold:    "H",
}

var (
	dimStyle          = lipgloss.NewStyle().Foreground(dimGray)
	statusCountStyles = buildStatusCountStyles()
)

func buildStatusCountStyles() map[model.Status]lipgloss.Style {
	out := make(map[model.Status]lipgloss.Style, len(model.AllStatuses))
	for _, s := range model.AllStatuses {
		out[s] = lipgloss.NewStyle().Foreground(columnColor(s))
	}
	return out
}

type Model struct {
	store      *store.Store
	board      *model.Board
	sprintName string // empty for main board
	width      int
	height     int
	ready      bool
	view       viewMode
	focusedCol int    // index into model.ColumnOrder
	cursors    [5]int // selected item index per column
	input      textinput.Model
	inputMode  inputMode
	err        error

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

	// Split view state
	splitFocus int // 0 = list panel, 1 = detail panel

	// Board layout toggle. false = columns (default), true = rows.
	rowLayout bool

	// Archive view state
	archiveEntries []archiveEntry
	archiveCursor  int

	// Add popup state
	addTitle       textinput.Model
	addDesc        textarea.Model
	addTags        textinput.Model
	addAssign      textinput.Model
	addFocusIdx    int
	addDescEditing bool

	// Board picker state
	pickerBoards []pickerEntry
	pickerIdx    int
	pickerWidth  int

	lastModTime time.Time // last known mod time of board.json
}

// archiveEntry is a single row in the archive browser — either a date header
// or a ticket.
type archiveEntry struct {
	isHeader bool
	date     string // YYYY-MM-DD, set when isHeader
	ticket   model.Ticket
}

// pickerEntry is one row in the board picker — the main board or a sprint.
type pickerEntry struct {
	name   string // "" for main
	counts map[model.Status]int
}

// boardDisplayName resolves "" to "main"; sprint names pass through.
func boardDisplayName(sprintName string) string {
	if sprintName == "" {
		return "main"
	}
	return sprintName
}

type tickMsg time.Time

func tickCmd() tea.Cmd {
	return tea.Tick(2*time.Second, func(t time.Time) tea.Msg {
		return tickMsg(t)
	})
}

func NewModel(s *store.Store, sprintName string) (*Model, error) {
	board, err := s.Load()
	if err != nil {
		return nil, err
	}

	ti := textinput.New()
	ti.CharLimit = 200

	var modTime time.Time
	if info, err := os.Stat(s.BoardPath()); err == nil {
		modTime = info.ModTime()
	}

	return &Model{
		store:       s,
		board:       board,
		sprintName:  sprintName,
		input:       ti,
		focusedCol:  1, // default to Todo
		lastModTime: modTime,
	}, nil
}

func (m *Model) footerLine() string {
	help := helpStyle.Render(m.helpText())
	badge := sprintBadgeStyle.Render(boardDisplayName(m.sprintName))
	return lipgloss.JoinHorizontal(lipgloss.Center, badge, help)
}

func (m *Model) Init() tea.Cmd {
	return tickCmd()
}

func (m *Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tickMsg:
		if info, err := os.Stat(m.store.BoardPath()); err == nil {
			if info.ModTime().After(m.lastModTime) {
				m.lastModTime = info.ModTime()
				m.reload()
				m.clampCursors()
			}
		}
		return m, tickCmd()

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
		case splitView:
			return m.updateSplit(msg)
		case columnView:
			return m.updateColumn(msg)
		case detailView:
			return m.updateDetail(msg)
		case archiveView:
			return m.updateArchive(msg)
		case addView:
			return m.updateAdd(msg)
		case pickerView:
			return m.updatePicker(msg)
		}
	}
	return m, nil
}

func (m *Model) View() string {
	if !m.ready {
		return "Loading..."
	}

	if m.width < minTerminalWidth || m.height < minTerminalHeight {
		return m.viewTooSmall()
	}

	var content string
	switch m.view {
	case boardView:
		content = m.viewBoard()
	case splitView:
		content = m.viewSplit()
	case columnView:
		content = m.viewColumn()
	case detailView:
		content = m.viewDetail()
	case archiveView:
		content = m.viewArchive()
	case addView:
		content = m.viewAdd()
	case pickerView:
		content = m.viewPicker()
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

// wideLayoutMinWidth is the terminal width above which all 5 columns render
// side-by-side. Below it, a 3-column sliding window centered on focus is used.
const wideLayoutMinWidth = 150

// tallLayoutMinHeight is the same idea for row layout, against height.
const tallLayoutMinHeight = 30

// Minimum terminal dimensions for a usable TUI render. Below this, we show a
// placeholder instead of a mangled layout.
const (
	minTerminalWidth  = 50
	minTerminalHeight = 10
)

// visibleColumns returns the column indices currently rendered.
// Wide terminals show all 5 columns. Narrower ones show a 3-column window
// that sits at [1,2,3] by default; only the edge columns (0 and 4) drag the
// window sideways, giving a "peek" into Backlog or Hold.
func (m *Model) visibleColumns() []int {
	return slidingWindow(m.width >= wideLayoutMinWidth, m.focusedCol)
}

// visibleRows is the row-layout analogue of visibleColumns: tall terminals
// show all 5 rows, shorter ones slide a 3-row window.
func (m *Model) visibleRows() []int {
	return slidingWindow(m.height >= tallLayoutMinHeight, m.focusedCol)
}

func slidingWindow(showAll bool, focused int) []int {
	if showAll {
		return []int{0, 1, 2, 3, 4}
	}
	switch focused {
	case 0:
		return []int{0, 1, 2}
	case 4:
		return []int{2, 3, 4}
	default:
		return []int{1, 2, 3}
	}
}

// moveFocus moves focus left/right through all columns (0..4).
// The visible window re-centers on the next render.
func (m *Model) moveFocus(dir int) {
	next := m.focusedCol + dir
	if next < 0 || next > 4 {
		return
	}
	m.focusedCol = next
}

// moveCursor moves the selection cursor within the focused column's ticket list.
func (m *Model) moveCursor(dir int) {
	if dir < 0 {
		if m.cursors[m.focusedCol] > 0 {
			m.cursors[m.focusedCol]--
		}
		return
	}
	status := model.ColumnOrder[m.focusedCol]
	count := len(m.board.ByStatus(status))
	if m.cursors[m.focusedCol] < count-1 {
		m.cursors[m.focusedCol]++
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

// ─── Board view ──────────────────────────────────────────────────────

func (m *Model) updateBoard(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch {
	case key.Matches(msg, keys.Quit):
		return m, tea.Quit
	case key.Matches(msg, keys.Left):
		m.moveFocus(-1)
	case key.Matches(msg, keys.Right):
		m.moveFocus(1)
	case key.Matches(msg, keys.Up):
		m.moveCursor(-1)
	case key.Matches(msg, keys.Down):
		m.moveCursor(1)
	case key.Matches(msg, keys.Enter), key.Matches(msg, keys.Zoom):
		m.enterSplit()
		return m, nil
	case key.Matches(msg, keys.Add):
		return m.enterAddPopup()
	case key.Matches(msg, keys.Zero):
		m.focusedCol = 0
	case key.Matches(msg, keys.One):
		m.focusedCol = 1
	case key.Matches(msg, keys.Two):
		m.focusedCol = 2
	case key.Matches(msg, keys.Three):
		m.focusedCol = 3
	case key.Matches(msg, keys.Four):
		m.focusedCol = 4
	case key.Matches(msg, keys.MoveLeft):
		m.moveTicket(-1)
	case key.Matches(msg, keys.MoveRight):
		m.moveTicket(1)
	case key.Matches(msg, keys.Archive):
		m.archiveTicket()
	case key.Matches(msg, keys.Layout):
		m.rowLayout = !m.rowLayout
	case key.Matches(msg, keys.ArchiveView):
		m.enterArchive()
	case key.Matches(msg, keys.BoardPicker):
		return m.enterPicker()
	}
	return m, nil
}

// ─── Split view ──────────────────────────────────────────────────────

func (m *Model) enterSplit() {
	m.splitFocus = 0 // start on list
	m.refreshDetailEditors()
	m.view = splitView
}

// refreshDetailEditors sets up the edit widgets for the currently selected ticket.
func (m *Model) refreshDetailEditors() {
	t := m.selectedTicket()
	if t == nil {
		m.editTicketID = ""
		return
	}
	m.editTicketID = t.ID
	m.editField = 0
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
}

func (m *Model) updateSplit(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if m.splitFocus == 0 {
		return m.updateSplitList(msg)
	}
	return m.updateSplitDetail(msg)
}

func (m *Model) updateSplitList(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch {
	case key.Matches(msg, keys.Quit):
		return m, tea.Quit
	case key.Matches(msg, keys.Unzoom), key.Matches(msg, keys.Esc):
		m.view = boardView
	case key.Matches(msg, keys.Zoom):
		m.view = columnView
	case key.Matches(msg, keys.PanelNext), key.Matches(msg, keys.Enter), key.Matches(msg, keys.Right):
		m.splitFocus = 1
		m.refreshDetailEditors() // start on meta, nothing focused
	case key.Matches(msg, keys.Up):
		if m.cursors[m.focusedCol] > 0 {
			m.cursors[m.focusedCol]--
			m.refreshDetailEditors()
		}
	case key.Matches(msg, keys.Down):
		status := model.ColumnOrder[m.focusedCol]
		count := len(m.board.ByStatus(status))
		if m.cursors[m.focusedCol] < count-1 {
			m.cursors[m.focusedCol]++
			m.refreshDetailEditors()
		}
	case key.Matches(msg, keys.Add):
		return m.enterAddPopup()
	case key.Matches(msg, keys.Zero):
		m.focusedCol = 0
		m.refreshDetailEditors()
	case key.Matches(msg, keys.One):
		m.focusedCol = 1
		m.refreshDetailEditors()
	case key.Matches(msg, keys.Two):
		m.focusedCol = 2
		m.refreshDetailEditors()
	case key.Matches(msg, keys.Three):
		m.focusedCol = 3
		m.refreshDetailEditors()
	case key.Matches(msg, keys.Four):
		m.focusedCol = 4
		m.refreshDetailEditors()
	case key.Matches(msg, keys.MoveLeft):
		m.moveTicket(-1)
		m.refreshDetailEditors()
	case key.Matches(msg, keys.MoveRight):
		m.moveTicket(1)
		m.refreshDetailEditors()
	case key.Matches(msg, keys.Archive):
		m.archiveTicket()
		m.refreshDetailEditors()
	case key.Matches(msg, keys.ArchiveView):
		m.enterArchive()
	}
	return m, nil
}

func (m *Model) updateSplitDetail(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch m.editField {
	case 0: // metadata bar
		return m.updateSplitDetailMeta(msg)
	case 1: // title
		return m.updateSplitDetailTitle(msg)
	case 2: // description
		return m.updateSplitDetailDesc(msg)
	}
	return m, nil
}

// jumpDetailCol changes focus to another column from within a detail view
// and re-seeds the edit widgets with the new ticket's data.
func (m *Model) jumpDetailCol(col int) {
	m.focusedCol = col
	m.refreshDetailEditors()
}

func (m *Model) updateSplitDetailMeta(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch {
	case key.Matches(msg, keys.Quit):
		return m, tea.Quit
	case key.Matches(msg, keys.Left):
		if m.metaIdx > 0 {
			m.metaIdx--
		} else {
			m.splitFocus = 0
		}
	case key.Matches(msg, keys.Right):
		if m.metaIdx < 2 {
			m.metaIdx++
		}
	case key.Matches(msg, keys.PanelPrev), key.Matches(msg, keys.Esc):
		m.splitFocus = 0
	case key.Matches(msg, keys.Unzoom):
		m.splitFocus = 0
		m.view = boardView
	case key.Matches(msg, keys.Zoom):
		m.enterDetail()
		return m, nil
	case key.Matches(msg, keys.Down):
		m.editField = 1
	case key.Matches(msg, keys.Enter):
		return m.editMetaField()
	case key.Matches(msg, keys.Delete):
		m.deleteTicket()
		m.splitFocus = 0
		m.refreshDetailEditors()
	case key.Matches(msg, keys.Archive):
		m.archiveTicket()
		m.splitFocus = 0
		m.refreshDetailEditors()
	case key.Matches(msg, keys.MoveLeft):
		m.moveTicket(-1)
		m.refreshDetailEditors()
	case key.Matches(msg, keys.MoveRight):
		m.moveTicket(1)
		m.refreshDetailEditors()
	case key.Matches(msg, keys.Zero):
		m.jumpDetailCol(0)
	case key.Matches(msg, keys.One):
		m.jumpDetailCol(1)
	case key.Matches(msg, keys.Two):
		m.jumpDetailCol(2)
	case key.Matches(msg, keys.Three):
		m.jumpDetailCol(3)
	case key.Matches(msg, keys.Four):
		m.jumpDetailCol(4)
	}
	return m, nil
}

func (m *Model) updateSplitDetailTitle(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if m.editTitle.Focused() {
		// Editing mode
		switch msg.String() {
		case "esc":
			m.editTitle.Blur()
			m.saveEdit()
			return m, nil
		case "enter":
			m.editTitle.Blur()
			m.saveEdit()
			return m, nil
		}
		var cmd tea.Cmd
		m.editTitle, cmd = m.editTitle.Update(msg)
		return m, cmd
	}
	// Viewing mode — hjkl to navigate fields
	switch {
	case key.Matches(msg, keys.Quit):
		return m, tea.Quit
	case key.Matches(msg, keys.Up):
		m.editField = 0
	case key.Matches(msg, keys.Down):
		m.editField = 2
	case key.Matches(msg, keys.Left):
		m.splitFocus = 0
	case key.Matches(msg, keys.Enter), key.Matches(msg, keys.Edit):
		m.editTitle.Focus()
		return m, textinput.Blink
	case key.Matches(msg, keys.PanelPrev), key.Matches(msg, keys.Esc):
		m.splitFocus = 0
	case key.Matches(msg, keys.Unzoom):
		m.splitFocus = 0
		m.view = boardView
	case key.Matches(msg, keys.Zoom):
		m.enterDetail()
		return m, nil
	case key.Matches(msg, keys.MoveLeft):
		m.moveTicket(-1)
		m.refreshDetailEditors()
	case key.Matches(msg, keys.MoveRight):
		m.moveTicket(1)
		m.refreshDetailEditors()
	case key.Matches(msg, keys.Zero):
		m.jumpDetailCol(0)
	case key.Matches(msg, keys.One):
		m.jumpDetailCol(1)
	case key.Matches(msg, keys.Two):
		m.jumpDetailCol(2)
	case key.Matches(msg, keys.Three):
		m.jumpDetailCol(3)
	case key.Matches(msg, keys.Four):
		m.jumpDetailCol(4)
	}
	return m, nil
}

func (m *Model) updateSplitDetailDesc(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if m.editDesc.Focused() {
		// Editing mode
		switch msg.String() {
		case "esc":
			m.editDesc.Blur()
			m.saveEdit()
			return m, nil
		}
		var cmd tea.Cmd
		m.editDesc, cmd = m.editDesc.Update(msg)
		return m, cmd
	}
	// Viewing mode — hjkl to navigate fields
	switch {
	case key.Matches(msg, keys.Quit):
		return m, tea.Quit
	case key.Matches(msg, keys.Up):
		m.editField = 1
	case key.Matches(msg, keys.Left):
		m.splitFocus = 0
	case key.Matches(msg, keys.Enter), key.Matches(msg, keys.Edit):
		m.editDesc.Focus()
		return m, nil
	case key.Matches(msg, keys.PanelPrev), key.Matches(msg, keys.Esc):
		m.splitFocus = 0
	case key.Matches(msg, keys.Unzoom):
		m.splitFocus = 0
		m.view = boardView
	case key.Matches(msg, keys.Zoom):
		m.enterDetail()
		return m, nil
	case key.Matches(msg, keys.MoveLeft):
		m.moveTicket(-1)
		m.refreshDetailEditors()
	case key.Matches(msg, keys.MoveRight):
		m.moveTicket(1)
		m.refreshDetailEditors()
	case key.Matches(msg, keys.Zero):
		m.jumpDetailCol(0)
	case key.Matches(msg, keys.One):
		m.jumpDetailCol(1)
	case key.Matches(msg, keys.Two):
		m.jumpDetailCol(2)
	case key.Matches(msg, keys.Three):
		m.jumpDetailCol(3)
	case key.Matches(msg, keys.Four):
		m.jumpDetailCol(4)
	}
	return m, nil
}

// ─── Column view (full-width list) ──────────────────────────────────

func (m *Model) updateColumn(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch {
	case key.Matches(msg, keys.Quit):
		return m, tea.Quit
	case key.Matches(msg, keys.Unzoom), key.Matches(msg, keys.Esc):
		m.enterSplit()
		return m, nil
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
		return m.enterAddPopup()
	case key.Matches(msg, keys.Zero):
		m.focusedCol = 0
	case key.Matches(msg, keys.One):
		m.focusedCol = 1
	case key.Matches(msg, keys.Two):
		m.focusedCol = 2
	case key.Matches(msg, keys.Three):
		m.focusedCol = 3
	case key.Matches(msg, keys.Four):
		m.focusedCol = 4
	case key.Matches(msg, keys.MoveLeft):
		m.moveTicket(-1)
	case key.Matches(msg, keys.MoveRight):
		m.moveTicket(1)
	case key.Matches(msg, keys.Archive):
		m.archiveTicket()
	}
	return m, nil
}

// ─── Detail view (full-screen editor) ───────────────────────────────

func (m *Model) enterDetail() (tea.Model, tea.Cmd) {
	t := m.selectedTicket()
	if t == nil {
		return m, nil
	}
	m.editTicketID = t.ID
	m.editField = 0
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

func (m *Model) updateDetailMeta(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch {
	case key.Matches(msg, keys.Quit):
		return m, tea.Quit
	case key.Matches(msg, keys.Esc), key.Matches(msg, keys.Unzoom):
		m.saveEdit()
		m.enterSplit()
		return m, nil
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
	case key.Matches(msg, keys.Archive):
		m.archiveTicket()
		m.view = boardView
	case key.Matches(msg, keys.MoveLeft):
		m.moveTicket(-1)
	case key.Matches(msg, keys.MoveRight):
		m.moveTicket(1)
	case key.Matches(msg, keys.Zero):
		m.jumpDetailCol(0)
	case key.Matches(msg, keys.One):
		m.jumpDetailCol(1)
	case key.Matches(msg, keys.Two):
		m.jumpDetailCol(2)
	case key.Matches(msg, keys.Three):
		m.jumpDetailCol(3)
	case key.Matches(msg, keys.Four):
		m.jumpDetailCol(4)
	}
	return m, nil
}

func (m *Model) editMetaField() (tea.Model, tea.Cmd) {
	switch m.metaIdx {
	case 0: // status
		m.startSelect("Status", []string{"Todo", "Doing", "Done", "Hold"}, func(val string) {
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
	case 1: // assigned
		m.startInput(inputAssign, "Assign to: ")
		t := m.selectedTicket()
		if t != nil {
			m.input.SetValue(t.AssignedTo)
		}
		return m, textinput.Blink
	case 2: // tags
		t := m.selectedTicket()
		current := ""
		if t != nil && len(t.Tags) > 0 {
			current = strings.Join(t.Tags, ", ")
		}
		m.startInput(inputAssign, "Tags (comma separated): ")
		m.input.SetValue(current)
		m.inputMode = inputAdd
		m.input.Prompt = "Tags: "
		return m, textinput.Blink
	}
	return m, nil
}

func (m *Model) updateDetailTitle(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc":
		m.editTitle.Blur()
		m.editField = 0
		m.saveEdit()
		return m, nil
	case "enter":
		m.editTitle.Blur()
		m.editField = 0
		m.saveEdit()
		return m, nil
	}
	var cmd tea.Cmd
	m.editTitle, cmd = m.editTitle.Update(msg)
	return m, cmd
}

func (m *Model) updateDetailDesc(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc":
		m.editDesc.Blur()
		m.editField = 0
		m.saveEdit()
		return m, nil
	}
	var cmd tea.Cmd
	m.editDesc, cmd = m.editDesc.Update(msg)
	return m, cmd
}

// ─── Input / selection helpers ──────────────────────────────────────

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

// ─── Persistence helpers ────────────────────────────────────────────

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
	newColTickets := m.board.ByStatus(newStatus)
	for i, nt := range newColTickets {
		if nt.ID == ticketID {
			m.cursors[colIdx] = i
			break
		}
	}
	m.clampCursors()
}

func (m *Model) archiveTicket() {
	t := m.selectedTicket()
	if t == nil {
		return
	}
	m.store.ArchiveByID(t.ID)
	m.reload()
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

// ─── Archive view ───────────────────────────────────────────────────

func (m *Model) enterArchive() {
	arch, err := m.store.LoadArchive()
	if err != nil {
		m.err = err
		return
	}
	m.archiveEntries = buildArchiveEntries(arch.Tickets)
	m.archiveCursor = firstTicketIdx(m.archiveEntries)
	m.view = archiveView
}

func (m *Model) updateArchive(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch {
	case key.Matches(msg, keys.Quit):
		return m, tea.Quit
	case key.Matches(msg, keys.Esc), key.Matches(msg, keys.Unzoom), key.Matches(msg, keys.ArchiveView):
		m.view = boardView
	case key.Matches(msg, keys.Up):
		m.moveArchiveCursor(-1)
	case key.Matches(msg, keys.Down):
		m.moveArchiveCursor(1)
	case key.Matches(msg, keys.Unarchive):
		m.unarchiveSelected()
	}
	return m, nil
}

func (m *Model) moveArchiveCursor(dir int) {
	n := len(m.archiveEntries)
	if n == 0 {
		return
	}
	i := m.archiveCursor + dir
	for i >= 0 && i < n && m.archiveEntries[i].isHeader {
		i += dir
	}
	if i < 0 || i >= n {
		return
	}
	m.archiveCursor = i
}

func (m *Model) archiveSelected() *model.Ticket {
	if m.archiveCursor < 0 || m.archiveCursor >= len(m.archiveEntries) {
		return nil
	}
	e := &m.archiveEntries[m.archiveCursor]
	if e.isHeader {
		return nil
	}
	return &e.ticket
}

func (m *Model) unarchiveSelected() {
	t := m.archiveSelected()
	if t == nil {
		return
	}
	if err := m.store.Unarchive(t.ID); err != nil {
		m.err = err
		return
	}
	m.reload()
	m.clampCursors()
	arch, err := m.store.LoadArchive()
	if err != nil {
		m.err = err
		return
	}
	m.archiveEntries = buildArchiveEntries(arch.Tickets)
	if m.archiveCursor >= len(m.archiveEntries) {
		m.archiveCursor = len(m.archiveEntries) - 1
	}
	if m.archiveCursor < 0 {
		m.archiveCursor = 0
	}
	for m.archiveCursor < len(m.archiveEntries) && m.archiveEntries[m.archiveCursor].isHeader {
		m.archiveCursor++
	}
}

// buildArchiveEntries sorts tickets newest-archived-first and inserts date
// header rows between groups.
func buildArchiveEntries(tickets []model.Ticket) []archiveEntry {
	if len(tickets) == 0 {
		return nil
	}
	sorted := make([]model.Ticket, len(tickets))
	copy(sorted, tickets)
	sort.SliceStable(sorted, func(i, j int) bool {
		return archiveDate(sorted[i]).After(archiveDate(sorted[j]))
	})
	var entries []archiveEntry
	var lastDate string
	for _, t := range sorted {
		d := archiveDate(t).Format("2006-01-02")
		if d != lastDate {
			entries = append(entries, archiveEntry{isHeader: true, date: d})
			lastDate = d
		}
		entries = append(entries, archiveEntry{ticket: t})
	}
	return entries
}

// archiveDate returns the best-known archive timestamp. Falls back to
// UpdatedAt for archive entries written before ArchivedAt was added.
func archiveDate(t model.Ticket) time.Time {
	if t.ArchivedAt != nil {
		return *t.ArchivedAt
	}
	return t.UpdatedAt
}

func firstTicketIdx(entries []archiveEntry) int {
	for i, e := range entries {
		if !e.isHeader {
			return i
		}
	}
	return 0
}

func countArchiveTickets(entries []archiveEntry) int {
	n := 0
	for _, e := range entries {
		if !e.isHeader {
			n++
		}
	}
	return n
}

func (m *Model) viewArchive() string {
	availHeight := m.height - 1
	availWidth := m.width

	listWidth := availWidth * 35 / 100
	if listWidth < 20 {
		listWidth = 20
	}
	detailWidth := availWidth - listWidth

	listPanel := m.renderArchiveList(listWidth, availHeight)
	detailPanel := m.renderArchiveDetail(detailWidth, availHeight)
	body := lipgloss.JoinHorizontal(lipgloss.Top, listPanel, detailPanel)
	return lipgloss.JoinVertical(lipgloss.Left, body, m.footerLine())
}

func (m *Model) renderArchiveList(width, height int) string {
	title := fmt.Sprintf("Archive (%d)", countArchiveTickets(m.archiveEntries))
	innerWidth := width - 2
	if innerWidth < 3 {
		innerWidth = 3
	}

	visibleCount := height - 2
	if visibleCount < 1 {
		visibleCount = 1
	}

	startIdx := 0
	if m.archiveCursor >= visibleCount {
		startIdx = m.archiveCursor - visibleCount + 1
	}

	var lines []string
	for i := startIdx; i < len(m.archiveEntries) && len(lines) < visibleCount; i++ {
		e := m.archiveEntries[i]
		if e.isHeader {
			header := "── " + e.date + " "
			pad := innerWidth - len([]rune(header))
			if pad < 0 {
				pad = 0
			}
			header += strings.Repeat("─", pad)
			lines = append(lines, lipgloss.NewStyle().Foreground(midGray).Render(header))
			continue
		}
		selected := i == m.archiveCursor
		lines = append(lines, m.renderTicketLine(e.ticket, selected, innerWidth, green))
	}

	content := strings.Join(lines, "\n")
	if content == "" {
		content = lipgloss.NewStyle().Foreground(subtle).Render("(empty)")
	}
	return renderPanel(title, content, width, height, green, true)
}

func (m *Model) renderArchiveDetail(width, height int) string {
	t := m.archiveSelected()
	if t == nil {
		return renderPanel("Ticket", "", width, height, softWhite, false)
	}

	innerWidth := width - 4
	if innerWidth < 1 {
		innerWidth = 1
	}

	metaContent := m.renderArchiveMeta(t, innerWidth)
	metaPanel := renderPanel("Info", metaContent, width, 3, softWhite, false)

	titleContent := lipgloss.NewStyle().Bold(true).Foreground(white).Render(t.Title)
	titlePanel := renderPanel("Title", titleContent, width, 3, softWhite, false)

	descPanelHeight := height - 6
	if descPanelHeight < 4 {
		descPanelHeight = 4
	}
	var descContent string
	if t.Description == "" {
		descContent = lipgloss.NewStyle().Foreground(subtle).Render("(empty)")
	} else {
		wrapped := lipgloss.NewStyle().Width(innerWidth).Render(t.Description)
		descContent = lipgloss.NewStyle().Foreground(softWhite).Render(wrapped)
	}
	descPanel := renderPanel("Description", descContent, width, descPanelHeight, softWhite, false)

	return lipgloss.JoinVertical(lipgloss.Left, metaPanel, titlePanel, descPanel)
}

func (m *Model) renderArchiveMeta(t *model.Ticket, maxWidth int) string {
	statusText := statusDisplay[t.Status]
	statusColor := columnColor(t.Status)

	archivedText := archiveDate(*t).Format("2006-01-02")

	tagsText := ""
	if len(t.Tags) > 0 {
		tagsText = "#" + strings.Join(t.Tags, " #")
	}
	assignText := ""
	if t.AssignedTo != "" {
		assignText = "● " + t.AssignedTo
	}

	parts := []string{
		lipgloss.NewStyle().Foreground(statusColor).Bold(true).Render(statusText),
		lipgloss.NewStyle().Foreground(midGray).Render("archived " + archivedText),
	}
	if tagsText != "" {
		parts = append(parts, tagStyle.Render(tagsText))
	}
	if assignText != "" {
		parts = append(parts, assigneeStyle.Render(assignText))
	}
	parts = append(parts, lipgloss.NewStyle().Foreground(midGray).Render(t.ShortID))
	return strings.Join(parts, "  ")
}

// ─── Add popup ──────────────────────────────────────────────────────

// addFocusIdx values. The numeric order is also the tab cycle order:
// assign → tags → title → description → (wrap).
const (
	addFocusAssign = iota
	addFocusTags
	addFocusTitle
	addFocusDesc
)

func (m *Model) enterAddPopup() (tea.Model, tea.Cmd) {
	ti := textinput.New()
	ti.Prompt = ""
	ti.CharLimit = 200
	ti.Focus()
	m.addTitle = ti

	ta := textarea.New()
	ta.Prompt = ""
	ta.ShowLineNumbers = false
	ta.FocusedStyle.CursorLine = lipgloss.NewStyle()
	ta.FocusedStyle.Base = lipgloss.NewStyle()
	ta.Blur()
	m.addDesc = ta

	tagsIn := textinput.New()
	tagsIn.Prompt = ""
	tagsIn.Placeholder = "+tag"
	tagsIn.CharLimit = 100
	tagsIn.Blur()
	m.addTags = tagsIn

	assignIn := textinput.New()
	assignIn.Prompt = ""
	assignIn.Placeholder = "+assign"
	assignIn.CharLimit = 50
	assignIn.Blur()
	m.addAssign = assignIn

	m.addFocusIdx = addFocusTitle
	m.addDescEditing = false
	m.view = addView
	return m, textinput.Blink
}

func (m *Model) updateAdd(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if m.addFocusIdx == addFocusDesc && m.addDescEditing {
		if msg.String() == "esc" {
			m.addDesc.Blur()
			m.addDescEditing = false
			return m, nil
		}
		var cmd tea.Cmd
		m.addDesc, cmd = m.addDesc.Update(msg)
		return m, cmd
	}

	switch msg.String() {
	case "esc":
		m.view = boardView
		return m, nil
	case "tab":
		m.cycleAddField(1)
		return m, nil
	case "shift+tab":
		m.cycleAddField(-1)
		return m, nil
	}

	if m.addFocusIdx == addFocusDesc {
		switch msg.String() {
		case "enter":
			m.addDesc.Focus()
			m.addDescEditing = true
			return m, textarea.Blink
		case "h", "k":
			m.cycleAddField(-1)
			return m, nil
		case "l", "j":
			m.cycleAddField(1)
			return m, nil
		}
		return m, nil
	}

	if m.addFocusIdx == addFocusTitle {
		if msg.String() == "enter" {
			m.submitAdd()
			return m, nil
		}
		var cmd tea.Cmd
		m.addTitle, cmd = m.addTitle.Update(msg)
		return m, cmd
	}

	// Tags / assign share behaviour: enter is a no-op (so it doesn't submit
	// from a half-filled input), any other keystroke types into the widget.
	if msg.String() == "enter" {
		return m, nil
	}
	var cmd tea.Cmd
	switch m.addFocusIdx {
	case addFocusTags:
		m.addTags, cmd = m.addTags.Update(msg)
	case addFocusAssign:
		m.addAssign, cmd = m.addAssign.Update(msg)
	}
	return m, cmd
}

func (m *Model) cycleAddField(dir int) {
	switch m.addFocusIdx {
	case addFocusAssign:
		m.addAssign.Blur()
	case addFocusTags:
		m.addTags.Blur()
	case addFocusTitle:
		m.addTitle.Blur()
	case addFocusDesc:
		m.addDesc.Blur()
		m.addDescEditing = false
	}
	m.addFocusIdx = (m.addFocusIdx + dir + 4) % 4
	switch m.addFocusIdx {
	case addFocusAssign:
		m.addAssign.Focus()
	case addFocusTags:
		m.addTags.Focus()
	case addFocusTitle:
		m.addTitle.Focus()
		// addFocusDesc arrives in nav mode — not focused.
	}
}

func (m *Model) submitAdd() {
	title := strings.TrimSpace(m.addTitle.Value())
	if title == "" {
		return // silent no-op: title is the only required field.
	}
	desc := m.addDesc.Value()
	var tags []string
	for _, t := range strings.Split(m.addTags.Value(), ",") {
		t = strings.TrimSpace(t)
		if t != "" {
			tags = append(tags, t)
		}
	}
	assign := strings.TrimSpace(m.addAssign.Value())
	status := model.ColumnOrder[m.focusedCol]

	if _, err := m.store.Add(title, desc, status, tags, assign, "tui"); err != nil {
		m.err = err
		return
	}
	m.reload()
	// Store.Add appends, so the new ticket is last within its status column.
	m.cursors[m.focusedCol] = len(m.board.ByStatus(status)) - 1
	m.clampCursors()
	m.view = boardView
}

func (m *Model) viewAdd() string {
	popupWidth := 66
	if popupWidth > m.width-4 {
		popupWidth = m.width - 4
	}
	if popupWidth < 30 {
		popupWidth = 30
	}
	popupHeight := 28
	if popupHeight > m.height-4 {
		popupHeight = m.height - 4
	}
	if popupHeight < 12 {
		popupHeight = 12
	}

	popup := m.renderAddPopup(popupWidth, popupHeight)
	return m.centerOverPopup(popup, popupWidth, popupHeight)
}

// centerOverPopup overlays a popup on top of the board, centered. Vertical
// center uses height-1 so the popup sits centered on the board, not pushed
// down by the footer line.
func (m *Model) centerOverPopup(popup string, w, h int) string {
	bg := m.viewBoard()
	x := (m.width - w) / 2
	y := ((m.height - 1) - h) / 2
	if y < 0 {
		y = 0
	}
	return overlayAt(bg, popup, x, y)
}

// overlayAt composites fg on top of bg at position (x, y), measured in visual
// columns/rows. ANSI escape sequences in bg are preserved for unaffected
// regions; the fg block completely replaces the bg columns it covers.
func overlayAt(bg, fg string, x, y int) string {
	bgLines := strings.Split(bg, "\n")
	fgLines := strings.Split(fg, "\n")

	for i, fgLine := range fgLines {
		row := y + i
		if row < 0 || row >= len(bgLines) {
			continue
		}
		fgWidth := lipgloss.Width(fgLine)
		bgLine := bgLines[row]

		left := ansi.Truncate(bgLine, x, "")
		if w := lipgloss.Width(left); w < x {
			left += strings.Repeat(" ", x-w)
		}
		right := ansi.TruncateLeft(bgLine, x+fgWidth, "")

		bgLines[row] = left + fgLine + right
	}
	return strings.Join(bgLines, "\n")
}

func (m *Model) renderAddPopup(width, height int) string {
	// width is the outer popup width. The outer border eats 2 cols; we also
	// reserve 1 col of left pad + 1 col of right pad so inner panels sit
	// symmetrically inside the popup.
	innerWidth := width - 4
	if innerWidth < 10 {
		innerWidth = 10
	}

	status := model.ColumnOrder[m.focusedCol]
	accent := columnColor(status)

	meta := m.renderAddMeta()

	titleColor := softWhite
	if m.addFocusIdx == addFocusTitle {
		titleColor = accent
	}
	m.addTitle.Width = innerWidth - 2
	titlePanel := renderPanel("Title", m.addTitle.View(), innerWidth, 3, titleColor, m.addFocusIdx == addFocusTitle)

	descColor := softWhite
	if m.addFocusIdx == addFocusDesc {
		descColor = accent
	}
	// popup inner rows = height - 2 (popup borders). Fixed rows used:
	// 1 meta + 3 titlePanel + 1 help = 5. Description takes the remainder.
	descHeight := height - 7
	if descHeight < 5 {
		descHeight = 5
	}
	m.addDesc.SetWidth(innerWidth - 2)
	m.addDesc.SetHeight(descHeight - 2)
	descPanel := renderPanel("Description", m.addDesc.View(), innerWidth, descHeight, descColor, m.addFocusIdx == addFocusDesc)

	// lipgloss PaddingLeft on a multi-line block pads every line, so
	// sub-panel borders don't collide with the outer popup's left border.
	pad := lipgloss.NewStyle().PaddingLeft(1)
	lines := []string{
		pad.Render(meta),
		pad.Render(titlePanel),
		pad.Render(descPanel),
		pad.Render(m.addHelpLine()),
	}
	content := strings.Join(lines, "\n")

	return renderPanel("New ticket", content, width, height, accent, true)
}

// renderAddMeta mirrors the detail view's meta bar: focused fields get the
// reverse-highlight treatment via selectedFieldStyle. Unfocused empty slots
// show a dim placeholder so the user knows they can tab into them. We render
// static text even when a widget is focused (widget still captures keys
// invisibly) because stacking styles on top of textinput.View() mangles its
// internal cursor rendering.
func (m *Model) renderAddMeta() string {
	status := model.ColumnOrder[m.focusedCol]
	statusColor := columnColor(status)
	statusText := statusDisplay[status]

	dim := lipgloss.NewStyle().Foreground(midGray)

	assignVal := m.addAssign.Value()
	var assignRender string
	switch {
	case m.addFocusIdx == addFocusAssign:
		display := "+assign"
		if assignVal != "" {
			display = "● " + assignVal
		}
		assignRender = selectedFieldStyle.Render(display)
	case assignVal == "":
		assignRender = dim.Render("+assign")
	default:
		assignRender = assigneeStyle.Render("● " + assignVal)
	}

	tagsVal := m.addTags.Value()
	var tagsRender string
	switch {
	case m.addFocusIdx == addFocusTags:
		display := "+tag"
		if tagsVal != "" {
			display = "#" + tagsVal
		}
		tagsRender = selectedFieldStyle.Render(display)
	case tagsVal == "":
		tagsRender = dim.Render("+tag")
	default:
		tagsRender = tagStyle.Render("#" + tagsVal)
	}

	statusRender := lipgloss.NewStyle().Foreground(statusColor).Bold(true).Render(statusText)

	return strings.Join([]string{statusRender, assignRender, tagsRender}, "  ")
}

func (m *Model) addHelpLine() string {
	parts := []string{
		"tab/shift-tab: field",
		"enter (title): save",
	}
	if m.addFocusIdx == addFocusDesc && !m.addDescEditing {
		parts = append(parts, "enter: edit", "h/l: field")
	}
	if m.addFocusIdx == addFocusDesc && m.addDescEditing {
		parts = []string{"esc: exit edit"}
	}
	parts = append(parts, "esc: cancel")
	return helpStyle.Render(strings.Join(parts, "  •  "))
}

// ─── Board picker ───────────────────────────────────────────────────

func (m *Model) enterPicker() (tea.Model, tea.Cmd) {
	entries, err := loadPickerEntries()
	if err != nil {
		m.err = err
		return m, nil
	}
	m.pickerBoards = entries
	m.pickerWidth = pickerPopupWidth(entries)
	m.pickerIdx = 0
	for i, e := range entries {
		if e.name == m.sprintName {
			m.pickerIdx = i
			break
		}
	}
	m.view = pickerView
	return m, nil
}

// loadPickerEntries returns main first, then sprints alphabetically.
func loadPickerEntries() ([]pickerEntry, error) {
	mainStore := store.New("")
	mainBoard, err := mainStore.Load()
	if err != nil {
		return nil, err
	}
	entries := []pickerEntry{{name: "", counts: store.CountByStatus(mainBoard)}}

	sprints, err := store.ListSprints()
	if err != nil {
		return nil, err
	}
	for _, s := range sprints {
		entries = append(entries, pickerEntry{name: s.Name, counts: s.StatusCounts})
	}
	return entries, nil
}

func (m *Model) updatePicker(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch {
	case key.Matches(msg, keys.Quit):
		return m, tea.Quit
	case key.Matches(msg, keys.Esc), key.Matches(msg, keys.BoardPicker):
		m.view = boardView
	case key.Matches(msg, keys.Up):
		if m.pickerIdx > 0 {
			m.pickerIdx--
		}
	case key.Matches(msg, keys.Down):
		if m.pickerIdx < len(m.pickerBoards)-1 {
			m.pickerIdx++
		}
	case key.Matches(msg, keys.Enter):
		if m.pickerIdx < len(m.pickerBoards) {
			entry := m.pickerBoards[m.pickerIdx]
			if err := m.switchBoard(entry.name); err != nil {
				m.err = err
				return m, nil
			}
		}
		m.view = boardView
	}
	return m, nil
}

func (m *Model) switchBoard(sprintName string) error {
	var newStore *store.Store
	if sprintName == "" {
		newStore = store.New("")
	} else {
		s, err := store.NewSprint(sprintName)
		if err != nil {
			return err
		}
		newStore = s
	}

	board, err := newStore.Load()
	if err != nil {
		return err
	}

	m.store = newStore
	m.sprintName = sprintName
	m.board = board
	m.focusedCol = 1
	m.cursors = [5]int{}
	m.clampCursors()

	if info, err := os.Stat(newStore.BoardPath()); err == nil {
		m.lastModTime = info.ModTime()
	} else {
		m.lastModTime = time.Time{}
	}
	return nil
}

func (m *Model) viewPicker() string {
	rowCount := len(m.pickerBoards)
	if rowCount < 1 {
		rowCount = 1
	}
	popupHeight := rowCount + 2
	if popupHeight > m.height-4 {
		popupHeight = m.height - 4
	}
	if popupHeight < 6 {
		popupHeight = 6
	}

	popupWidth := m.pickerWidth
	if popupWidth > m.width-4 {
		popupWidth = m.width - 4
	}
	if popupWidth < 30 {
		popupWidth = 30
	}

	popup := m.renderPickerPopup(popupWidth, popupHeight)
	return m.centerOverPopup(popup, popupWidth, popupHeight)
}

// pickerPopupWidth sizes the popup to fit the widest row (name + counts).
func pickerPopupWidth(entries []pickerEntry) int {
	const (
		minWidth = 40
		maxWidth = 72
	)
	widest := 0
	for _, e := range entries {
		w := lipgloss.Width(boardDisplayName(e.name)) + 2 + lipgloss.Width(formatCounts(e.counts))
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

func (m *Model) renderPickerPopup(width, height int) string {
	innerWidth := width - 4
	if innerWidth < 10 {
		innerWidth = 10
	}

	var rows []string
	for i, e := range m.pickerBoards {
		rows = append(rows, renderPickerRow(e, innerWidth, i == m.pickerIdx, e.name == m.sprintName))
	}

	visible := height - 2
	if visible < 1 {
		visible = 1
	}
	if len(rows) > visible {
		start := m.pickerIdx - visible/2
		if start < 0 {
			start = 0
		}
		if start+visible > len(rows) {
			start = len(rows) - visible
		}
		rows = rows[start : start+visible]
	}

	content := lipgloss.NewStyle().PaddingLeft(1).Render(strings.Join(rows, "\n"))
	return renderPanel("Boards", content, width, height, green, true)
}

func renderPickerRow(e pickerEntry, width int, selected, current bool) string {
	marker := "  "
	if selected {
		marker = selectedMarker.Render("* ")
	}
	name := boardDisplayName(e.name)
	nameStyle := lipgloss.NewStyle()
	if current {
		nameStyle = nameStyle.Foreground(green).Bold(true)
	}
	counts := formatCounts(e.counts)

	// Fill the space between name and counts so counts right-align.
	left := marker + nameStyle.Render(name)
	leftWidth := lipgloss.Width(left)
	rightWidth := lipgloss.Width(counts)
	gap := width - leftWidth - rightWidth
	if gap < 1 {
		gap = 1
	}
	return left + strings.Repeat(" ", gap) + counts
}

// formatCounts renders the status-count line (e.g. "1B 3T 2Do 1Dn 1H").
// Zero-count statuses are dimmed; non-zero use the column accent color.
func formatCounts(counts map[model.Status]int) string {
	parts := make([]string, 0, len(model.ColumnOrder))
	for _, s := range model.ColumnOrder {
		n := counts[s]
		text := fmt.Sprintf("%d%s", n, statusShort[s])
		if n == 0 {
			parts = append(parts, dimStyle.Render(text))
		} else {
			parts = append(parts, statusCountStyles[s].Render(text))
		}
	}
	return strings.Join(parts, " ")
}

// ─── Help text ──────────────────────────────────────────────────────

func (m *Model) helpText() string {
	switch m.view {
	case boardView:
		return "h/l nav | j/k select | v layout | H/L move | a add | x archive | X browser | tab board | q quit"
	case pickerView:
		return "j/k select | enter switch | esc/tab close"
	case archiveView:
		return "j/k nav | u unarchive | X/esc back | q quit"
	case splitView:
		if m.splitFocus == 0 {
			return "j/k select | ] edit | + zoom | H/L move | x archive | X browser | - back | q quit"
		}
		if m.editTitle.Focused() || m.editDesc.Focused() {
			return "esc done editing"
		}
		switch m.editField {
		case 0:
			return "h/l meta | j/k fields | enter edit | H/L move | x archive | q quit"
		case 1, 2:
			return "j/k fields | enter/e edit | H/L move | h list | q quit"
		}
	case columnView:
		return "j/k select | H/L move | x archive | enter detail | - back | a add | q quit"
	case detailView:
		switch m.editField {
		case 0:
			return "h/l meta | j/k fields | enter edit | H/L move | d delete | x archive | - back | q quit"
		case 1:
			return "enter done | esc back"
		case 2:
			return "esc back"
		}
	}
	return ""
}

// viewTooSmall renders a placeholder when the terminal is below the usable
// minimum size. Shows current vs required dimensions so the user can resize.
func (m *Model) viewTooSmall() string {
	lines := []string{
		"Terminal too small",
		"",
		fmt.Sprintf("current:  %dx%d", m.width, m.height),
		fmt.Sprintf("required: %dx%d", minTerminalWidth, minTerminalHeight),
		"",
		"resize or press q to quit",
	}
	msg := strings.Join(lines, "\n")
	return lipgloss.Place(m.width, m.height,
		lipgloss.Center, lipgloss.Center,
		lipgloss.NewStyle().Foreground(softWhite).Render(msg))
}

// ─── Rendering ──────────────────────────────────────────────────────

// renderPanel draws a bordered panel with the title embedded in the top border (lazygit style).
func renderPanel(title string, content string, width, height int, borderColor lipgloss.Color, boldTitle bool) string {
	tl, tr, bl, br := "╭", "╮", "╰", "╯"
	h, v := "─", "│"

	innerWidth := width - 2
	if innerWidth < 1 {
		innerWidth = 1
	}

	borderStyle := lipgloss.NewStyle().Foreground(borderColor)

	titleStyle := lipgloss.NewStyle().Foreground(borderColor)
	if boldTitle {
		titleStyle = titleStyle.Bold(true)
	}
	renderedTitle := titleStyle.Render(title)

	titleLen := len([]rune(title))
	remaining := innerWidth - 1 - titleLen
	if remaining < 0 {
		remaining = 0
	}
	topBorder := borderStyle.Render(tl+h) + renderedTitle + borderStyle.Render(strings.Repeat(h, remaining)+tr)

	bottomBorder := borderStyle.Render(bl + strings.Repeat(h, innerWidth) + br)

	contentLines := strings.Split(content, "\n")
	var bodyLines []string
	innerHeight := height - 2
	if innerHeight < 0 {
		innerHeight = 0
	}
	for i := 0; i < innerHeight; i++ {
		line := ""
		if i < len(contentLines) {
			line = contentLines[i]
		}
		paddedLine := lipgloss.NewStyle().Inline(true).Width(innerWidth).MaxWidth(innerWidth).Render(line)
		bodyLines = append(bodyLines, borderStyle.Render(v)+paddedLine+borderStyle.Render(v))
	}

	result := topBorder + "\n"
	result += strings.Join(bodyLines, "\n") + "\n"
	result += bottomBorder

	return result
}

// viewBoard renders the board view (column layout by default, row layout on toggle).
func (m *Model) viewBoard() string {
	if m.rowLayout {
		return m.viewBoardRows()
	}
	availHeight := m.height - 1 // just help bar
	availWidth := m.width

	visCols := m.visibleColumns()
	numCols := len(visCols)

	colWidths := make([]int, numCols)
	if availWidth < 120 && numCols > 2 {
		focusedIdx := -1
		for i, c := range visCols {
			if c == m.focusedCol {
				focusedIdx = i
				break
			}
		}
		focusedWidth := availWidth * 50 / 100
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

	return lipgloss.JoinVertical(lipgloss.Left, board, m.footerLine())
}

// renderColumn renders a single column panel.
func (m *Model) renderColumn(colIdx int, status model.Status, width, height int, focused bool) string {
	tickets := m.board.ByStatus(status)
	title := fmt.Sprintf("[%d] %s", colIdx, statusDisplay[status])

	color := softWhite
	if focused {
		color = columnColor(status)
	}

	innerWidth := width - 2
	if innerWidth < 3 {
		innerWidth = 3
	}

	visibleCount := height - 2
	cursor := m.cursors[colIdx]

	// Only scroll to keep the cursor visible when the column is focused;
	// unfocused columns should always render from the top so switching away
	// doesn't leave a column scrolled mid-list.
	startIdx := 0
	if focused && cursor >= visibleCount {
		startIdx = cursor - visibleCount + 1
	}

	var lines []string
	for i := startIdx; i < len(tickets) && len(lines) < visibleCount; i++ {
		line := m.renderTicketLine(tickets[i], i == cursor && focused, innerWidth, color)
		lines = append(lines, line)
	}

	content := strings.Join(lines, "\n")
	return renderPanel(title, content, width, height, color, focused)
}

// renderTicketLine renders a single ticket in a column.
func (m *Model) renderTicketLine(t model.Ticket, selected bool, width int, accentColor lipgloss.Color) string {
	title := t.Title
	maxTitle := width - 1
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
		marker := lipgloss.NewStyle().Foreground(accentColor).Bold(true).Render(" * ")
		titleRendered := lipgloss.NewStyle().Bold(true).Foreground(white).Render(title)
		line := marker + titleRendered
		if t.AssignedTo != "" {
			line += " " + assigneeStyle.Render("●")
		}
		return line
	}

	return lipgloss.NewStyle().Foreground(softWhite).PaddingLeft(1).Render(title)
}

// viewBoardRows renders the board as stacked full-width rows — one per status.
// Tall terminals show all 5 rows; shorter ones show a 3-row sliding window
// centered on the focused row (same logic as the horizontal layout, applied to height).
func (m *Model) viewBoardRows() string {
	availHeight := m.height - 1
	availWidth := m.width

	visRows := m.visibleRows()
	numRows := len(visRows)

	rowHeights := make([]int, numRows)
	if availHeight < 24 && numRows > 2 {
		focusedIdx := -1
		for i, c := range visRows {
			if c == m.focusedCol {
				focusedIdx = i
				break
			}
		}
		focusedHeight := availHeight * 50 / 100
		remaining := availHeight - focusedHeight
		unfocusedHeight := remaining / (numRows - 1)
		for i := range rowHeights {
			if i == focusedIdx {
				rowHeights[i] = focusedHeight
			} else {
				rowHeights[i] = unfocusedHeight
			}
		}
	} else {
		baseHeight := availHeight / numRows
		for i := range rowHeights {
			rowHeights[i] = baseHeight
		}
	}
	total := 0
	for _, h := range rowHeights {
		total += h
	}
	rowHeights[numRows-1] += availHeight - total

	rows := make([]string, numRows)
	for i, colIdx := range visRows {
		status := model.ColumnOrder[colIdx]
		rows[i] = m.renderRow(colIdx, status, availWidth, rowHeights[i], colIdx == m.focusedCol)
	}
	board := lipgloss.JoinVertical(lipgloss.Left, rows...)
	return lipgloss.JoinVertical(lipgloss.Left, board, m.footerLine())
}

// renderRow draws one status as a full-width panel with its tickets as a
// vertical list (one ticket per line, same shape as renderColumn content).
func (m *Model) renderRow(colIdx int, status model.Status, width, height int, focused bool) string {
	tickets := m.board.ByStatus(status)
	title := fmt.Sprintf("[%d] %s (%d)", colIdx, statusDisplay[status], len(tickets))

	color := softWhite
	if focused {
		color = columnColor(status)
	}

	innerWidth := width - 2
	if innerWidth < 3 {
		innerWidth = 3
	}
	visibleCount := height - 2
	if visibleCount < 1 {
		visibleCount = 1
	}

	cursor := m.cursors[colIdx]
	startIdx := 0
	if focused && cursor >= visibleCount {
		startIdx = cursor - visibleCount + 1
	}

	var lines []string
	for i := startIdx; i < len(tickets) && len(lines) < visibleCount; i++ {
		line := m.renderTicketLine(tickets[i], i == cursor && focused, innerWidth, color)
		lines = append(lines, line)
	}

	content := strings.Join(lines, "\n")
	if content == "" {
		content = lipgloss.NewStyle().Foreground(subtle).Render("(empty)")
	}
	return renderPanel(title, content, width, height, color, focused)
}

// viewSplit renders the split view: list on left, detail on right.
func (m *Model) viewSplit() string {
	availHeight := m.height - 1
	availWidth := m.width

	// 35/65 split
	listWidth := availWidth * 35 / 100
	if listWidth < 20 {
		listWidth = 20
	}
	detailWidth := availWidth - listWidth

	status := model.ColumnOrder[m.focusedCol]
	color := columnColor(status)

	// Left panel: ticket list
	listFocused := m.splitFocus == 0
	listColor := color
	if !listFocused {
		listColor = softWhite
	}
	listPanel := m.renderSplitList(status, listWidth, availHeight, listFocused, listColor)

	// Right panel: ticket detail
	detailFocused := m.splitFocus == 1
	detailColor := color
	if !detailFocused {
		detailColor = softWhite
	}
	detailPanel := m.renderSplitDetail(detailWidth, availHeight, detailFocused, detailColor)

	body := lipgloss.JoinHorizontal(lipgloss.Top, listPanel, detailPanel)

	return lipgloss.JoinVertical(lipgloss.Left, body, m.footerLine())
}

func (m *Model) renderSplitList(status model.Status, width, height int, focused bool, borderColor lipgloss.Color) string {
	tickets := m.board.ByStatus(status)
	title := fmt.Sprintf("[%d] %s (%d)", m.focusedCol, statusDisplay[status], len(tickets))

	innerWidth := width - 2
	if innerWidth < 3 {
		innerWidth = 3
	}

	visibleCount := height - 2
	cursor := m.cursors[m.focusedCol]

	// Scroll window: ensure cursor is always visible
	startIdx := 0
	if cursor >= visibleCount {
		startIdx = cursor - visibleCount + 1
	}

	var lines []string
	for i := startIdx; i < len(tickets) && len(lines) < visibleCount; i++ {
		line := m.renderTicketLine(tickets[i], i == cursor, innerWidth, borderColor)
		lines = append(lines, line)
	}

	content := strings.Join(lines, "\n")
	return renderPanel(title, content, width, height, borderColor, focused)
}

func (m *Model) renderSplitDetail(width, height int, focused bool, borderColor lipgloss.Color) string {
	t := m.selectedTicket()
	if t == nil {
		return renderPanel("Detail", "No ticket selected", width, height, borderColor, focused)
	}

	innerWidth := width - 4 // account for panel borders
	if innerWidth < 1 {
		innerWidth = 1
	}

	// Metadata panel — height 3
	metaColor := softWhite
	if focused && m.editField == 0 {
		metaColor = borderColor
	}
	metaContent := m.renderCompactMeta(t, innerWidth, focused && m.editField == 0)
	metaPanel := renderPanel("Info", metaContent, width, 3, metaColor, focused && m.editField == 0)

	// Title panel — height 3
	titleColor := softWhite
	if focused && m.editField == 1 {
		titleColor = borderColor
	}
	var titleContent string
	if focused && m.editField == 1 && m.editTitle.Focused() {
		m.editTitle.Width = innerWidth
		titleContent = m.editTitle.View()
	} else {
		titleContent = lipgloss.NewStyle().Bold(true).Foreground(white).Render(t.Title)
	}
	titlePanel := renderPanel("Title", titleContent, width, 3, titleColor, focused && m.editField == 1)

	// Description panel — fills remaining space
	descPanelHeight := height - 6
	if descPanelHeight < 4 {
		descPanelHeight = 4
	}
	descColor := softWhite
	if focused && m.editField == 2 {
		descColor = borderColor
	}
	var descContent string
	if focused && m.editField == 2 && m.editDesc.Focused() {
		m.editDesc.SetWidth(innerWidth)
		m.editDesc.SetHeight(descPanelHeight - 2)
		descContent = m.editDesc.View()
	} else {
		desc := t.Description
		if desc == "" {
			descContent = lipgloss.NewStyle().Foreground(subtle).Render("(empty)")
		} else {
			// Pre-wrap to innerWidth, then style each line
			wrapped := lipgloss.NewStyle().Width(innerWidth).Render(desc)
			descContent = lipgloss.NewStyle().Foreground(softWhite).Render(wrapped)
		}
	}
	descPanel := renderPanel("Description", descContent, width, descPanelHeight, descColor, focused && m.editField == 2)

	return lipgloss.JoinVertical(lipgloss.Left, metaPanel, titlePanel, descPanel)
}

// viewColumn renders the expanded single-column view.
func (m *Model) viewColumn() string {
	status := model.ColumnOrder[m.focusedCol]
	tickets := m.board.ByStatus(status)
	availHeight := m.height - 1
	color := columnColor(status)

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
		tStyle := lipgloss.NewStyle()
		if i == cursor {
			marker = lipgloss.NewStyle().Foreground(color).Bold(true).Render(" * ")
			tStyle = tStyle.Bold(true).Foreground(white)
		} else {
			tStyle = tStyle.Faint(true)
		}

		// Build suffix first so we can truncate title to fit
		suffix := ""
		if len(t.Tags) > 0 {
			suffix += " #" + strings.Join(t.Tags, " #")
		}
		if t.AssignedTo != "" {
			suffix += " " + "● " + t.AssignedTo
		}

		maxTitle := innerWidth - 3 - len([]rune(suffix))
		if maxTitle < 3 {
			maxTitle = 3
		}
		if len([]rune(titleText)) > maxTitle {
			titleText = string([]rune(titleText)[:maxTitle-1]) + "…"
		}

		line := marker + tStyle.Render(titleText)
		if len(t.Tags) > 0 {
			line += tagStyle.Render(" #" + strings.Join(t.Tags, " #"))
		}
		if t.AssignedTo != "" {
			line += " " + assigneeStyle.Render("● "+t.AssignedTo)
		}

		lines = append(lines, line)

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
	panel := renderPanel(title, content, m.width, availHeight, color, true)

	return lipgloss.JoinVertical(lipgloss.Left, panel, m.footerLine())
}

// viewDetail renders the ticket detail view (full screen).
func (m *Model) viewDetail() string {
	t := m.selectedTicket()
	if t == nil {
		m.view = boardView
		return m.viewBoard()
	}

	status := model.ColumnOrder[m.focusedCol]
	color := columnColor(status)

	innerWidth := m.width - 4

	// Metadata bar
	metaBorderColor := softWhite
	if m.editField == 0 {
		metaBorderColor = color
	}
	metaContent := m.renderMetaBar(t)
	metaPanel := renderPanel("Info", metaContent, innerWidth+2, 3, metaBorderColor, m.editField == 0)

	// Title field
	titleBorderColor := softWhite
	if m.editField == 1 {
		titleBorderColor = color
	}
	m.editTitle.Width = innerWidth - 2
	titlePanel := renderPanel("Title", m.editTitle.View(), innerWidth+2, 3, titleBorderColor, m.editField == 1)

	// Description field
	descBorderColor := softWhite
	if m.editField == 2 {
		descBorderColor = color
	}
	descPanelHeight := m.height - 7
	if descPanelHeight < 4 {
		descPanelHeight = 4
	}
	m.editDesc.SetWidth(innerWidth - 2)
	m.editDesc.SetHeight(descPanelHeight - 2)
	descPanel := renderPanel("Description", m.editDesc.View(), innerWidth+2, descPanelHeight, descBorderColor, m.editField == 2)

	return lipgloss.JoinVertical(lipgloss.Left,
		metaPanel,
		titlePanel,
		descPanel,
		m.footerLine(),
	)
}

// renderCompactMeta renders a compact metadata bar that fits within a given width.
// When the Info panel is focused (navigable), empty assign/tag fields render as
// dim "+assign" / "+tag" prompts so the user can tab to them and create a value.
// When not focused, empty fields are hidden entirely to keep the bar uncluttered.
func (m *Model) renderCompactMeta(t *model.Ticket, maxWidth int, navigable bool) string {
	status := model.ColumnOrder[m.focusedCol]
	color := columnColor(status)

	statusText := statusDisplay[t.Status]

	assignText, assignEmpty := "+assign", true
	if t.AssignedTo != "" {
		assignText, assignEmpty = "● "+t.AssignedTo, false
	}
	tagsText, tagsEmpty := "+tag", true
	if len(t.Tags) > 0 {
		tagsText, tagsEmpty = "#"+strings.Join(t.Tags, " #"), false
	}

	dim := lipgloss.NewStyle().Foreground(midGray)

	fields := []struct {
		value string
		style lipgloss.Style
		empty bool
	}{
		{statusText, lipgloss.NewStyle().Foreground(color).Bold(true), false},
		{assignText, assigneeStyle, assignEmpty},
		{tagsText, tagStyle, tagsEmpty},
	}

	var parts []string
	for i, f := range fields {
		if f.empty && !navigable {
			continue
		}
		var rendered string
		switch {
		case navigable && i == m.metaIdx:
			rendered = selectedFieldStyle.Render(f.value)
		case f.empty:
			rendered = dim.Render(f.value)
		default:
			rendered = f.style.Render(f.value)
		}
		parts = append(parts, rendered)
	}
	parts = append(parts, dim.Render(t.ShortID))

	return strings.Join(parts, "  ")
}

// renderMetaBar renders the metadata fields with the selected one highlighted.
func (m *Model) renderMetaBar(t *model.Ticket) string {
	isMeta := m.editField == 0

	status := model.ColumnOrder[m.focusedCol]
	color := columnColor(status)

	statusText := statusDisplay[t.Status]

	assignText, assignEmpty := "+assign", true
	if t.AssignedTo != "" {
		assignText, assignEmpty = "● "+t.AssignedTo, false
	}
	tagsText, tagsEmpty := "+tag", true
	if len(t.Tags) > 0 {
		tagsText, tagsEmpty = "#"+strings.Join(t.Tags, " #"), false
	}

	dim := lipgloss.NewStyle().Foreground(midGray)

	fields := []struct {
		value string
		style lipgloss.Style
		empty bool
	}{
		{statusText, lipgloss.NewStyle().Foreground(color).Bold(true), false},
		{assignText, assigneeStyle, assignEmpty},
		{tagsText, tagStyle, tagsEmpty},
	}

	var parts []string
	for i, f := range fields {
		if f.empty && !isMeta {
			continue
		}
		var rendered string
		switch {
		case isMeta && i == m.metaIdx:
			rendered = selectedFieldStyle.Render(f.value)
		case f.empty:
			rendered = dim.Render(f.value)
		default:
			rendered = f.style.Render(f.value)
		}
		parts = append(parts, rendered)
	}

	parts = append(parts, lipgloss.NewStyle().Foreground(midGray).Render(t.ShortID))
	parts = append(parts, lipgloss.NewStyle().Foreground(midGray).Render(t.CreatedAt.Format("2006-01-02 15:04")))

	return strings.Join(parts, "  ")
}
