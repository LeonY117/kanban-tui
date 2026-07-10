package tui

import (
	"fmt"
	"os"
	"sort"
	"strings"
	"time"
	"unicode"

	"github.com/LeonY117/kanban-tui/internal/model"
	"github.com/LeonY117/kanban-tui/internal/store"
	"github.com/atotto/clipboard"
	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/runeutil"
	"github.com/charmbracelet/bubbles/textarea"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
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
	moveView             // floating move-ticket picker (column / other board)
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
	model.StatusWaiting: "Waiting",
	model.StatusDone:    "Done",
	model.StatusHold:    "Hold",
}

// statusShort is the compact label used in the board picker count strip.
var statusShort = map[model.Status]string{
	model.StatusBacklog: "B",
	model.StatusTodo:    "T",
	model.StatusDoing:   "Do",
	model.StatusWaiting: "W",
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

	// How much of each ticket a list row shows (cards by default).
	layout ticketLayout

	// First ticket rendered per column. Sticky: the cursor moves inside the
	// window and only pushes it from an edge.
	scrollStart [5]int

	// Read-only description scroll offset (in wrapped lines) and the largest
	// offset the last render could use. Editing hands scrolling to the textarea.
	descScroll    int
	descScrollMax int

	// Mouse hit-testing zones, rebuilt every render.
	zones []hitZone

	// Wheel notches banked toward the next ticket step — a trackpad emits
	// far more of them than there are tickets worth moving through.
	wheelAccum int

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
	addConfirmQuit bool // esc pressed with content in the popup — awaiting y/N

	// Board picker state
	pickerBoards       []pickerEntry
	pickerIdx          int
	pickerWidth        int
	pickerShowArchived bool   // when true, picker lists archived sprints below active ones
	confirmArchive     string // non-empty = mid-confirm prompt for that sprint name

	// Move popup state
	moveStage        moveStage
	moveRows         []moveRow
	moveIdx          int
	moveTicketID     string
	moveTicketStatus model.Status
	moveTargetBoard  string

	// Source view for the active popup or picker — restored on close, also
	// rendered as the backdrop behind the popup.
	popupReturnView viewMode

	// archived is true when this Model was launched on an archived sprint —
	// the TUI then refuses mutations and shows an archived tag in the footer.
	archived bool

	// notice is a transient one-shot message shown in the footer; cleared on
	// the next non-confirm key press.
	notice string

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
	name     string // "" for main
	counts   map[model.Status]int
	archived bool // sprints only; main is never archived
}

// prefixLabel renders a board's ticket-id prefix. The main board has none —
// its ids are bare numbers — which shows as "#".
func prefixLabel(prefix string) string {
	if prefix == "" {
		return "#"
	}
	return prefix
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

	archived := sprintName != "" && store.IsSprintArchived(sprintName)

	return &Model{
		store:       s,
		board:       board,
		sprintName:  sprintName,
		input:       ti,
		focusedCol:  1, // default to Todo
		lastModTime: modTime,
		archived:    archived,
	}, nil
}

// guardMutate returns true if the mutation should proceed. When the current
// sprint is archived, it sets a footer notice and returns false.
func (m *Model) guardMutate() bool {
	if m.archived {
		m.notice = fmt.Sprintf("sprint %q is archived — `kanban sprints unarchive %s` to edit", m.sprintName, m.sprintName)
		return false
	}
	return true
}

func (m *Model) footerLine() string {
	badge := sprintBadgeStyle.Render(boardDisplayName(m.sprintName))
	// A hint at what ids new tickets here will carry — not part of the
	// board's name, so it appears here and nowhere else.
	badge = lipgloss.JoinHorizontal(lipgloss.Top, badge,
		dimStyle.Render("["+prefixLabel(store.EffectivePrefix(m.board, m.sprintName))+"]"))
	if m.archived {
		archivedTag := lipgloss.NewStyle().Foreground(dimGray).Render("[archived]")
		badge = lipgloss.JoinHorizontal(lipgloss.Top, badge, archivedTag)
	}

	var rightText string
	if m.notice != "" {
		rightText = m.notice
	} else {
		rightText = m.helpText()
	}
	help := helpStyle.Render(rightText)
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

	case tea.MouseMsg:
		return m.updateMouse(msg)

	case tea.KeyMsg:
		// Transient notices clear on the next keypress so they show for
		// exactly one beat. The picker's confirm prompt sets its own notice
		// each render, so it's unaffected.
		m.notice = ""

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
		case moveView:
			return m.updateMove(msg)
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

	m.resetZones()
	content := m.renderView(m.view)

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
	case key.Matches(msg, keys.Five):
		m.focusedCol = 5
	case key.Matches(msg, keys.MoveLeft):
		m.moveTicket(-1)
	case key.Matches(msg, keys.MoveRight):
		m.moveTicket(1)
	case key.Matches(msg, keys.MoveUp):
		m.moveTicketInColumn(-1)
	case key.Matches(msg, keys.MoveDown):
		m.moveTicketInColumn(1)
	case key.Matches(msg, keys.Archive):
		m.archiveTicket()
	case key.Matches(msg, keys.Layout):
		m.cycleTicketLayout()
	case key.Matches(msg, keys.RowLayout):
		m.rowLayout = !m.rowLayout
	case key.Matches(msg, keys.Move):
		return m.enterMovePopup()
	case key.Matches(msg, keys.Copy):
		m.copyFocused()
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

// wrapDesc wraps a description the way the textarea does, so the read-only
// render and the editor agree line for line.
//
// Both lipgloss and ansi.Wordwrap break at hyphens, which the textarea never
// does — "daily-management-report" would split in one mode and jump whole to
// the next line in the other. This mirrors the textarea instead: whitespace is
// the only break, a word too long for the width is chopped, and runs of spaces
// are preserved so indented lists keep their shape.
func wrapDesc(text string, width int) string {
	if width < 1 {
		width = 1
	}
	var out []string
	for _, line := range strings.Split(sanitizeDesc(text), "\n") {
		out = append(out, wrapDescLine(line, width)...)
	}
	return strings.Join(out, "\n")
}

// sanitizeDesc runs the description through the same sanitizer the textarea
// applies on SetValue. Wrapping identically isn't enough on its own: the editor
// expands every tab to four spaces and drops control characters, so a
// description carrying either would still re-flow the moment editing started —
// the exact bug the wrap port exists to prevent. Descriptions arrive from the
// CLI and from agents, so neither is hypothetical.
func sanitizeDesc(text string) string {
	return string(runeutil.NewSanitizer().Sanitize([]rune(text)))
}

// wrapDescLine is a port of bubbles' textarea.wrap (MIT). Reimplementing it by
// eye kept leaving a handful of lines out of step wherever runs of spaces met a
// break, so the algorithm is mirrored outright: words accumulate with the
// spaces that follow them, and a break happens when the line plus the pending
// word plus those spaces would exceed the width.
func wrapDescLine(line string, width int) []string {
	var (
		lines  = [][]rune{{}}
		word   []rune
		row    int
		spaces int
	)
	runeWidth := func(r []rune) int { return lipgloss.Width(string(r)) }
	pad := func(n int) []rune { return []rune(strings.Repeat(" ", n)) }

	for _, r := range line {
		if unicode.IsSpace(r) {
			spaces++
		} else {
			word = append(word, r)
		}

		if spaces > 0 {
			if runeWidth(lines[row])+runeWidth(word)+spaces > width {
				row++
				lines = append(lines, []rune{})
			}
			lines[row] = append(lines[row], word...)
			lines[row] = append(lines[row], pad(spaces)...)
			spaces, word = 0, nil
			continue
		}

		// A word on its own too wide for the line moves down whole.
		lastCharLen := lipgloss.Width(string(word[len(word)-1]))
		if runeWidth(word)+lastCharLen > width {
			if len(lines[row]) > 0 {
				row++
				lines = append(lines, []rune{})
			}
			lines[row] = append(lines[row], word...)
			word = nil
		}
	}
	if runeWidth(lines[row])+runeWidth(word)+spaces >= width {
		lines = append(lines, []rune{})
		row++
	}
	lines[row] = append(lines[row], word...)

	// The textarea's viewport chops whatever still overflows; trailing spaces
	// are invisible against the panel's own padding, so they're dropped here.
	var out []string
	for _, l := range lines {
		s := strings.TrimRight(string(l), " ")
		for lipgloss.Width(s) > width {
			cut := ansi.Truncate(s, width, "")
			out = append(out, cut)
			s = strings.TrimPrefix(s, cut)
		}
		out = append(out, s)
	}
	return out
}

// setDescWidth sizes a description textarea to the same column count
// renderDescBody wraps to, so entering edit mode doesn't re-flow the text
// under the cursor. wrapDesc mirrors the textarea's own rules from there.
func setDescWidth(ta *textarea.Model, wrapWidth int) {
	if wrapWidth < 1 {
		wrapWidth = 1
	}
	ta.SetWidth(wrapWidth)
}

// newTitleInput builds a blurred title input seeded with value.
func newTitleInput(value string) textinput.Model {
	ti := textinput.New()
	ti.Prompt = ""
	ti.CharLimit = 200
	ti.SetValue(value)
	ti.Blur()
	return ti
}

// newDescArea builds a blurred description textarea seeded with value. Enter
// is reserved for confirm/save, so newlines move to shift+enter (keys.NewLine).
func newDescArea(value string) textarea.Model {
	ta := textarea.New()
	ta.Prompt = ""
	ta.SetValue(value)
	ta.ShowLineNumbers = false
	ta.FocusedStyle.CursorLine = lipgloss.NewStyle()
	ta.FocusedStyle.Base = lipgloss.NewStyle()
	ta.KeyMap.InsertNewline = keys.NewLine
	ta.Blur()
	return ta
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
	m.descScroll = 0

	m.editTitle = newTitleInput(t.Title)
	m.editDesc = newDescArea(t.Description)
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
	case key.Matches(msg, keys.BoardPicker):
		return m.enterPicker()
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
	case key.Matches(msg, keys.Five):
		m.focusedCol = 5
		m.refreshDetailEditors()
	case key.Matches(msg, keys.MoveLeft):
		m.moveTicket(-1)
		m.refreshDetailEditors()
	case key.Matches(msg, keys.MoveRight):
		m.moveTicket(1)
		m.refreshDetailEditors()
	case key.Matches(msg, keys.MoveUp):
		m.moveTicketInColumn(-1)
	case key.Matches(msg, keys.MoveDown):
		m.moveTicketInColumn(1)
	case key.Matches(msg, keys.Archive):
		m.archiveTicket()
		m.refreshDetailEditors()
	case key.Matches(msg, keys.Move):
		return m.enterMovePopup()
	case key.Matches(msg, keys.Copy):
		m.copyFocused()
	case key.Matches(msg, keys.Layout):
		m.cycleTicketLayout()
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
	case key.Matches(msg, keys.Add):
		return m.enterAddPopup()
	case key.Matches(msg, keys.BoardPicker):
		return m.enterPicker()
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
	case key.Matches(msg, keys.Move):
		return m.enterMovePopup()
	case key.Matches(msg, keys.Copy):
		m.copyFocused()
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
	case key.Matches(msg, keys.Five):
		m.jumpDetailCol(5)
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
	case key.Matches(msg, keys.Add):
		return m.enterAddPopup()
	case key.Matches(msg, keys.BoardPicker):
		return m.enterPicker()
	case key.Matches(msg, keys.Up):
		m.editField = 0
	case key.Matches(msg, keys.Down):
		m.editField = 2
	case key.Matches(msg, keys.Left):
		m.splitFocus = 0
	case key.Matches(msg, keys.Enter), key.Matches(msg, keys.Edit):
		if !m.guardMutate() {
			return m, nil
		}
		m.editTitle.Focus()
		return m, textinput.Blink
	case key.Matches(msg, keys.Copy):
		m.copyFocused()
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
	case key.Matches(msg, keys.Five):
		m.jumpDetailCol(5)
	}
	return m, nil
}

func (m *Model) updateSplitDetailDesc(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if m.editDesc.Focused() {
		// Editing mode: enter saves and drops back out, shift+enter (see
		// keys.NewLine) is the newline — the textarea's own binding.
		if key.Matches(msg, keys.Enter) || key.Matches(msg, keys.Esc) {
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
	case key.Matches(msg, keys.Add):
		return m.enterAddPopup()
	case key.Matches(msg, keys.BoardPicker):
		return m.enterPicker()
	case key.Matches(msg, keys.Up):
		m.editField = 1
	case key.Matches(msg, keys.Left):
		m.splitFocus = 0
	case key.Matches(msg, keys.Enter), key.Matches(msg, keys.Edit):
		if !m.guardMutate() {
			return m, nil
		}
		m.editDesc.Focus()
		return m, nil
	case key.Matches(msg, keys.Copy):
		m.copyFocused()
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
	case key.Matches(msg, keys.Five):
		m.jumpDetailCol(5)
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
	case key.Matches(msg, keys.Five):
		m.focusedCol = 5
	case key.Matches(msg, keys.MoveLeft):
		m.moveTicket(-1)
	case key.Matches(msg, keys.MoveRight):
		m.moveTicket(1)
	case key.Matches(msg, keys.MoveUp):
		m.moveTicketInColumn(-1)
	case key.Matches(msg, keys.MoveDown):
		m.moveTicketInColumn(1)
	case key.Matches(msg, keys.Archive):
		m.archiveTicket()
	case key.Matches(msg, keys.Move):
		return m.enterMovePopup()
	case key.Matches(msg, keys.Copy):
		m.copyFocused()
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
	m.descScroll = 0

	m.editTitle = newTitleInput(t.Title)
	m.editDesc = newDescArea(t.Description)

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
	case key.Matches(msg, keys.Add):
		return m.enterAddPopup()
	case key.Matches(msg, keys.BoardPicker):
		return m.enterPicker()
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
	case key.Matches(msg, keys.Down):
		m.editField = 1
	case key.Matches(msg, keys.Enter):
		return m.editMetaField()
	case key.Matches(msg, keys.Move):
		return m.enterMovePopup()
	case key.Matches(msg, keys.Copy):
		m.copyFocused()
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
	case key.Matches(msg, keys.Five):
		m.jumpDetailCol(5)
	}
	return m, nil
}

func (m *Model) editMetaField() (tea.Model, tea.Cmd) {
	if !m.guardMutate() {
		return m, nil
	}
	switch m.metaIdx {
	case 0: // status
		m.startSelect("Status", []string{"Todo", "Doing", "Waiting", "Done", "Hold"}, func(val string) {
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
	if m.editTitle.Focused() {
		switch msg.String() {
		case "esc", "enter":
			m.editTitle.Blur()
			m.saveEdit()
			return m, nil
		case "tab":
			m.editTitle.Blur()
			m.saveEdit()
			return m.enterPicker()
		}
		var cmd tea.Cmd
		m.editTitle, cmd = m.editTitle.Update(msg)
		return m, cmd
	}

	switch {
	case key.Matches(msg, keys.Quit):
		return m, tea.Quit
	case key.Matches(msg, keys.Up):
		m.editField = 0
	case key.Matches(msg, keys.Down):
		m.editField = 2
	case key.Matches(msg, keys.Enter), key.Matches(msg, keys.Edit):
		if !m.guardMutate() {
			return m, nil
		}
		m.editTitle.Focus()
		return m, textinput.Blink
	case key.Matches(msg, keys.Copy):
		m.copyFocused()
	case key.Matches(msg, keys.Esc), key.Matches(msg, keys.Unzoom):
		m.editField = 0
		m.enterSplit()
	case key.Matches(msg, keys.BoardPicker):
		return m.enterPicker()
	}
	return m, nil
}

func (m *Model) updateDetailDesc(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if m.editDesc.Focused() {
		if key.Matches(msg, keys.Enter) || key.Matches(msg, keys.Esc) {
			m.editDesc.Blur()
			m.saveEdit()
			return m, nil
		}
		if msg.String() == "tab" {
			m.editDesc.Blur()
			m.saveEdit()
			return m.enterPicker()
		}
		var cmd tea.Cmd
		m.editDesc, cmd = m.editDesc.Update(msg)
		return m, cmd
	}

	switch {
	case key.Matches(msg, keys.Quit):
		return m, tea.Quit
	case key.Matches(msg, keys.Up):
		m.editField = 1
	case key.Matches(msg, keys.Enter), key.Matches(msg, keys.Edit):
		if !m.guardMutate() {
			return m, nil
		}
		m.editDesc.Focus()
		return m, textarea.Blink
	case key.Matches(msg, keys.Copy):
		m.copyFocused()
	case key.Matches(msg, keys.Esc), key.Matches(msg, keys.Unzoom):
		m.editField = 0
		m.enterSplit()
	case key.Matches(msg, keys.BoardPicker):
		return m.enterPicker()
	}
	return m, nil
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
	if !m.guardMutate() {
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
	if !m.guardMutate() {
		return
	}
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
	if !m.guardMutate() {
		return
	}
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

func (m *Model) moveTicketInColumn(dir int) {
	if !m.guardMutate() {
		return
	}
	t := m.selectedTicket()
	if t == nil {
		return
	}
	colTickets := m.board.ByStatus(model.ColumnOrder[m.focusedCol])
	cursor := m.cursors[m.focusedCol]
	newCursor := cursor + dir
	if newCursor < 0 || newCursor >= len(colTickets) {
		return
	}
	neighbourID := colTickets[newCursor].ID

	m.store.WithLock(func() error {
		board, err := m.store.Load()
		if err != nil {
			return err
		}
		_, i := board.FindByID(t.ID)
		_, j := board.FindByID(neighbourID)
		if i < 0 || j < 0 {
			return nil
		}
		board.Tickets[i], board.Tickets[j] = board.Tickets[j], board.Tickets[i]
		return m.store.Save(board)
	})
	m.cursors[m.focusedCol] = newCursor
	m.reload()
	m.clampCursors()
}

// copyKind is what `c` puts on the clipboard, chosen by what's focused.
type copyKind int

const (
	copyID copyKind = iota
	copyTitle
	copyDescription
)

func (k copyKind) label() string {
	switch k {
	case copyTitle:
		return "title"
	case copyDescription:
		return "description"
	default:
		return "ticket id"
	}
}

// copyFocused copies whatever the cursor is on: the ticket id from a list or
// the info bar, the title or description when that field is focused.
func (m *Model) copyFocused() {
	t := m.selectedTicket()
	if t == nil {
		return
	}

	kind := copyID
	// Field focus only counts in the detail pane — from the rail, `c` always
	// means the id.
	inDetail := m.view == detailView || (m.view == splitView && m.splitFocus == 1)
	if inDetail {
		switch m.editField {
		case 1:
			kind = copyTitle
		case 2:
			kind = copyDescription
		}
	}

	var value string
	switch kind {
	case copyTitle:
		value = t.Title
	case copyDescription:
		value = t.Description
	default:
		value = t.ShortID
	}
	m.copyToClipboard(value, kind)
}

// copyToClipboard writes to the system clipboard and reports what happened in
// the footer either way — a copy you can't see is a copy you don't trust.
func (m *Model) copyToClipboard(value string, kind copyKind) {
	if strings.TrimSpace(value) == "" {
		m.notice = fmt.Sprintf("nothing to copy — %s is empty", kind.label())
		return
	}
	if err := clipboard.WriteAll(value); err != nil {
		m.notice = "clipboard failed: " + err.Error()
		return
	}
	m.notice = fmt.Sprintf("copied %s to clipboard", kind.label())
}

func (m *Model) archiveTicket() {
	if !m.guardMutate() {
		return
	}
	t := m.selectedTicket()
	if t == nil {
		return
	}
	m.store.ArchiveByID(t.ID)
	m.reload()
	m.clampCursors()
}

func (m *Model) deleteTicket() {
	if !m.guardMutate() {
		return
	}
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
		m.descScroll = 0
	case key.Matches(msg, keys.Down):
		m.moveArchiveCursor(1)
		m.descScroll = 0
	case key.Matches(msg, keys.Unarchive):
		m.unarchiveSelected()
	case key.Matches(msg, keys.Copy):
		if t := m.archiveSelected(); t != nil {
			m.copyToClipboard(t.ShortID, copyID)
		}
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
	if !m.guardMutate() {
		return
	}
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
	m.addZone(hitZone{kind: zoneArchiveDetail, x: listWidth, y: 0, w: detailWidth, h: availHeight})
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
		if !e.isHeader {
			m.addZone(hitZone{kind: zoneArchiveRow, x: 1, y: 1 + len(lines), w: innerWidth, h: 1, idx: i})
		}
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
	descContent := m.renderDescBody(t.Description, innerWidth, descPanelHeight-2)
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
	if !m.guardMutate() {
		return m, nil
	}
	ti := newTitleInput("")
	ti.Focus()
	m.addTitle = ti
	m.addDesc = newDescArea("")

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
	m.addConfirmQuit = false
	m.popupReturnView = m.view
	m.view = addView
	return m, textinput.Blink
}

// restorePopupView returns to the view that opened the active popup. self
// guards against falling back into the popup itself (e.g. on re-entry).
func (m *Model) restorePopupView(self viewMode) {
	target := m.popupReturnView
	if target == self {
		target = boardView
	}
	m.view = target
}

// closeAddPopup also refreshes the detail editors — submitAdd may have moved
// the cursor to the newly created ticket.
func (m *Model) closeAddPopup() {
	m.restorePopupView(addView)
	if m.view == splitView || m.view == detailView {
		m.refreshDetailEditors()
	}
}

func (m *Model) updateAdd(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if m.addConfirmQuit {
		switch msg.String() {
		case "y", "Y":
			m.addConfirmQuit = false
			m.closeAddPopup()
		case "n", "N", "esc":
			m.addConfirmQuit = false
		}
		return m, nil
	}

	if m.addFocusIdx == addFocusDesc && m.addDescEditing {
		// enter submits the whole ticket; shift+enter (keys.NewLine) is the
		// newline, bound on the textarea itself.
		if key.Matches(msg, keys.Enter) {
			m.submitAdd()
			return m, nil
		}
		if key.Matches(msg, keys.Esc) {
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
		if m.addHasContent() {
			m.addConfirmQuit = true
		} else {
			m.closeAddPopup()
		}
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

// addHasContent reports whether the new-ticket popup holds anything worth
// confirming before it's thrown away.
func (m *Model) addHasContent() bool {
	return strings.TrimSpace(m.addTitle.Value()) != "" ||
		strings.TrimSpace(m.addDesc.Value()) != "" ||
		strings.TrimSpace(m.addTags.Value()) != "" ||
		strings.TrimSpace(m.addAssign.Value()) != ""
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
		m.notice = "a title is required"
		return
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
	m.closeAddPopup()
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

	backdrop := m.popupBackdrop(m.popupReturnView)
	// The backdrop's zones belong to a view the user can't reach right now.
	m.resetZones()
	popup := m.renderAddPopup(popupWidth, popupHeight)
	return m.centerOverPopup(popup, backdrop, popupWidth, popupHeight)
}

// centerOverPopup overlays a popup on top of bg, centered. Vertical center
// uses height-1 so the popup sits centered on the board, not pushed down by
// the footer line.
func (m *Model) centerOverPopup(popup, bg string, w, h int) string {
	o := m.popupOrigin(w, h)
	return overlayAt(bg, popup, o.x, o.y)
}

// popupOrigin is the top-left cell a centered popup of this size occupies.
func (m *Model) popupOrigin(w, h int) point {
	x := (m.width - w) / 2
	if x < 0 {
		x = 0
	}
	y := ((m.height - 1) - h) / 2
	if y < 0 {
		y = 0
	}
	return point{x: x, y: y}
}

// renderView dispatches a viewMode to its render function. Falls back to the
// board view for unset / unknown values.
func (m *Model) renderView(v viewMode) string {
	switch v {
	case splitView:
		return m.viewSplit()
	case columnView:
		return m.viewColumn()
	case detailView:
		return m.viewDetail()
	case archiveView:
		return m.viewArchive()
	case addView:
		return m.viewAdd()
	case pickerView:
		return m.viewPicker()
	case moveView:
		return m.viewMove()
	default:
		return m.viewBoard()
	}
}

// popupBackdrop renders the source view as the backdrop behind a popup, but
// avoids recursing into popup views themselves.
func (m *Model) popupBackdrop(source viewMode) string {
	if source == addView || source == pickerView || source == moveView {
		return m.viewBoard()
	}
	return m.renderView(source)
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
	setDescWidth(&m.addDesc, innerWidth-2)
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
	if m.addConfirmQuit {
		return lipgloss.NewStyle().Foreground(peach).Bold(true).
			Render("discard this ticket? [y/N]")
	}
	if m.notice != "" {
		return lipgloss.NewStyle().Foreground(peach).Render(m.notice)
	}

	parts := []string{
		"tab/shift-tab: field",
		"enter: save",
	}
	if m.addFocusIdx == addFocusDesc && !m.addDescEditing {
		parts = append(parts, "enter: edit", "h/l: field")
	}
	if m.addFocusIdx == addFocusDesc && m.addDescEditing {
		parts = []string{"enter: save", "shift+enter: new line", "esc: exit edit"}
	}
	parts = append(parts, "esc: cancel")
	return helpStyle.Render(strings.Join(parts, "  •  "))
}

// ─── Board picker ───────────────────────────────────────────────────

func (m *Model) enterPicker() (tea.Model, tea.Cmd) {
	// Each open is a clean default view — show-archived and confirm state
	// are session-scoped to one popup, not persistent.
	m.pickerShowArchived = false
	m.confirmArchive = ""
	if !m.loadPickerData() {
		return m, nil
	}
	m.pickerIdx = 0
	for i, e := range m.pickerBoards {
		if e.name == m.sprintName {
			m.pickerIdx = i
			break
		}
	}
	m.popupReturnView = m.view
	m.view = pickerView
	return m, nil
}

// loadPickerData (re)populates the picker board list and width from disk.
// Returns false on error (m.err is set). Callers handle cursor placement.
func (m *Model) loadPickerData() bool {
	entries, err := loadPickerEntries(m.pickerShowArchived)
	if err != nil {
		m.err = err
		return false
	}
	m.pickerBoards = entries
	m.pickerWidth = pickerPopupWidth(entries)
	return true
}

// reloadPickerEntries refreshes after archive/unarchive and clamps the cursor
// (the active row count may shrink).
func (m *Model) reloadPickerEntries() {
	if !m.loadPickerData() {
		return
	}
	if m.pickerIdx >= len(m.pickerBoards) {
		m.pickerIdx = len(m.pickerBoards) - 1
	}
	if m.pickerIdx < 0 {
		m.pickerIdx = 0
	}
}

// loadPickerEntries returns main first (always sticky), then active sprints by
// most recently edited; if includeArchived, archived sprints follow at the
// bottom in the same order.
func loadPickerEntries(includeArchived bool) ([]pickerEntry, error) {
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
		if s.Archived && !includeArchived {
			continue
		}
		entries = append(entries, pickerEntry{name: s.Name, counts: s.StatusCounts, archived: s.Archived})
	}
	return entries, nil
}

func (m *Model) updatePicker(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if m.confirmArchive != "" {
		return m.updatePickerConfirm(msg)
	}
	switch {
	case key.Matches(msg, keys.Quit):
		return m, tea.Quit
	case key.Matches(msg, keys.Esc), key.Matches(msg, keys.BoardPicker):
		m.restorePopupView(pickerView)
	case key.Matches(msg, keys.Up):
		m.movePickerCursor(-1)
	case key.Matches(msg, keys.Down):
		m.movePickerCursor(1)
	case key.Matches(msg, keys.Enter):
		return m.pickerActivate()
	case key.Matches(msg, keys.Archive):
		return m.startPickerArchive()
	case key.Matches(msg, keys.ArchiveView):
		m.pickerShowArchived = !m.pickerShowArchived
		m.reloadPickerEntries()
	case key.Matches(msg, keys.Unarchive):
		return m.pickerUnarchive()
	}
	return m, nil
}

func (m *Model) movePickerCursor(dir int) {
	next := m.pickerIdx + dir
	if next < 0 || next >= len(m.pickerBoards) {
		return
	}
	m.pickerIdx = next
}

// pickerActivate switches to the highlighted board and closes the picker.
func (m *Model) pickerActivate() (tea.Model, tea.Cmd) {
	if m.pickerIdx < len(m.pickerBoards) {
		entry := m.pickerBoards[m.pickerIdx]
		if err := m.switchBoard(entry.name); err != nil {
			m.err = err
			return m, nil
		}
	}
	m.view = boardView
	return m, nil
}

// startPickerArchive enters the confirm-archive prompt for the highlighted
// sprint. Refuses on the main board (always shown but never archivable) and
// on already-archived sprints.
func (m *Model) startPickerArchive() (tea.Model, tea.Cmd) {
	if m.pickerIdx < 0 || m.pickerIdx >= len(m.pickerBoards) {
		return m, nil
	}
	e := m.pickerBoards[m.pickerIdx]
	if e.name == "" {
		m.notice = "main board can't be archived"
		return m, nil
	}
	if e.archived {
		m.notice = "already archived — press u to unarchive"
		return m, nil
	}
	m.confirmArchive = e.name
	return m, nil
}

func (m *Model) updatePickerConfirm(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "y", "Y":
		name := m.confirmArchive
		m.confirmArchive = ""
		if err := store.ArchiveSprint(name); err != nil {
			m.err = err
			m.notice = err.Error()
			return m, nil
		}
		// Archiving the currently-viewed sprint switches to main so the
		// model isn't pinned to an archived (read-only) board.
		if name == m.sprintName {
			if err := m.switchBoard(""); err != nil {
				m.err = err
				return m, nil
			}
		}
		m.reloadPickerEntries()
		m.notice = fmt.Sprintf("archived %q", name)
		return m, nil
	case "n", "N", "esc":
		m.confirmArchive = ""
		return m, nil
	}
	return m, nil
}

// pickerUnarchive unarchives the highlighted sprint, no confirm needed.
func (m *Model) pickerUnarchive() (tea.Model, tea.Cmd) {
	if m.pickerIdx < 0 || m.pickerIdx >= len(m.pickerBoards) {
		return m, nil
	}
	e := m.pickerBoards[m.pickerIdx]
	if e.name == "" {
		return m, nil
	}
	if !e.archived {
		m.notice = "sprint isn't archived"
		return m, nil
	}
	if err := store.UnarchiveSprint(e.name); err != nil {
		m.err = err
		m.notice = err.Error()
		return m, nil
	}
	m.reloadPickerEntries()
	m.notice = fmt.Sprintf("unarchived %q", e.name)
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
	m.archived = sprintName != "" && store.IsSprintArchived(sprintName)
	m.board = board
	m.focusedCol = 1
	m.cursors = [5]int{}
	m.scrollStart = [5]int{}
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
	if m.confirmArchive != "" {
		rowCount += 2 // blank + confirm prompt
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

	backdrop := m.popupBackdrop(m.popupReturnView)
	m.resetZones()
	origin := m.popupOrigin(popupWidth, popupHeight)
	popup := m.renderPickerPopup(popupWidth, popupHeight, origin)
	return overlayAt(backdrop, popup, origin.x, origin.y)
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

func (m *Model) renderPickerPopup(width, height int, origin point) string {
	innerWidth := width - 4
	if innerWidth < 10 {
		innerWidth = 10
	}

	var rows []string
	for i, e := range m.pickerBoards {
		rows = append(rows, renderPickerRow(e, innerWidth, i == m.pickerIdx, e.name == m.sprintName))
	}
	rowOffset := 0
	if m.confirmArchive != "" {
		prompt := fmt.Sprintf("archive %q? [y/N]", m.confirmArchive)
		rows = append(rows, "", lipgloss.NewStyle().Foreground(peach).Bold(true).Render(prompt))
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
		rowOffset = start
	}

	// Board rows are clickable; the confirm prompt rows (appended after them)
	// are not, and fall outside the loop below.
	for i := range rows {
		idx := rowOffset + i
		if idx >= len(m.pickerBoards) {
			break
		}
		m.addZone(hitZone{kind: zonePickerRow, x: origin.x + 2, y: origin.y + 1 + i, w: innerWidth, h: 1, idx: idx})
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
	switch {
	case e.archived:
		nameStyle = nameStyle.Foreground(dimGray)
	case current:
		nameStyle = nameStyle.Foreground(green).Bold(true)
	}
	counts := formatCounts(e.counts)
	if e.archived {
		counts = dimStyle.Render(ansi.Strip(counts))
	}

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
		return "h/l nav | j/k select | v size | H/L move | m move | c copy id | a add | x archive | q quit"
	case moveView:
		switch m.moveStage {
		case moveStageColumn:
			return "j/k select | enter move | esc cancel"
		default:
			return "j/k select | enter move | esc back"
		}
	case pickerView:
		if m.confirmArchive != "" {
			return fmt.Sprintf("archive %q? y / n", m.confirmArchive)
		}
		if m.pickerShowArchived {
			return "j/k select | enter switch | x archive | u unarchive | X hide archived | esc/tab close"
		}
		return "j/k select | enter switch | x archive | X show archived | esc/tab close"
	case archiveView:
		return "j/k nav | u unarchive | c copy id | X/esc back | q quit"
	case splitView:
		if m.splitFocus == 0 {
			return "j/k select | ] edit | + zoom | H/L move | m move | c copy id | x archive | - back | q quit"
		}
		if m.editDesc.Focused() {
			return "enter save | shift+enter new line | esc save"
		}
		if m.editTitle.Focused() {
			return "enter save | esc save"
		}
		switch m.editField {
		case 0:
			return "h/l meta | j/k fields | enter edit | H/L move | m move to | x archive | q quit"
		case 1, 2:
			return "j/k fields | enter/e edit | H/L move | h list | q quit"
		}
	case columnView:
		return "j/k select | H/L move | m move to | x archive | enter detail | - back | a add | q quit"
	case detailView:
		if m.editDesc.Focused() {
			return "enter save | shift+enter new line | esc save"
		}
		if m.editTitle.Focused() {
			return "enter save | esc save"
		}
		switch m.editField {
		case 0:
			return "h/l meta | j/k fields | enter edit | H/L move | m move to | d delete | - back | q quit"
		case 1, 2:
			return "j/k fields | enter/e edit | esc back | q quit"
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
	// A title longer than the panel used to push the top border past the
	// requested width. Mouse zones are registered at the width we asked for, so
	// every column right of an overlong title sat one cell off from where it
	// was drawn, and clicks near the boundary hit the wrong column.
	maxTitle := innerWidth - 1
	if maxTitle < 0 {
		maxTitle = 0
	}
	if lipgloss.Width(title) > maxTitle {
		title = ansi.Truncate(title, maxTitle, "…")
	}
	renderedTitle := titleStyle.Render(title)

	remaining := innerWidth - 1 - lipgloss.Width(title)
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
	x := 0
	for i, colIdx := range visCols {
		status := model.ColumnOrder[colIdx]
		columns[i] = m.renderColumn(colIdx, status, colWidths[i], availHeight, colIdx == m.focusedCol, point{x: x, y: 0})
		x += colWidths[i]
	}

	board := lipgloss.JoinHorizontal(lipgloss.Top, columns...)

	return lipgloss.JoinVertical(lipgloss.Left, board, m.footerLine())
}

// renderColumn renders a single column panel. origin is the panel's top-left
// cell on screen, used to register mouse zones.
func (m *Model) renderColumn(colIdx int, status model.Status, width, height int, focused bool, origin point) string {
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

	// Only the focused column has a selection. Passing cursor -1 keeps an
	// unfocused column at its remembered scroll position without highlighting
	// a ticket.
	cursor := -1
	if focused {
		cursor = m.cursors[colIdx]
	}

	// Registered before the ticket zones so a click on a ticket wins over the
	// surrounding column.
	m.addZone(hitZone{kind: zoneColumn, x: origin.x, y: origin.y, w: width, h: height, col: colIdx})

	content := m.renderTicketList(tickets, colIdx, innerWidth, visibleCount, cursor, color,
		point{x: origin.x + 1, y: origin.y + 1})
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
	y := 0
	for i, colIdx := range visRows {
		status := model.ColumnOrder[colIdx]
		rows[i] = m.renderRow(colIdx, status, availWidth, rowHeights[i], colIdx == m.focusedCol, point{x: 0, y: y})
		y += rowHeights[i]
	}
	board := lipgloss.JoinVertical(lipgloss.Left, rows...)
	return lipgloss.JoinVertical(lipgloss.Left, board, m.footerLine())
}

// renderRow draws one status as a full-width panel with its tickets as a
// vertical list (one ticket per line, same shape as renderColumn content).
func (m *Model) renderRow(colIdx int, status model.Status, width, height int, focused bool, origin point) string {
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

	cursor := -1
	if focused {
		cursor = m.cursors[colIdx]
	}

	m.addZone(hitZone{kind: zoneColumn, x: origin.x, y: origin.y, w: width, h: height, col: colIdx})

	content := m.renderTicketList(tickets, colIdx, innerWidth, visibleCount, cursor, color,
		point{x: origin.x + 1, y: origin.y + 1})
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
	listPanel := m.renderSplitList(status, listWidth, availHeight, listFocused, listColor, point{x: 0, y: 0})

	// Right panel: ticket detail
	detailFocused := m.splitFocus == 1
	detailColor := color
	if !detailFocused {
		detailColor = softWhite
	}
	detailPanel := m.renderSplitDetail(detailWidth, availHeight, detailFocused, detailColor, point{x: listWidth, y: 0})

	body := lipgloss.JoinHorizontal(lipgloss.Top, listPanel, detailPanel)

	return lipgloss.JoinVertical(lipgloss.Left, body, m.footerLine())
}

func (m *Model) renderSplitList(status model.Status, width, height int, focused bool, borderColor lipgloss.Color, origin point) string {
	tickets := m.board.ByStatus(status)
	title := fmt.Sprintf("[%d] %s (%d)", m.focusedCol, statusDisplay[status], len(tickets))

	innerWidth := width - 2
	if innerWidth < 3 {
		innerWidth = 3
	}

	visibleCount := height - 2

	m.addZone(hitZone{kind: zoneColumn, x: origin.x, y: origin.y, w: width, h: height, col: m.focusedCol})

	content := m.renderTicketList(tickets, m.focusedCol, innerWidth, visibleCount, m.cursors[m.focusedCol], borderColor,
		point{x: origin.x + 1, y: origin.y + 1})
	return renderPanel(title, content, width, height, borderColor, focused)
}

func (m *Model) renderSplitDetail(width, height int, focused bool, borderColor lipgloss.Color, origin point) string {
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
	m.addZone(hitZone{kind: zoneField, x: origin.x, y: origin.y, w: width, h: 3, idx: 0})

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
	m.addZone(hitZone{kind: zoneField, x: origin.x, y: origin.y + 3, w: width, h: 3, idx: 1})

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
		setDescWidth(&m.editDesc, innerWidth)
		m.editDesc.SetHeight(descPanelHeight - 2)
		descContent = m.editDesc.View()
	} else {
		descContent = m.renderDescBody(t.Description, innerWidth, descPanelHeight-2)
	}
	descPanel := renderPanel("Description", descContent, width, descPanelHeight, descColor, focused && m.editField == 2)
	m.addZone(hitZone{kind: zoneField, x: origin.x, y: origin.y + 6, w: width, h: descPanelHeight, idx: 2})

	return lipgloss.JoinVertical(lipgloss.Left, metaPanel, titlePanel, descPanel)
}

// renderDescBody renders a read-only description, wrapped to width and offset
// by the mouse scroll position. It also records how far the body can scroll so
// the wheel handler can clamp itself.
func (m *Model) renderDescBody(desc string, width, height int) string {
	if height < 1 {
		height = 1
	}
	if desc == "" {
		m.descScrollMax = 0
		m.descScroll = 0
		return lipgloss.NewStyle().Foreground(subtle).Render("(empty)")
	}

	wrapped := strings.Split(wrapDesc(desc, width), "\n")
	maxScroll := len(wrapped) - height
	if maxScroll < 0 {
		maxScroll = 0
	}
	m.descScrollMax = maxScroll
	if m.descScroll > maxScroll {
		m.descScroll = maxScroll
	}
	if m.descScroll < 0 {
		m.descScroll = 0
	}

	lines := wrapped[m.descScroll:]
	if len(lines) > height {
		lines = lines[:height]
	}
	return lipgloss.NewStyle().Foreground(softWhite).Render(strings.Join(lines, "\n"))
}

// viewColumn renders the expanded single-column view.
func (m *Model) viewColumn() string {
	status := model.ColumnOrder[m.focusedCol]
	tickets := m.board.ByStatus(status)
	availHeight := m.height - 1
	color := columnColor(status)

	title := fmt.Sprintf("[%d] %s (%d)", m.focusedCol, statusDisplay[status], len(tickets))

	innerWidth := m.width - 2

	cursor := m.cursors[m.focusedCol]

	// The zoomed view scrolls on the same sticky window as the column it zooms,
	// and registers the same click targets — without them the cursor could walk
	// off the bottom of the rendered slice and the mouse did nothing here.
	// A ticket costs one row, two when the cursor's description shows beneath.
	costs := make([]int, len(tickets))
	for i, t := range tickets {
		costs[i] = 1
		if i == cursor && t.Description != "" {
			costs[i] = 2
		}
	}
	body := availHeight - 2
	if body < 1 {
		body = 1
	}
	start := m.scrollWindow(m.focusedCol, costs, cursor, body)

	var lines []string
	for i := start; i < len(tickets); i++ {
		t := tickets[i]
		if len(lines) >= body {
			break
		}
		rowY := len(lines)

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

		// Panel border: body starts one row down and one column in.
		m.addTicketZone(m.focusedCol, i, 1, 1+rowY, innerWidth, len(lines)-rowY)
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
	m.addZone(hitZone{kind: zoneField, x: 0, y: 0, w: innerWidth + 2, h: 3, idx: 0})

	// Title field
	titleBorderColor := softWhite
	if m.editField == 1 {
		titleBorderColor = color
	}
	var titleContent string
	if m.editTitle.Focused() {
		m.editTitle.Width = innerWidth - 2
		titleContent = m.editTitle.View()
	} else {
		titleContent = lipgloss.NewStyle().Bold(true).Foreground(white).Render(t.Title)
	}
	titlePanel := renderPanel("Title", titleContent, innerWidth+2, 3, titleBorderColor, m.editField == 1)
	m.addZone(hitZone{kind: zoneField, x: 0, y: 3, w: innerWidth + 2, h: 3, idx: 1})

	// Description field
	descBorderColor := softWhite
	if m.editField == 2 {
		descBorderColor = color
	}
	descPanelHeight := m.height - 7
	if descPanelHeight < 4 {
		descPanelHeight = 4
	}
	var descContent string
	if m.editDesc.Focused() {
		setDescWidth(&m.editDesc, innerWidth-2)
		m.editDesc.SetHeight(descPanelHeight - 2)
		descContent = m.editDesc.View()
	} else {
		descContent = m.renderDescBody(t.Description, innerWidth-2, descPanelHeight-2)
	}
	descPanel := renderPanel("Description", descContent, innerWidth+2, descPanelHeight, descBorderColor, m.editField == 2)
	m.addZone(hitZone{kind: zoneField, x: 0, y: 6, w: innerWidth + 2, h: descPanelHeight, idx: 2})

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
