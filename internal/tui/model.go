package tui

import (
	"fmt"
	"maps"
	"os"
	"sort"
	"strings"
	"time"
	"unicode"

	"github.com/LeonY117/kanban-tui/internal/model"
	"github.com/LeonY117/kanban-tui/internal/store"
	"github.com/LeonY117/kanban-tui/internal/termwidth"
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
	boardView    viewMode = iota
	splitView             // list + detail side by side
	columnView            // full-width single column
	detailView            // full-screen detail editor
	archiveView           // archive browser (split: list + read-only detail)
	addView               // floating popup for new ticket
	pickerView            // floating board picker (main + sprints)
	moveView              // floating move-ticket picker (board / column)
	settingsView          // floating settings popup
	tagView               // floating tag picker, feeds the search
	infoView              // floating board-description popup
	emojiView             // floating emoji picker over any text field
)

// inputMode tracks what the user is typing into.
type inputMode int

const (
	inputNone inputMode = iota
	inputAdd
	inputAssign
	inputSelect // for status picker
)

// defaultStatusDisplay maps internal status to sentence-case display name.
var defaultStatusDisplay = map[model.Status]string{
	model.StatusBacklog: "Backlog",
	model.StatusTodo:    "Todo",
	model.StatusDoing:   "Doing",
	model.StatusDone:    "Done",
	model.StatusHold:    "Hold",
}

// defaultStatusShort is the compact label used in the board picker count strip.
var defaultStatusShort = map[model.Status]string{
	model.StatusBacklog: "B",
	model.StatusTodo:    "T",
	model.StatusDoing:   "Do",
	model.StatusDone:    "Dn",
	model.StatusHold:    "H",
}

// The labels the TUI actually draws. They start as the defaults and pick up
// whatever config.json renames, so two people can call one column different
// things without either board changing on disk.
var (
	statusDisplay = maps.Clone(defaultStatusDisplay)
	statusShort   = maps.Clone(defaultStatusShort)
)

// widthProfile is how the terminal in front of us measures emoji. Everything
// lipgloss lays out assumes the grapheme-aware answer; where the terminal
// disagrees, View corrects the finished frame rather than the layout — see
// internal/termwidth for why it cannot be done any earlier.
var widthProfile = termwidth.Grapheme

// ApplyConfig points the display labels and the keymap at the user's config,
// and reports any key override that had to be refused, plus any action left
// with no key because an override claimed its default. Call it before
// NewModel. Separate from NewModel so tests set preferences explicitly rather
// than inheriting whatever the machine running them has in config.json.
func ApplyConfig(cfg store.Config) (refused, unbound []string) {
	widthProfile, _ = termwidth.ParseProfile(cfg.TerminalWidth)
	statusDisplay = maps.Clone(defaultStatusDisplay)
	statusShort = maps.Clone(defaultStatusShort)
	for status, label := range cfg.Labels() {
		statusDisplay[status] = label
		// Two renamed columns sharing an initial share a short code, so the
		// picker's count strip can read "3W ... 4W". Accepted deliberately
		// (Leon, 2026-08-03): the strip is a glance, not a control, and
		// statusLabelsShort in config.json is the way out if it ever matters.
		statusShort[status] = firstRune(label)
	}
	for status, label := range cfg.ShortLabels() {
		statusShort[status] = label
	}
	return applyKeyBindings(cfg.Keys)
}

func firstRune(s string) string {
	for _, r := range s {
		return string(r)
	}
	return ""
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
	termWidth  int // the window's real width, before any reserve
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

	// What the previous click landed on, so acting can require a second click
	// rather than a first click on something already selected.
	lastClick clickTarget

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
	emojiPick      emojiPicker
	emojiType      emojiTypeahead
	addFocusIdx    int
	addDescEditing bool
	addConfirmQuit bool // esc pressed with content in the popup — awaiting y/N

	// Board picker state
	pickerBoards       []pickerEntry
	pickerIdx          int
	pickerWidestRow    int
	pickerShowArchived bool   // when true, picker lists archived sprints below active ones
	confirmArchive     string // non-empty = mid-confirm prompt for that sprint name

	// Sprint rename form, hosted inside the picker popup.
	renameTarget     string // non-empty = the sprint being renamed
	renameFromPrefix string // its prefix when the form opened, for the id hint
	renameName       textinput.Model
	renamePrefix     textinput.Model
	renameFocus      int // renameFocusName | renameFocusPrefix

	move moveState

	settings settingsState

	// Two filters, one implementation. The board's and the archive browser's
	// are separate values because they narrow separate lists — see
	// activeSearch for which one a keystroke reaches.
	search        searchState
	archiveSearch searchState

	tags tagPickerState

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

	windowTitle string // board name last written to the terminal title

	// The board-description popup — see info.go. infoBoard is the board being
	// described ("" for main), which is not always the one this Model is on.
	infoBoard     string
	infoText      string
	infoScroll    int
	infoScrollMax int
	infoEditing   bool
	infoDesc      textarea.Model
	infoReturn    viewMode // the view this popup closes back onto

	// Focus sits on the board name in the footer rather than on a card — see
	// footerfocus.go.
	footerFocus bool
}

// archiveEntry is a single row in the archive browser — either a date header
// or a ticket.
type archiveEntry struct {
	isHeader bool
	date     string // YYYY-MM-DD, set when isHeader
	ticket   model.Ticket
}

// pickerEntry is one board in the board picker — the main board or a sprint.
type pickerEntry struct {
	name     string // "" for main
	prefix   string // ticket-id prefix; "" on main, whose ids are bare numbers
	counts   map[model.Status]int
	archived bool // sprints only; main is never archived
	pinned   bool // main is always pinned; sprints opt in with p
}

// pickerLine is one rendered line of the board picker: a board, or the divider
// closing the pinned block. Keeping the divider out of pickerBoards means the
// cursor index stays a board index and never has to skip over a non-board row.
type pickerLine struct {
	boardIdx int // index into pickerBoards; -1 for the divider
}

// boardLines lays a board list out: pinned boards, a divider, then the rest.
// The divider is dropped when either side of it is empty — with nothing pinned
// but main, a line under main is noise rather than structure.
//
// The board picker and the move popup both draw from this, so a board's pins
// sit in the same place wherever the list is shown.
func boardLines(entries []pickerEntry) []pickerLine {
	lines := make([]pickerLine, 0, len(entries)+1)
	pinnedSprints := 0
	for _, e := range entries {
		if e.pinned && e.name != "" {
			pinnedSprints++
		}
	}
	for i, e := range entries {
		if !e.pinned && pinnedSprints > 0 && (i == 0 || entries[i-1].pinned) {
			lines = append(lines, pickerLine{boardIdx: -1})
		}
		lines = append(lines, pickerLine{boardIdx: i})
	}
	return lines
}

func (m *Model) pickerLines() []pickerLine { return boardLines(m.pickerBoards) }

// pickerLineOf returns the rendered line index showing a given board.
func pickerLineOf(lines []pickerLine, boardIdx int) int {
	for i, l := range lines {
		if l.boardIdx == boardIdx {
			return i
		}
	}
	return 0
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
		store:         s,
		board:         board,
		sprintName:    sprintName,
		input:         ti,
		search:        newSearchState(),
		archiveSearch: newSearchState(),
		focusedCol:    1, // default to Todo
		lastModTime:   modTime,
		archived:      archived,
	}, nil
}

// guardMutate returns true if the mutation should proceed. It refuses on an
// archived sprint, and on a card a global search borrowed from another board:
// this Model holds one store, and that card's writes belong to a different
// one. enter follows it home, where everything works normally.
func (m *Model) guardMutate() bool {
	if !m.guardBoardMutate() {
		return false
	}
	// The borrowed-card check reads the board cursor, which only speaks for
	// the mutation in views that cursor drives. The archive browser has its
	// own cursor over its own entries, and those are always this board's — so
	// consulting the board selection there refuses an unarchive over a card
	// that isn't even on screen.
	if m.view == archiveView {
		return true
	}
	if t := m.selectedTicket(); t != nil {
		if owner, ok := m.ticketOwner(t.ID); ok {
			m.notice = fmt.Sprintf("%s lives on %s — enter to open it there", t.ShortID, boardDisplayName(owner))
			return false
		}
	}
	return true
}

// guardZoom refuses the zoom ladder while a search is active. A filtered board
// is a narrower surface on purpose (Leon, 2026-08-03): keeping the zoom ladder
// out of it means one less view that has to reason about borrowed cards, and
// esc is one keystroke away.
func (m *Model) guardZoom() bool {
	if m.searchActive() {
		m.notice = "zoom is off while searching — esc clears the filter"
		return false
	}
	return true
}

// guardBoardMutate is guardMutate without the borrowed-card check, for
// mutations aimed at the board rather than at the selection. Adding a card
// writes to the board you are standing on whatever the cursor happens to be
// resting on.
func (m *Model) guardBoardMutate() bool {
	if m.archived {
		m.notice = fmt.Sprintf("sprint %q is archived — `kanban sprints unarchive %s` to edit", m.sprintName, m.sprintName)
		return false
	}
	return true
}

func (m *Model) footerLine() string {
	badgeStyle := sprintBadgeStyle
	if m.badgeLit() {
		badgeStyle = sprintBadgeFocusStyle
	}
	badge := badgeStyle.Render(boardDisplayName(m.sprintName))
	// The board's name is the thing on screen that identifies the board, so it
	// is also where a click asking "what is this board?" lands. Registered on
	// the name alone, not the chips beside it, which mean other things.
	//
	// Not while the search input is open: it takes keys ahead of the view, so a
	// popup opened behind it would ignore every key and the esc dismissing it
	// would cancel the search instead.
	if !m.activeSearch().open {
		m.addZone(hitZone{kind: zoneBoardBadge, x: 0, y: m.height - 1, w: lipgloss.Width(badge), h: 1})
	}
	// The active filter rides next to the board's name, in green, because it
	// changes what the board in front of you means. It sits inside the badge
	// rather than in the hint text so it is never the thing that gets trimmed
	// away — a narrowed board that looks like an empty one is the failure
	// worth spending a permanent slot on.
	// Not while the input is open: the input beside it already shows the
	// query, and printing it twice costs the line ~25 columns in the one
	// state where the input, the completions, the count and the hints are
	// already competing for it.
	if m.filterBadgeVisible() {
		badge = lipgloss.JoinHorizontal(lipgloss.Top, badge,
			lipgloss.NewStyle().Foreground(green).Bold(true).Render(m.filterBadge()))
	}
	// A hint at what ids new tickets here will carry — not part of the
	// board's name, so it appears here and nowhere else.
	badge = lipgloss.JoinHorizontal(lipgloss.Top, badge,
		dimStyle.Render("["+prefixLabel(store.EffectivePrefix(m.board, m.sprintName))+"]"))
	if m.archived {
		archivedTag := lipgloss.NewStyle().Foreground(dimGray).Render("[archived]")
		badge = lipgloss.JoinHorizontal(lipgloss.Top, badge, archivedTag)
	}

	if m.activeSearch().open {
		return m.searchFooter(badge)
	}

	budget := m.width - lipgloss.Width(badge) - 2
	var rightText string
	switch {
	case m.notice != "":
		rightText = m.notice
	case m.activeSearch().active():
		// The chip is rendered outside fitHints rather than as its leading
		// hint: fitHints protects only the last hint, so a long enough query
		// could push out the very thing that explains why the board looks
		// half empty.
		chip := m.searchCountLabel()
		rightText = chip + " | " + fitHints(m.helpText(), hintSep, budget-lipgloss.Width(chip)-3)
	default:
		rightText = fitHints(m.helpText(), hintSep, budget)
	}
	help := helpStyle.Render(rightText)
	return m.renderFooter(badge, help)
}

// renderFooter joins the footer pieces and ensures the line never leaves the
// terminal, whatever space the pieces negotiated among themselves.
//
// It matters more than a trimmed hint would suggest. lipgloss pads every board
// row out to the widest line in the frame, so a footer one cell too wide does
// not wrap on its own — it widens the entire board and wraps all of it.
func (m *Model) renderFooter(parts ...string) string {
	line := lipgloss.JoinHorizontal(lipgloss.Center, parts...)
	if m.width < 1 || lipgloss.Width(line) <= m.width {
		return line
	}
	return ansi.Truncate(line, m.width, "…")
}

// The two separators help lines are written with: the board and pickers use a
// pipe, the add popup a bullet.
const (
	hintSep   = " | "
	bulletSep = "  •  "
)

// fitHints drops whole hints off the end of a help line until it fits, keeping
// the last one — how to get out of wherever you are.
//
// Bubble Tea truncates a rendered line to the terminal width with no ellipsis,
// and a popup's interior truncates rather than wrapping, so an over-long footer
// loses its tail silently. The footer is the only place a popup's keys are
// documented, and the tail is where the close hint lives, so losing it strands
// the user in a popup with no visible way out. The add popup found this the
// hard way: its interior is 62 cells on a wide terminal but 54 at the narrowest
// supported width, where one added hint took `esc: cancel` with it.
func fitHints(text, sep string, width int) string {
	if width < 1 || lipgloss.Width(text) <= width {
		return text
	}
	hints := strings.Split(text, sep)
	if len(hints) < 2 {
		return text
	}
	// The escape hint is the one worth protecting; everything before it is
	// negotiable, dropped from the end so the leading hints survive.
	last := hints[len(hints)-1]
	for n := len(hints) - 1; n > 0; n-- {
		// Concatenating leaves the hint list intact; appending onto hints[:n]
		// could overwrite the final hint we are protecting.
		candidate := strings.Join(hints[:n], sep) + sep + last
		if lipgloss.Width(candidate) <= width {
			return candidate
		}
	}
	return last
}

func (m *Model) Init() tea.Cmd {
	return tea.Batch(tickCmd(), m.titleCmd())
}

// titleCmd names the terminal window after the board in view, so a tab running
// kanban reads as the sprint rather than as the tool. It returns nil when the
// title is already right: every Update runs through here, and re-announcing the
// same title on each keystroke would be noise on the wire.
//
// Nothing restores the title on exit — the shell integration that put the
// command name there in the first place rewrites it at the next prompt.
func (m *Model) titleCmd() tea.Cmd {
	title := boardDisplayName(m.sprintName)
	if title == m.windowTitle {
		return nil
	}
	m.windowTitle = title
	return tea.SetWindowTitle(title)
}

// Update wraps the real update so that a board switch renames the window from
// one place. switchBoard has five call sites across the picker and both global
// search jumps; threading a title command out of each would leave the sixth to
// remember.
func (m *Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	// The `:` typeahead brackets the real update: it takes the few keys it
	// owns while its list is on screen, and otherwise lets the keystroke reach
	// the field and mirrors it afterwards. One place, rather than a hook in
	// each of the seven handlers that own a text widget.
	// Named keyMsg, not key: this file's other handlers reach for the bubbles
	// key package by that name.
	keyMsg, isKey := msg.(tea.KeyMsg)
	if isKey {
		if consumed, cmd := m.typeaheadKey(keyMsg); consumed {
			return m, tea.Batch(cmd, m.titleCmd())
		}
	}

	next, cmd := m.update(msg)
	if isKey {
		m.trackTypeahead(keyMsg)
	}
	return next, tea.Batch(cmd, m.titleCmd())
}

func (m *Model) update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tickMsg:
		if info, err := os.Stat(m.store.BoardPath()); err == nil {
			if info.ModTime().After(m.lastModTime) {
				m.lastModTime = info.ModTime()
				m.reload()
				m.clampCursors()
			}
		}
		m.refreshInfoText()
		return m, tickCmd()

	case tea.WindowSizeMsg:
		m.termWidth = msg.Width
		m.applyWidthProfile()
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
		// The search input sits over the footer of whichever view is behind
		// it, so it takes keys ahead of that view rather than being one.
		if m.activeSearch().open {
			return m.updateSearch(msg)
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
		case settingsView:
			return m.updateSettings(msg)
		case tagView:
			return m.updateTagPicker(msg)
		case infoView:
			return m.updateInfo(msg)
		case emojiView:
			return m.updateEmojiPicker(msg)
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

	// Settings previews a profile live, so the geometry has to follow it
	// before anything is laid out — the reserve moves with the profile.
	m.applyWidthProfile()

	m.resetZones()
	content := m.renderView(m.view)

	// Add input bar or picker if active
	if m.inputMode == inputSelect {
		content = lipgloss.JoinVertical(lipgloss.Left, content, m.viewSelect())
	} else if m.inputMode != inputNone {
		content = lipgloss.JoinVertical(lipgloss.Left, content, m.viewInput())
	}

	// Late, so it floats over whatever was drawn — and after the zones it
	// anchors to have been registered by the render above.
	content = m.overlayTypeahead(content)

	// The frame is laid out in the cells lipgloss counts; the terminal may
	// spend fewer on the same glyphs. Hand back the cells it declines to
	// spend, so what gets painted is the grid this was built on.
	return termwidth.Compensate(content, m.widthProfile(), termwidth.Reserve)
}

// widthProfile is how the terminal in front of us measures emoji right now.
// While the settings popup is open that is whatever it is previewing, so the
// board redraws under each option as the cursor moves over it.
func (m *Model) widthProfile() termwidth.Profile {
	if m.view == settingsView {
		return m.settings.previewProfile()
	}
	return widthProfile
}

// applyWidthProfile sizes the layout for the profile in force. A terminal that
// needs cells handed back is laid out narrower by exactly that reserve, so a
// compensated line never trips Bubble Tea's truncation — which measures with
// lipgloss and would cut off the very cells we added.
func (m *Model) applyWidthProfile() {
	if m.termWidth == 0 {
		return // no WindowSizeMsg yet; tests set m.width directly
	}
	m.width = m.termWidth
	if m.widthProfile() != termwidth.Grapheme {
		m.width -= termwidth.Reserve
	}
}

func (m *Model) reload() {
	board, err := m.store.Load()
	if err != nil {
		m.err = err
		return
	}
	m.board = board
	// Under a global search the other boards are on screen too, so they go
	// stale on the same beat this one does.
	m.loadForeign()

	// A write can change a field the filter reads — title, tags, assignee —
	// so the card just saved may have left the visible set on its own edit.
	// Cursors index what is on screen, so re-clamp, and re-seed the detail
	// editors when the selection has moved out from under them: the pane
	// renders from selectedTicket while writes go to editTicketID, and a
	// mismatch sends the next save to a card nobody is looking at.
	m.clampCursors()
	// The emoji picker floats over a still-focused editor without changing
	// what is being edited, so the guard has to see through it to the view
	// underneath — otherwise a reload while the picker is open skips the
	// re-seed and the next save lands on a card nobody is looking at.
	editing := m.view
	if editing == emojiView {
		editing = m.emojiPick.returnView
	}
	if editing == splitView || editing == detailView {
		if t := m.selectedTicket(); t == nil || t.ID != m.editTicketID {
			m.refreshDetailEditors()
		}
	}
}

func (m *Model) selectedTicket() *model.Ticket {
	// While the footer holds focus the board draws no selection, so there is
	// no selected ticket to report either. Without this the card verbs — x, m,
	// c, H/L, J/K — keep acting on the remembered cursor, and a ticket can be
	// archived with nothing on screen naming it.
	if m.footerHasFocus() {
		return nil
	}
	status := model.ColumnOrder[m.focusedCol]
	tickets := m.visibleTickets(status)
	idx := m.cursors[m.focusedCol]
	if idx >= len(tickets) {
		return nil
	}
	return &tickets[idx]
}

// wideLayoutMinWidth is the terminal width above which all 5 columns render
// side-by-side. Below it, a 3-column sliding window centered on focus is used.
const wideLayoutMinWidth = 150

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
	if m.width >= wideLayoutMinWidth {
		return []int{0, 1, 2, 3, 4}
	}
	switch m.focusedCol {
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
	// Moving sideways is a return to the cards — see footerfocus.go.
	m.footerFocus = false
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
	count := len(m.visibleTickets(status))
	if m.cursors[m.focusedCol] < count-1 {
		m.cursors[m.focusedCol]++
	}
}

func (m *Model) clampCursors() {
	for i, status := range model.ColumnOrder {
		count := len(m.visibleTickets(status))
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
		if m.footerFocusUp() {
			return m, nil
		}
		m.moveCursor(-1)
	case key.Matches(msg, keys.Down):
		if m.footerFocusDown() {
			return m, nil
		}
		m.moveCursor(1)
	case key.Matches(msg, keys.Enter):
		if m.footerFocusEnter() {
			return m, nil
		}
		// enter is the only key that follows a borrowed card home. Overloading
		// zoom or panel-navigation with it made those keys do something their
		// own help line doesn't describe.
		if m.jumpToForeign() {
			return m, nil
		}
		m.enterSplit()
		return m, nil
	case key.Matches(msg, keys.Zoom):
		if !m.guardZoom() {
			return m, nil
		}
		m.enterSplit()
		return m, nil
	case key.Matches(msg, keys.Esc):
		if m.searchActive() {
			m.clearSearch()
		}
	case key.Matches(msg, keys.Search):
		m.enterSearch()
	case key.Matches(msg, keys.Add):
		return m.enterAddPopup()
	case key.Matches(msg, keys.Zero):
		m.jumpToColumn(0)
	case key.Matches(msg, keys.One):
		m.jumpToColumn(1)
	case key.Matches(msg, keys.Two):
		m.jumpToColumn(2)
	case key.Matches(msg, keys.Three):
		m.jumpToColumn(3)
	case key.Matches(msg, keys.Four):
		m.jumpToColumn(4)
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
	case key.Matches(msg, keys.Bigger):
		m.resizeTickets(1)
	case key.Matches(msg, keys.Smaller):
		m.resizeTickets(-1)
	case key.Matches(msg, keys.Move):
		return m.enterMovePopup()
	case key.Matches(msg, keys.Help):
		return m.enterSettings()
	case key.Matches(msg, keys.Copy):
		m.copyFocused()
	case key.Matches(msg, keys.ArchiveView):
		m.enterArchive()
	case key.Matches(msg, keys.BoardPicker):
		return m.enterPicker()
	case key.Matches(msg, keys.TagPicker):
		return m.enterTagPicker()
	case key.Matches(msg, keys.Info):
		m.enterInfo(m.sprintName)
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
	// A pending emoji pick or shortcode was aimed at the editors these lines
	// are about to replace, and neither remembers which ticket it was armed
	// on. Left standing, the next enter writes the emoji into whichever card
	// the editors now hold: open the picker on one card, let an agent move it,
	// and the pick lands on the card that slid into its place. Closing under
	// the user is the mild outcome here.
	if m.emojiPick.open {
		m.closeEmojiPicker()
	}
	m.emojiType = emojiTypeahead{}

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
		if !m.guardZoom() {
			return m, nil
		}
		m.view = columnView
	case key.Matches(msg, keys.Enter):
		if m.jumpToForeign() {
			return m, nil
		}
		m.splitFocus = 1
		m.refreshDetailEditors() // start on meta, nothing focused
	case key.Matches(msg, keys.PanelNext), key.Matches(msg, keys.Right):
		// A borrowed card opens read-only here; every write path is refused by
		// guardMutate and names the board to press enter for.
		m.splitFocus = 1
		m.refreshDetailEditors()
	case key.Matches(msg, keys.Search):
		m.enterSearch()
	case key.Matches(msg, keys.Up):
		if m.cursors[m.focusedCol] > 0 {
			m.cursors[m.focusedCol]--
			m.refreshDetailEditors()
		}
	case key.Matches(msg, keys.Down):
		status := model.ColumnOrder[m.focusedCol]
		count := len(m.visibleTickets(status))
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
	case key.Matches(msg, keys.Help):
		return m.enterSettings()
	case key.Matches(msg, keys.Copy):
		m.copyFocused()
	case key.Matches(msg, keys.Bigger):
		m.resizeTickets(1)
	case key.Matches(msg, keys.Smaller):
		m.resizeTickets(-1)
	case key.Matches(msg, keys.TagPicker):
		return m.enterTagPicker()
	case key.Matches(msg, keys.ArchiveView):
		m.enterArchive()
	}
	return m, nil
}

func (m *Model) updateSplitDetail(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	// enter follows a borrowed card home from this pane too. Every write here
	// is refused for a foreign card with a notice naming enter as the way to
	// open it on its own board — and this was the one surface where that
	// instruction did nothing, because enter reached the field handler and was
	// refused by the same guard that had just recommended it.
	//
	// Safe ahead of the field handlers: jumpToForeign reports false for a
	// local selection, and a borrowed card can never have a focused editor —
	// guardMutate refuses before anything gets focus.
	if key.Matches(msg, keys.Enter) && m.jumpToForeign() {
		return m, nil
	}
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
		if !m.guardZoom() {
			return m, nil
		}
		m.enterDetail()
		return m, nil
	case key.Matches(msg, keys.Down):
		m.editField = 1
	case key.Matches(msg, keys.Enter):
		return m.editMetaField()
	case key.Matches(msg, keys.Move):
		return m.enterMovePopup()
	case key.Matches(msg, keys.Help):
		return m.enterSettings()
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
	}
	return m, nil
}

func (m *Model) updateSplitDetailTitle(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if m.editTitle.Focused() {
		// Editing mode
		if key.Matches(msg, keys.Emoji) {
			return m.openEmojiPicker(emojiToEditTitle)
		}
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
		if !m.guardZoom() {
			return m, nil
		}
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
		// Editing mode: enter saves and drops back out, shift+enter (see
		// keys.NewLine) is the newline — the textarea's own binding.
		if key.Matches(msg, keys.Enter) || key.Matches(msg, keys.Esc) {
			m.editDesc.Blur()
			m.saveEdit()
			return m, nil
		}
		if key.Matches(msg, keys.Emoji) {
			return m.openEmojiPicker(emojiToEditDesc)
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
		if !m.guardZoom() {
			return m, nil
		}
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
		count := len(m.visibleTickets(status))
		if m.cursors[m.focusedCol] < count-1 {
			m.cursors[m.focusedCol]++
		}
	case key.Matches(msg, keys.Enter):
		if m.jumpToForeign() {
			return m, nil
		}
		if m.selectedTicket() != nil {
			return m.enterDetail()
		}
	case key.Matches(msg, keys.Search):
		m.enterSearch()
	case key.Matches(msg, keys.TagPicker):
		return m.enterTagPicker()
	case key.Matches(msg, keys.Add):
		return m.enterAddPopup()
	case key.Matches(msg, keys.Zero):
		m.jumpToColumn(0)
	case key.Matches(msg, keys.One):
		m.jumpToColumn(1)
	case key.Matches(msg, keys.Two):
		m.jumpToColumn(2)
	case key.Matches(msg, keys.Three):
		m.jumpToColumn(3)
	case key.Matches(msg, keys.Four):
		m.jumpToColumn(4)
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
	case key.Matches(msg, keys.Help):
		return m.enterSettings()
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
	case key.Matches(msg, keys.Help):
		return m.enterSettings()
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
	}
	return m, nil
}

func (m *Model) editMetaField() (tea.Model, tea.Cmd) {
	if !m.guardMutate() {
		return m, nil
	}
	switch m.metaIdx {
	case 0: // status
		labels, byLabel := statusChoices()
		m.startSelect("Status", labels, func(val string) {
			status, ok := byLabel[val]
			if !ok {
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
		switch {
		case msg.String() == "esc", msg.String() == "enter":
			m.editTitle.Blur()
			m.saveEdit()
			return m, nil
		case key.Matches(msg, keys.Emoji):
			return m.openEmojiPicker(emojiToEditTitle)
		// Goes through the binding rather than a literal "tab": the picker key
		// is rebindable, and a hard-coded alias here stayed live after a rebind.
		case key.Matches(msg, keys.BoardPicker):
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
		if key.Matches(msg, keys.Emoji) {
			return m.openEmojiPicker(emojiToEditDesc)
		}
		if key.Matches(msg, keys.BoardPicker) {
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
	// Without this the key reaches textinput.Update and types a literal "e"
	// into the tag or assignee being edited — the one text field in the TUI
	// where the emoji key did nothing it advertised.
	if key.Matches(msg, keys.Emoji) {
		return m.openEmojiPicker(emojiToMetaInput)
	}
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

// viewSelect draws the meta-bar option strip.
//
// Options are user-supplied once columns can be renamed, so the strip has to
// fit the terminal rather than assume four short words: it lays out on one
// line, and bubbletea clips an over-long line with no ellipsis, which would
// leave the user able to arrow onto an option they cannot read. Labels share
// the space left after the prompt, shrinking together so the row stays legible
// before it stays complete.
func (m *Model) viewSelect() string {
	prompt := helpStyle.Render(m.selectLabel + ":")
	// Measure the decoration rather than assuming it: helpStyle carries
	// Padding(0, 1), so every rendered part costs two cells beyond its text.
	overhead := lipgloss.Width(helpStyle.Render("   "))
	budget := m.width - lipgloss.Width(prompt)
	perOption := 0
	if n := len(m.selectOptions); n > 0 {
		perOption = budget/n - overhead
	}

	var parts []string
	parts = append(parts, prompt)
	for i, opt := range m.selectOptions {
		if perOption > 0 && lipgloss.Width(opt) > perOption {
			opt = ansi.Truncate(opt, perOption, "…")
		}
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
	newColTickets := m.visibleTickets(newStatus)
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
	colTickets := m.visibleTickets(model.ColumnOrder[m.focusedCol])
	cursor := m.cursors[m.focusedCol]
	newCursor := cursor + dir
	if newCursor < 0 || newCursor >= len(colTickets) {
		return
	}
	neighbour := colTickets[newCursor]

	// Reordering is a rewrite of this board's ticket order, so it stops where
	// this board does. A borrowed neighbour cannot be swapped with — say so
	// rather than no-oping, which used to look identical to a successful move
	// because the cursor advanced either way.
	if owner, ok := m.ticketOwner(neighbour.ID); ok {
		m.notice = fmt.Sprintf("%s lives on %s — reordering stops at this board", neighbour.ShortID, boardDisplayName(owner))
		return
	}

	moved := false
	if err := m.store.WithLock(func() error {
		board, err := m.store.Load()
		if err != nil {
			return err
		}
		_, i := board.FindByID(t.ID)
		_, j := board.FindByID(neighbour.ID)
		if i < 0 || j < 0 {
			return nil
		}
		board.Tickets[i], board.Tickets[j] = board.Tickets[j], board.Tickets[i]
		if err := m.store.Save(board); err != nil {
			return err
		}
		moved = true
		return nil
	}); err != nil {
		m.notice = err.Error()
	}
	m.reload()
	// Only follow the card if it actually went somewhere.
	if moved {
		m.cursors[m.focusedCol] = newCursor
	}
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
		// Qualified for a borrowed card, exactly as every surface renders it.
		// A bare DE1 does not resolve from the board you are standing on, and
		// nothing in the clipboard would have said which board it came from.
		value = m.boardBadge(t.ID) + t.ShortID
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
	// Load before switching view, so an unreadable archive leaves you on the
	// board with the error rather than inside a panel that looks like an empty
	// archive. Nothing here needs the view set first: loadForeignArchive and
	// visibleArchiveEntries read m.archiveSearch directly rather than through
	// activeSearch.
	//
	// The cursor starts at the top and the one clamp finds the first row that
	// is actually a ticket. Picking the index here as well meant two places
	// deciding where the cursor may rest.
	m.archiveCursor = 0
	if !m.loadForeignArchive() {
		return
	}
	m.view = archiveView
}

func (m *Model) updateArchive(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch {
	case key.Matches(msg, keys.Quit):
		return m, tea.Quit
	case key.Matches(msg, keys.Esc), key.Matches(msg, keys.Unzoom), key.Matches(msg, keys.ArchiveView):
		// esc clears a filter before it closes the browser, exactly as it does
		// on the board — leaving takes one more press than clearing, so a
		// narrowed archive is never abandoned silently.
		if key.Matches(msg, keys.Esc) && m.archiveSearch.active() {
			m.clearSearch()
			return m, nil
		}
		m.view = boardView
	case key.Matches(msg, keys.Up):
		m.moveArchiveCursor(-1)
		m.descScroll = 0
	case key.Matches(msg, keys.Down):
		m.moveArchiveCursor(1)
		m.descScroll = 0
	case key.Matches(msg, keys.Unarchive):
		m.unarchiveSelected()
	case key.Matches(msg, keys.Search):
		m.enterSearch()
	case key.Matches(msg, keys.Enter):
		m.jumpToForeignArchive()
	case key.Matches(msg, keys.Copy):
		if t := m.archiveSelected(); t != nil {
			m.copyToClipboard(m.boardBadge(t.ID)+t.ShortID, copyID)
		}
	}
	return m, nil
}

func (m *Model) moveArchiveCursor(dir int) {
	entries := m.visibleArchiveEntries()
	n := len(entries)
	if n == 0 {
		return
	}
	i := m.archiveCursor + dir
	for i >= 0 && i < n && entries[i].isHeader {
		i += dir
	}
	if i < 0 || i >= n {
		return
	}
	m.archiveCursor = i
}

func (m *Model) archiveSelected() *model.Ticket {
	entries := m.visibleArchiveEntries()
	if m.archiveCursor < 0 || m.archiveCursor >= len(entries) {
		return nil
	}
	e := &entries[m.archiveCursor]
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
	// A row borrowed from another board's archive belongs to a different
	// store; unarchiving it here would write this board's file.
	if owner, ok := m.ticketOwner(t.ID); ok {
		m.notice = fmt.Sprintf("%s lives on %s — enter to open it there", t.ShortID, boardDisplayName(owner))
		return
	}
	if err := m.store.Unarchive(t.ID); err != nil {
		m.err = err
		return
	}
	m.reload()
	m.clampCursors()
	m.loadForeignArchive()
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
	entries := m.visibleArchiveEntries()
	title := fmt.Sprintf("Archive (%d)", countArchiveTickets(entries))
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
	for i := startIdx; i < len(entries) && len(lines) < visibleCount; i++ {
		e := entries[i]
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
	if !m.guardBoardMutate() {
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
	m.emojiPick = emojiPicker{}
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
		if key.Matches(msg, keys.Emoji) {
			return m.openEmojiPicker(emojiToAddDesc)
		}
		var cmd tea.Cmd
		m.addDesc, cmd = m.addDesc.Update(msg)
		return m, cmd
	}

	// Ahead of the switch below, which reads literal key names: this one is
	// rebindable, so it has to go through the binding.
	if key.Matches(msg, keys.Emoji) {
		switch m.addFocusIdx {
		case addFocusDesc:
			return m.openEmojiPicker(emojiToAddDesc)
		case addFocusTags:
			return m.openEmojiPicker(emojiToAddTags)
		case addFocusAssign:
			return m.openEmojiPicker(emojiToAddAssign)
		}
		return m.openEmojiPicker(emojiToAddTitle)
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
	m.focusAddField((m.addFocusIdx + dir + 4) % 4)
}

// focusAddField hands the popup's keyboard to one field, blurring whichever had
// it. Reached by tab and by a click on the field.
func (m *Model) focusAddField(idx int) {
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
	m.addFocusIdx = idx
	switch idx {
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

	added, err := m.store.Add(title, desc, status, tags, assign, "tui")
	if err != nil {
		m.err = err
		return
	}
	m.reload()

	// Move the cursor before closing the popup, never after: closeAddPopup
	// re-seeds the detail editors from wherever the cursor is, so closing
	// first binds them to the card that was selected before the add and the
	// next save in the detail pane renames that one instead.
	//
	// Follow the new card by identity rather than by "last in the column":
	// under a filter those are different rows, and if the filter hides it
	// there is no row to land on at all. Saved but invisible looks exactly
	// like a failed save, so say so instead of moving the cursor somewhere
	// arbitrary.
	hidden := added != nil && m.searchActive() && !m.search.parsed.Match(*added)
	if added == nil || hidden {
		m.clampCursors()
	} else {
		m.focusTicket(added.ID)
	}
	m.closeAddPopup()
	if hidden {
		m.notice = fmt.Sprintf("%s saved — the search is hiding it", added.ShortID)
	}
}

// addPopupSize is the new-ticket popup's outer size. Split out from viewAdd so
// the help-line width test can assert against the same geometry the renderer
// uses, rather than restating the numbers.
func (m *Model) addPopupSize() (int, int) {
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
	return popupWidth, popupHeight
}

// addInnerWidth is the width the popup's inner panels render at.
func (m *Model) addInnerWidth() int {
	w, _ := m.addPopupSize()
	if w-4 < 10 {
		return 10
	}
	return w - 4
}

func (m *Model) viewAdd() string {
	popupWidth, popupHeight := m.addPopupSize()

	backdrop := m.popupBackdrop(m.popupReturnView)
	// The backdrop's zones belong to a view the user can't reach right now.
	m.resetZones()
	origin := m.popupOrigin(popupWidth, popupHeight)
	popup := m.renderAddPopup(popupWidth, popupHeight, origin)
	return overlayAt(backdrop, popup, origin.x, origin.y)
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
	case settingsView:
		return m.viewSettings()
	case tagView:
		return m.viewTagPicker()
	case infoView:
		return m.viewInfo()
	case emojiView:
		return m.viewEmoji()
	default:
		return m.viewBoard()
	}
}

// popupBackdrop renders the source view as the backdrop behind a popup, but
// avoids recursing into popup views themselves.
func (m *Model) popupBackdrop(source viewMode) string {
	if source == addView || source == pickerView || source == moveView || source == settingsView || source == tagView || source == infoView || source == emojiView {
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

func (m *Model) renderAddPopup(width, height int, origin point) string {
	// width is the outer popup width. The outer border eats 2 cols; we also
	// reserve 1 col of left pad + 1 col of right pad so inner panels sit
	// symmetrically inside the popup.
	innerWidth := m.addInnerWidth()

	// Content starts one row below the popup's top border, one column in from
	// its left border, plus the left pad every line below carries. While the
	// discard prompt is up no field is clickable: a click that moved focus
	// would answer a [y/N] question with something else entirely.
	live := !m.addConfirmQuit
	rowX, rowY := origin.x+2, origin.y+1

	status := model.ColumnOrder[m.focusedCol]
	accent := columnColor(status)

	meta := m.renderAddMeta(point{x: rowX, y: rowY}, live)

	titleColor := softWhite
	if m.addFocusIdx == addFocusTitle {
		titleColor = accent
	}
	m.addTitle.Width = innerWidth - 2
	titlePanel := renderPanel("Title", m.addTitle.View(), innerWidth, 3, titleColor, m.addFocusIdx == addFocusTitle)
	if live {
		m.addZone(hitZone{kind: zoneAddField, x: rowX, y: rowY + 1, w: innerWidth, h: 3, idx: addFocusTitle})
	}

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
	if live {
		m.addZone(hitZone{kind: zoneAddField, x: rowX, y: rowY + 4, w: innerWidth, h: descHeight, idx: addFocusDesc})
	}

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
// origin is where the line starts on screen, for the assign and tags click
// zones. The status field carries no zone: it is the column the popup was
// opened on, not something the form can change.
func (m *Model) renderAddMeta(origin point, live bool) string {
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

	if live {
		const gap = 2 // the "  " the parts are joined with
		x := origin.x + lipgloss.Width(statusRender) + gap
		m.addZone(hitZone{kind: zoneAddField, x: x, y: origin.y, w: lipgloss.Width(assignRender), h: 1, idx: addFocusAssign})
		x += lipgloss.Width(assignRender) + gap
		m.addZone(hitZone{kind: zoneAddField, x: x, y: origin.y, w: lipgloss.Width(tagsRender), h: 1, idx: addFocusTags})
	}

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

	// MaxWidth truncates rather than wraps, so this line has a budget and the
	// tail is what gets eaten — which is where "esc: cancel" lives. "tab:
	// field" implies shift+tab the way every other TUI does, buying room.
	parts := []string{
		"tab: field",
		"enter: save",
		hk("card.emoji") + ": emoji",
	}
	if m.addFocusIdx == addFocusDesc {
		if m.addDescEditing {
			parts = []string{"enter: save", "shift+enter: new line", "esc: exit edit"}
		} else {
			parts = append(parts, "enter: edit", "h/l: field")
		}
	}
	parts = append(parts, "esc: cancel")
	// helpStyle pads a cell either side, so the text budget is two
	// short of the interior.
	return helpStyle.Render(fitHints(strings.Join(parts, bulletSep), bulletSep, m.addInnerWidth()-2))
}

// ─── Board picker ───────────────────────────────────────────────────

func (m *Model) enterPicker() (tea.Model, tea.Cmd) {
	// Each open is a clean default view — show-archived, confirm and rename
	// state are session-scoped to one popup, not persistent.
	m.pickerShowArchived = false
	m.confirmArchive = ""
	m.renameTarget = ""
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
	m.pickerWidestRow = widestPickerRow(entries)
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

// loadPickerEntries returns main first (always sticky, always pinned), then
// pinned sprints in pin order, then the remaining active sprints by most
// recently edited; if includeArchived, archived sprints follow at the bottom in
// the same order. ListSprints does the pinned-first sorting.
func loadPickerEntries(includeArchived bool) ([]pickerEntry, error) {
	mainStore := store.New("")
	mainBoard, err := mainStore.Load()
	if err != nil {
		return nil, err
	}
	entries := []pickerEntry{{name: "", counts: store.CountByStatus(mainBoard), pinned: true}}

	sprints, err := store.ListSprints()
	if err != nil {
		return nil, err
	}
	for _, s := range sprints {
		if s.Archived && !includeArchived {
			continue
		}
		entries = append(entries, pickerEntry{
			name:     s.Name,
			prefix:   s.Prefix,
			counts:   s.StatusCounts,
			archived: s.Archived,
			pinned:   s.Pinned,
		})
	}
	return entries, nil
}

func (m *Model) updatePicker(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if m.confirmArchive != "" {
		return m.updatePickerConfirm(msg)
	}
	if m.renameTarget != "" {
		return m.updatePickerRename(msg)
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
	case key.Matches(msg, keys.Pin):
		return m.pickerTogglePin()
	case key.Matches(msg, keys.TagPicker):
		return m.enterTagPicker()
	case key.Matches(msg, keys.Rename):
		return m.startPickerRename()
	case key.Matches(msg, keys.MoveUp):
		return m.pickerReorderPin(-1)
	case key.Matches(msg, keys.MoveDown):
		return m.pickerReorderPin(1)
	case key.Matches(msg, keys.Info):
		// The highlighted board, not the current one — reading what a sprint
		// covers before switching into it is the point.
		if e, ok := m.selectedPickerBoard(); ok {
			m.enterInfo(e.name)
		}
	}
	return m, nil
}

func (m *Model) selectedPickerBoard() (pickerEntry, bool) {
	if m.pickerIdx < 0 || m.pickerIdx >= len(m.pickerBoards) {
		return pickerEntry{}, false
	}
	return m.pickerBoards[m.pickerIdx], true
}

// pickerTogglePin pins or unpins the highlighted sprint. The cursor follows the
// board it was on, which the re-sort has usually moved.
func (m *Model) pickerTogglePin() (tea.Model, tea.Cmd) {
	e, ok := m.selectedPickerBoard()
	if !ok {
		return m, nil
	}
	if e.name == "" {
		m.notice = "main is always pinned"
		return m, nil
	}
	if e.archived {
		m.notice = "archived sprints can't be pinned — press u to unarchive"
		return m, nil
	}
	pinned, err := store.TogglePin(e.name)
	if err != nil {
		m.err = err
		m.notice = err.Error()
		return m, nil
	}
	m.reloadPickerEntriesOn(e.name)
	if pinned {
		m.notice = fmt.Sprintf("pinned %q", e.name)
	} else {
		m.notice = fmt.Sprintf("unpinned %q", e.name)
	}
	return m, nil
}

// pickerReorderPin moves the highlighted pinned sprint up or down within the
// pinned block. Main holds the top slot, and the block's lower edge is the
// divider — J on the last pinned sprint does nothing rather than unpinning it.
func (m *Model) pickerReorderPin(dir int) (tea.Model, tea.Cmd) {
	e, ok := m.selectedPickerBoard()
	if !ok {
		return m, nil
	}
	if e.name == "" {
		m.notice = "main stays at the top"
		return m, nil
	}
	if !e.pinned {
		m.notice = "only pinned boards can be reordered — press p to pin"
		return m, nil
	}
	// MovePin clamps silently at both ends, so say which edge was hit rather
	// than leaving the keypress looking broken.
	if atEdge, edge := m.pinnedBlockEdge(m.pickerIdx, dir); atEdge {
		m.notice = edge
		return m, nil
	}
	if err := store.MovePin(e.name, dir); err != nil {
		m.err = err
		m.notice = err.Error()
		return m, nil
	}
	m.reloadPickerEntriesOn(e.name)
	return m, nil
}

// pinnedBlockEdge reports whether moving the board at idx by dir would leave the
// pinned block, and what to tell the user if so.
func (m *Model) pinnedBlockEdge(idx, dir int) (bool, string) {
	if dir < 0 {
		// Index 0 is main, which always holds the top slot.
		if idx <= 1 {
			return true, "main stays at the top"
		}
		return false, ""
	}
	last := idx
	for i, e := range m.pickerBoards {
		if e.pinned && e.name != "" {
			last = i
		}
	}
	if idx >= last {
		return true, "already last in the pinned block — press p to unpin"
	}
	return false, ""
}

// renameFocus values — also the tab cycle order.
const (
	renameFocusName = iota
	renameFocusPrefix
)

// startPickerRename opens the two-field rename form on the highlighted sprint:
// its name, and the prefix its ticket ids carry.
func (m *Model) startPickerRename() (tea.Model, tea.Cmd) {
	e, ok := m.selectedPickerBoard()
	if !ok {
		return m, nil
	}
	if e.name == "" {
		m.notice = "main board can't be renamed"
		return m, nil
	}
	if e.archived {
		m.notice = "archived sprints are read-only — press u to unarchive"
		return m, nil
	}

	nameIn := textinput.New()
	nameIn.Prompt = ""
	nameIn.CharLimit = 64
	nameIn.Width = 28 // a 64-char name would otherwise push past the popup edge
	nameIn.SetValue(e.name)
	nameIn.CursorEnd()
	nameIn.Focus()
	m.renameName = nameIn

	prefixIn := textinput.New()
	prefixIn.Prompt = ""
	prefixIn.CharLimit = 4
	prefixIn.Width = 4
	prefixIn.SetValue(e.prefix)
	prefixIn.CursorEnd()
	prefixIn.Blur()
	m.renamePrefix = prefixIn

	m.renameFocus = renameFocusName
	m.renameFromPrefix = e.prefix
	m.renameTarget = e.name
	return m, textinput.Blink
}

func (m *Model) updatePickerRename(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	// The fields swallow runes, so j/k can't move between them — tab and the
	// arrow keys do.
	switch msg.Type {
	case tea.KeyEsc:
		m.cancelPickerRename()
		return m, nil
	case tea.KeyEnter:
		return m.submitPickerRename()
	case tea.KeyTab, tea.KeyDown:
		m.focusRenameField(1)
		return m, nil
	case tea.KeyShiftTab, tea.KeyUp:
		m.focusRenameField(-1)
		return m, nil
	}

	var cmd tea.Cmd
	if m.renameFocus == renameFocusName {
		m.renameName, cmd = m.renameName.Update(msg)
	} else {
		m.renamePrefix, cmd = m.renamePrefix.Update(msg)
	}
	return m, cmd
}

func (m *Model) focusRenameField(dir int) {
	m.setRenameFocus((m.renameFocus + dir + 2) % 2)
}

// setRenameFocus moves the keyboard to one field of the rename form. The
// highlight and the focused widget have to move together: a blurred textinput
// drops every key it is handed, so setting the index alone leaves the form
// looking focused while what you type goes nowhere.
func (m *Model) setRenameFocus(idx int) {
	m.renameFocus = idx
	if m.renameFocus == renameFocusName {
		m.renameName.Focus()
		m.renamePrefix.Blur()
		return
	}
	m.renamePrefix.Focus()
	m.renameName.Blur()
}

func (m *Model) cancelPickerRename() {
	m.renameTarget = ""
	m.renameName.Blur()
	m.renamePrefix.Blur()
}

// submitPickerRename applies the form. A rejected change leaves the form open on
// the values the user typed, with the reason in the footer — retyping a name
// from scratch to fix one character would be the worse trade.
func (m *Model) submitPickerRename() (tea.Model, tea.Cmd) {
	target := m.renameTarget
	newName := strings.TrimSpace(m.renameName.Value())
	newPrefix := strings.TrimSpace(m.renamePrefix.Value())
	if newName == "" {
		newName = target
	}

	// What actually changed, decided before the write so the notice can't claim
	// more than happened. An empty prefix field means "leave it alone", which is
	// UpdateSprint's own reading of it.
	renamed := newName != target
	retagged := newPrefix != "" && !strings.EqualFold(newPrefix, m.renameFromPrefix)

	if err := store.UpdateSprint(target, newName, newPrefix); err != nil {
		m.notice = err.Error()
		return m, nil
	}
	m.cancelPickerRename()

	// Only re-point the live model when its directory actually moved —
	// switchBoard resets column focus, cursors and scroll, so doing it on an
	// unchanged submit would throw away the user's place on the board.
	if renamed && m.sprintName == target {
		if err := m.switchBoard(newName); err != nil {
			m.err = err
			return m, nil
		}
	}
	m.reloadPickerEntriesOn(newName)
	switch {
	case renamed && retagged:
		m.notice = fmt.Sprintf("renamed %q to %q, ids now carry %s", target, newName, strings.ToUpper(newPrefix))
	case renamed:
		m.notice = fmt.Sprintf("renamed %q to %q", target, newName)
	case retagged:
		m.notice = fmt.Sprintf("%q ids now carry %s", newName, strings.ToUpper(newPrefix))
	default:
		m.notice = "nothing changed"
	}
	return m, nil
}

// reloadPickerEntriesOn refreshes the list and parks the cursor on a named
// board, wherever the new ordering put it.
func (m *Model) reloadPickerEntriesOn(name string) {
	if !m.loadPickerData() {
		return
	}
	m.pickerIdx = 0
	for i, e := range m.pickerBoards {
		if e.name == name {
			m.pickerIdx = i
			break
		}
	}
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
	e, ok := m.selectedPickerBoard()
	if !ok {
		return m, nil
	}
	if e.name == "" {
		m.notice = "main board can't be archived"
		return m, nil
	}
	if e.archived {
		m.notice = "already archived — press u to unarchive"
		return m, nil
	}
	if e.pinned {
		m.notice = "pinned — press p to unpin before archiving"
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
	e, ok := m.selectedPickerBoard()
	if !ok {
		return m, nil
	}
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
	newStore, err := boardStore(sprintName)
	if err != nil {
		return err
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
	// A filter belongs to the board it was typed on — and so does the other
	// one. Carrying the board's across would land you on a new board showing
	// three of its cards with no visible reason; carrying the archive's across
	// would filter the next archive by a query typed for the last one, with an
	// owners map still relative to the board it was built from, so local rows
	// would read as borrowed. Resetting only the filter whose surface happens
	// to be on screen left whichever one was not.
	//
	// jumpToForeign and jumpToForeignArchive are the deliberate exceptions and
	// re-apply their own query after calling this.
	m.search.reset()
	m.archiveSearch.reset()
	m.archiveEntries = nil
	m.clampCursors()

	if info, err := os.Stat(newStore.BoardPath()); err == nil {
		m.lastModTime = info.ModTime()
	} else {
		m.lastModTime = time.Time{}
	}
	return nil
}

func (m *Model) viewPicker() string {
	rowCount := len(m.pickerLines())
	if rowCount < 1 {
		rowCount = 1
	}
	if m.confirmArchive != "" {
		rowCount += 2 // blank + confirm prompt
	}
	if m.renameTarget != "" {
		rowCount += 4 // blank + heading + name + prefix
	}
	popupWidth, popupHeight := m.listPopupSize(m.pickerWidestRow, rowCount)

	backdrop := m.popupBackdrop(m.popupReturnView)
	m.resetZones()
	origin := m.popupOrigin(popupWidth, popupHeight)
	popup := m.renderPickerPopup(popupWidth, popupHeight, origin)
	return overlayAt(backdrop, popup, origin.x, origin.y)
}

// listRowWidth is one row of a name-and-counts list: the name, a two-column
// gap, and the right-aligned per-status counts.
func listRowWidth(name string, counts map[model.Status]int) int {
	return lipgloss.Width(name) + 2 + lipgloss.Width(formatCounts(counts))
}

// widestPickerRow measures the widest board row (name + counts).
func widestPickerRow(entries []pickerEntry) int {
	widest := 0
	for _, e := range entries {
		if w := listRowWidth(boardDisplayName(e.name), e.counts); w > widest {
			widest = w
		}
	}
	return widest
}

// listPopupSize is the shape of every name-and-counts popup — the board list
// and the tag list are the same object at two different moments, so they size
// by one rule rather than drifting apart by a dozen columns and half a screen.
//
// widestRow comes from listRowWidth; +6 is the marker (2), the outer border (2)
// and the inner padding (2). The bounds keep a one-board list from rendering as
// a sliver and a long board name from filling the terminal.
func (m *Model) listPopupSize(widestRow, rowCount int) (width, height int) {
	const (
		minWidth = 40
		maxWidth = 72
	)
	width = widestRow + 6
	width = min(max(width, minWidth), maxWidth)
	if width > m.width-4 {
		width = m.width - 4
	}
	if width < 30 {
		width = 30
	}

	height = max(rowCount, 1) + 2
	if height > m.height-4 {
		height = m.height - 4
	}
	if height < 6 {
		height = 6
	}
	return width, height
}

func (m *Model) renderPickerPopup(width, height int, origin point) string {
	innerWidth := width - 4
	if innerWidth < 10 {
		innerWidth = 10
	}

	lines := m.pickerLines()
	var rows []string
	for _, l := range lines {
		if l.boardIdx < 0 {
			rows = append(rows, dimStyle.Render(strings.Repeat("─", innerWidth)))
			continue
		}
		e := m.pickerBoards[l.boardIdx]
		rows = append(rows, renderPickerRow(e, innerWidth, l.boardIdx == m.pickerIdx, e.name == m.sprintName))
	}
	rowOffset := 0
	if m.confirmArchive != "" {
		prompt := fmt.Sprintf("archive %q? [y/N]", m.confirmArchive)
		rows = append(rows, "", lipgloss.NewStyle().Foreground(peach).Bold(true).Render(prompt))
	}
	if m.renameTarget != "" {
		rows = append(rows, m.renameFormRows(innerWidth)...)
	}

	visible := height - 2
	if visible < 1 {
		visible = 1
	}
	if len(rows) > visible {
		start := pickerLineOf(lines, m.pickerIdx) - visible/2
		if m.renameTarget != "" || m.confirmArchive != "" {
			// Both live at the bottom of the list and both want keys: anchor
			// there so a short terminal never hides the thing being typed into.
			start = len(rows) - visible
		}
		if start < 0 {
			start = 0
		}
		if start+visible > len(rows) {
			start = len(rows) - visible
		}
		rows = rows[start : start+visible]
		rowOffset = start
	}

	// The rename form's two inputs are the last rows in the list, and the window
	// above is anchored to the bottom whenever it is open, so they are always on
	// screen to be clicked.
	if m.renameTarget != "" && len(rows) >= 2 {
		for i, focus := range []int{renameFocusName, renameFocusPrefix} {
			m.addZone(hitZone{
				kind: zoneRenameField,
				x:    origin.x + 2,
				y:    origin.y + 1 + len(rows) - 2 + i,
				w:    innerWidth,
				h:    1,
				idx:  focus,
			})
		}
	}

	// Board rows are clickable; the divider and the confirm prompt rows
	// (appended after the boards) are not. While the rename form or the archive
	// confirm owns the keyboard, no row is: moving the highlight would leave it
	// pointing at a board the form isn't editing, and a click would close the
	// popup and silently discard what was typed.
	rowsAreLive := m.renameTarget == "" && m.confirmArchive == ""
	for i := 0; rowsAreLive && i < len(rows); i++ {
		idx := rowOffset + i
		if idx >= len(lines) {
			break
		}
		if lines[idx].boardIdx < 0 {
			continue
		}
		m.addZone(hitZone{
			kind: zonePickerRow,
			x:    origin.x + 2,
			y:    origin.y + 1 + i,
			w:    innerWidth,
			h:    1,
			idx:  lines[idx].boardIdx,
		})
	}

	content := lipgloss.NewStyle().PaddingLeft(1).Render(strings.Join(rows, "\n"))
	return renderPanel("Boards", content, width, height, green, true)
}

// renameFormRows renders the rename form under the board list: a heading, then
// the name and prefix fields. The focused field's label carries the accent —
// styling the inputs themselves mangles the cursor textinput draws.
func (m *Model) renameFormRows(width int) []string {
	const labelWidth = 8
	label := func(text string, focused bool) string {
		style := lipgloss.NewStyle().Foreground(midGray).Width(labelWidth)
		if focused {
			style = style.Foreground(green).Bold(true)
		}
		return style.Render(text)
	}
	// No sprint name in the heading — the name field below is already showing
	// it, and a 64-char one would push the popup past its width cap.
	heading := lipgloss.NewStyle().Foreground(peach).Bold(true).Render("rename sprint")

	return []string{
		"",
		heading,
		label("name", m.renameFocus == renameFocusName) + m.renameName.View(),
		label("prefix", m.renameFocus == renameFocusPrefix) + m.renamePrefix.View() + m.renameIDHint(),
	}
}

// renameIDHint spells out what a changed prefix does to existing ids — the part
// that isn't obvious from typing two letters into a field. Silent when the
// prefix is untouched.
func (m *Model) renameIDHint() string {
	next := strings.ToUpper(strings.TrimSpace(m.renamePrefix.Value()))
	if next == "" || next == strings.ToUpper(m.renameFromPrefix) {
		return ""
	}
	return dimStyle.Render(fmt.Sprintf("  %s1 → %s1", m.renameFromPrefix, next))
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
		// search and settings lead: they are the two the board cannot teach you
		// any other way, and fitHints drops from the end. The rest are either
		// guessable or already on a card in front of you.
		hints := fmt.Sprintf("h/l nav | j/k select | %s search | %s tags | %s info | %s settings | %s/%s move | %s move | %s add | %s archive | %s quit",
			hk("board.search"), hk("board.tags"), hk("board.info"), hk("board.settings"), hk("card.moveLeft"), hk("card.moveRight"),
			hk("card.move"), hk("card.add"), hk("card.archive"), "q")
		if m.searchActive() {
			// Only the board view frees esc for this — elsewhere it still
			// means "back", and shadowing that to clear a filter would be a
			// worse trade than walking out to the board first.
			return "esc clear | " + hints
		}
		return hints
	case infoView:
		if m.infoEditing {
			return "enter save | esc discard | shift+enter newline"
		}
		return "enter edit | esc close"
	case emojiView:
		if m.emojiPick.filtering {
			return "type filter | ctrl+n/p move | enter keep filter | esc clear"
		}
		if m.emojiPick.filter.Value() != "" {
			return "hjkl move | / edit filter | enter pick | esc clear filter"
		}
		return "hjkl move | / filter | enter pick | esc close"
	case settingsView:
		return "j/k select | h/l section | enter change | esc close"
	case tagView:
		return "j/k select | enter filter by tag | esc close"
	case moveView:
		if m.move.pane == movePaneBoards {
			return "j/k board | enter/l columns | esc close"
		}
		return "j/k column | h boards | enter move | esc close"
	case pickerView:
		if m.confirmArchive != "" {
			return fmt.Sprintf("archive %q? y / n", m.confirmArchive)
		}
		if m.renameTarget != "" {
			return "tab/↑↓ fields | enter apply | esc cancel"
		}
		if m.pickerShowArchived {
			return fmt.Sprintf("j/k select | enter switch | %s info | %s tags | %s rename | %s pin | %s/%s reorder | %s archive | %s unarchive | %s hide archived | esc/%s close",
				hk("board.info"), hk("board.tags"), hk("board.rename"), hk("board.pin"), hk("card.reorderUp"), hk("card.reorderDown"),
				hk("card.archive"), hk("board.unarchive"), hk("board.archiveView"), hk("board.picker"))
		}
		return fmt.Sprintf("j/k select | enter switch | %s info | %s tags | %s rename | %s pin | %s/%s reorder | %s archive | %s show archived | esc/%s close",
			hk("board.info"), hk("board.tags"), hk("board.rename"), hk("board.pin"), hk("card.reorderUp"), hk("card.reorderDown"),
			hk("card.archive"), hk("board.archiveView"), hk("board.picker"))
	case archiveView:
		if m.archiveSearch.active() {
			return fmt.Sprintf("esc clear | j/k nav | %s unarchive | %s copy id | %s back",
				hk("board.unarchive"), hk("card.copy"), hk("board.archiveView"))
		}
		// The way out goes last: fitHints protects the final hint, and in a
		// panel with its own close key that is the one that must survive.
		return fmt.Sprintf("j/k nav | %s search | %s unarchive | %s copy id | %s/esc back",
			hk("board.search"), hk("board.unarchive"), hk("card.copy"), hk("board.archiveView"))
	case splitView:
		if m.splitFocus == 0 {
			return fmt.Sprintf("j/k select | %s edit | %s search | %s/%s move | %s move | %s archive | %s back | %s settings | q quit",
				hk("board.panelNext"), hk("board.search"), hk("card.moveLeft"), hk("card.moveRight"), hk("card.move"),
				hk("card.archive"), hk("board.unzoom"), hk("board.settings"))
		}
		if m.editDesc.Focused() {
			return "enter save | shift+enter new line | esc save"
		}
		if m.editTitle.Focused() {
			return "enter save | esc save"
		}
		switch m.editField {
		case 0:
			return fmt.Sprintf("h/l meta | j/k fields | enter edit | %s/%s move | %s move to | %s archive | q quit",
				hk("card.moveLeft"), hk("card.moveRight"), hk("card.move"), hk("card.archive"))
		case 1, 2:
			return fmt.Sprintf("j/k fields | enter/%s edit | %s/%s move | h list | q quit",
				hk("card.edit"), hk("card.moveLeft"), hk("card.moveRight"))
		}
	case columnView:
		return fmt.Sprintf("j/k select | %s search | %s/%s move | %s move to | %s archive | enter detail | %s back | %s add | %s settings | q quit",
			hk("board.search"), hk("card.moveLeft"), hk("card.moveRight"), hk("card.move"), hk("card.archive"),
			hk("board.unzoom"), hk("card.add"), hk("board.settings"))
	case detailView:
		if m.editDesc.Focused() {
			return "enter save | shift+enter new line | esc save"
		}
		if m.editTitle.Focused() {
			return "enter save | esc save"
		}
		switch m.editField {
		case 0:
			return fmt.Sprintf("h/l meta | j/k fields | enter edit | %s/%s move | %s move to | %s delete | %s back | q quit",
				hk("card.moveLeft"), hk("card.moveRight"), hk("card.move"), hk("card.delete"), hk("board.unzoom"))
		case 1, 2:
			return fmt.Sprintf("j/k fields | enter/%s edit | esc back | q quit", hk("card.edit"))
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

func (m *Model) viewBoard() string {
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
	tickets := m.visibleTickets(status)
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
	// The footer holding focus is the same case: the column stays framed as
	// the one you'd come back to, but nothing in it is selected.
	cursor := -1
	if focused && !m.footerHasFocus() {
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
	// The condensed layout has only the title line, so a borrowed card's board
	// has to share it — the badge is what stops a foreign card reading as one
	// of this board's own.
	badge := m.boardBadge(t.ID)
	// The badge is unbounded — sprint names are valid up to 64 characters —
	// and prepending it raw drove maxTitle to its floor and overran the row
	// anyway, leaving the panel rather than ansi.Truncate to do the cutting,
	// so the row lost its title with no ellipsis to say anything was missing.
	// Half the row is the badge's ceiling: enough to tell boards apart, never
	// enough to leave nothing to read.
	if maxBadge := width / 2; lipgloss.Width(badge) > maxBadge {
		badge = ansi.Truncate(badge, maxBadge, "…")
	}

	title := t.Title
	maxTitle := width - 1 - lipgloss.Width(badge)
	if selected {
		maxTitle = width - 3 - lipgloss.Width(badge)
	}
	if t.AssignedTo != "" && selected {
		maxTitle -= 2
	}
	if maxTitle < 3 {
		maxTitle = 3
	}
	// Measure and cut by display width, not bytes: a byte slice through a
	// multi-byte rune renders as a replacement character, and the badge makes
	// the cut land in new places.
	if lipgloss.Width(title) > maxTitle {
		title = ansi.Truncate(title, maxTitle, "…")
	}

	if selected {
		marker := lipgloss.NewStyle().Foreground(accentColor).Bold(true).Render(" * ")
		titleRendered := foreignBoardStyle.Render(badge) +
			lipgloss.NewStyle().Bold(true).Foreground(white).Render(title)
		line := marker + titleRendered
		if t.AssignedTo != "" {
			line += " " + assigneeStyle.Render("●")
		}
		return line
	}

	return lipgloss.NewStyle().PaddingLeft(1).Render(
		foreignBoardStyle.Render(badge) + lipgloss.NewStyle().Foreground(softWhite).Render(title))
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
	tickets := m.visibleTickets(status)
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
	// The panel zone is registered before the field zones inside it, so a click
	// on a field wins over the panel it sits in.
	m.addZone(hitZone{kind: zoneField, x: origin.x, y: origin.y, w: width, h: 3, idx: 0})
	metaContent := m.renderMeta(t, focused && m.editField == 0, false, point{x: origin.x + 1, y: origin.y + 1})
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
	tickets := m.visibleTickets(status)
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

		// This view builds its own rows rather than going through
		// renderTicketLine or cardMetaLine, so it needs the borrowed-card
		// badge of its own — without it a foreign card here is
		// indistinguishable from one of this board's.
		badge := m.boardBadge(t.ID)

		maxTitle := innerWidth - 3 - len([]rune(suffix)) - len([]rune(badge))
		if maxTitle < 3 {
			maxTitle = 3
		}
		if len([]rune(titleText)) > maxTitle {
			titleText = string([]rune(titleText)[:maxTitle-1]) + "…"
		}

		line := marker + foreignBoardStyle.Render(badge) + tStyle.Render(titleText)
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
	m.addZone(hitZone{kind: zoneField, x: 0, y: 0, w: innerWidth + 2, h: 3, idx: 0})
	metaContent := m.renderMeta(t, m.editField == 0, true, point{x: 1, y: 1})
	metaPanel := renderPanel("Info", metaContent, innerWidth+2, 3, metaBorderColor, m.editField == 0)

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

// renderMeta draws the Info panel's one-line metadata bar and registers a click
// zone over each field, so the mouse can pick status / assignee / tags directly
// instead of walking to them with h and l.
//
// navigable means the panel currently holds the keyboard: the selected field
// gets the highlight, and empty assign/tag slots show as dim "+assign" / "+tag"
// prompts you can move onto to fill in. When it doesn't, empty fields are
// hidden entirely to keep the bar uncluttered — and get no zone, since there is
// nothing drawn to click.
//
// origin is where the panel's content starts on screen: one row below its top
// border and one column in from its left one.
func (m *Model) renderMeta(t *model.Ticket, navigable, showCreated bool, origin point) string {
	status := model.ColumnOrder[m.focusedCol]
	color := columnColor(status)

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
		{statusDisplay[t.Status], lipgloss.NewStyle().Foreground(color).Bold(true), false},
		{assignText, assigneeStyle, assignEmpty},
		{tagsText, tagStyle, tagsEmpty},
	}

	const gap = 2 // the "  " the parts are joined with
	var parts []string
	x := origin.x
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
		width := lipgloss.Width(rendered)
		// Only while the panel is navigable, because only then is the layout
		// stable. Focusing it fills the empty slots with their `+assign` /
		// `+tag` prompts, which shifts every field right of them: a zone
		// registered before that reflow would put the second click of a
		// two-click pair on the field next door. Unfocused, the click lands on
		// the panel itself and focuses it, which is the same first step every
		// other panel takes.
		if navigable {
			m.addZone(hitZone{kind: zoneMetaField, x: x, y: origin.y, w: width, h: 1, idx: i})
		}
		x += width + gap
		parts = append(parts, rendered)
	}
	parts = append(parts, m.renderTicketID(*t, dim))
	if showCreated {
		parts = append(parts, dim.Render(t.CreatedAt.Format("2006-01-02 15:04")))
	}

	return strings.Join(parts, "  ")
}
