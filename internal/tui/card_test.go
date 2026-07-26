package tui

import (
	"fmt"
	"strings"
	"testing"

	"github.com/LeonY117/kanban-tui/internal/model"
	"github.com/LeonY117/kanban-tui/internal/store"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/muesli/termenv"
)

func TestCardContentShape(t *testing.T) {
	ticket := model.Ticket{
		ShortID:    "abc123",
		Title:      "Fix the login bug",
		Tags:       []string{"backend"},
		AssignedTo: "claude",
		Status:     model.StatusTodo,
	}
	m := &Model{layout: layoutCard}

	lines := m.cardContent(ticket, true, 26, blue)
	if len(lines) != 2 {
		t.Fatalf("card body has %d lines, want 2 (title + meta)", len(lines))
	}
	if !strings.Contains(lines[0], "Fix the login bug") {
		t.Errorf("title line missing the title: %q", lines[0])
	}
	if !strings.Contains(lines[1], "abc123") || !strings.Contains(lines[1], "#backend") {
		t.Errorf("meta line missing id/tags: %q", lines[1])
	}
}

func TestCardContentWrapsLongTitle(t *testing.T) {
	ticket := model.Ticket{
		ShortID: "abc123",
		Title:   "A very long ticket title that will certainly not fit on one line of a narrow column",
	}
	m := &Model{layout: layoutCard}

	lines := m.cardContent(ticket, false, 20, blue)
	if len(lines) != 3 {
		t.Fatalf("card body has %d lines, want 3 (title wrapped to 2 + meta)", len(lines))
	}
	if !strings.Contains(lines[1], "…") {
		t.Errorf("truncated title line should end in an ellipsis: %q", lines[1])
	}
}

func TestLargeLayoutShowsDescriptionPreview(t *testing.T) {
	ticket := model.Ticket{
		ShortID:     "abc123",
		Title:       "Short title",
		Description: "first line of the description\nsecond line",
	}
	m := &Model{layout: layoutLarge}

	lines := m.cardContent(ticket, false, 40, blue)
	if len(lines) != 4 {
		t.Fatalf("large body has %d lines, want 4 (title + 2 desc + meta)", len(lines))
	}
	if !strings.Contains(lines[1], "first line") {
		t.Errorf("description preview missing: %q", lines[1])
	}
}

// The selected ticket has to be unmistakable in a column of identical boxes.
// It is closed into its own rounded box, accented on all four sides, while its
// neighbours share T-junction rules.
func TestSelectionMarkers(t *testing.T) {
	defer withColor(t)()

	m := testModel(t, "first", "second", "third", "fourth")
	m.layout = layoutCard
	block := m.renderTicketList(m.board.Tickets, 1, 30, 20, 1, blue, point{})
	lines := strings.Split(block, "\n")

	for i, l := range lines {
		if w := lipgloss.Width(l); w != 30 {
			t.Errorf("line %d width = %d, want 30 (%q)", i, w, l)
		}
	}
	top, bottom := lines[3], lines[6]
	if !strings.HasPrefix(ansi.Strip(top), "╭") || !strings.HasSuffix(ansi.Strip(top), "╮") {
		t.Errorf("selection top is not rounded: %q", ansi.Strip(top))
	}
	if !strings.HasPrefix(ansi.Strip(bottom), "╰") || !strings.HasSuffix(ansi.Strip(bottom), "╯") {
		t.Errorf("selection bottom is not rounded: %q", ansi.Strip(bottom))
	}
	if !strings.Contains(block, "├") {
		t.Errorf("ordinary neighbours should share a junction rule:\n%s", ansi.Strip(block))
	}

	// Colour is what carries the selection here, and lipgloss strips it under
	// `go test` unless a profile is forced — so assert on the rendered escapes,
	// not just the glyphs.
	accent, dim := colorOpener(blue), colorOpener(dimGray)
	for _, want := range []struct {
		name string
		line string
	}{{"top edge", top}, {"bottom edge", bottom}} {
		if !strings.HasPrefix(want.line, accent) {
			t.Errorf("selection %s is not accented: %q", want.name, want.line)
		}
	}
	if !strings.HasPrefix(lines[4], accent) {
		t.Errorf("selection side is not accented: %q", lines[4])
	}
	if !strings.HasPrefix(lines[1], dim) {
		t.Errorf("an unselected row should stay dim: %q", lines[1])
	}
}

// Heavy box-drawing glyphs overhang their cell in many terminal fonts, so a
// heavy rule visibly spills past the sides of the box it belongs to. The frame
// is light throughout and lets colour mark the selection.
func TestFrameStaysLight(t *testing.T) {
	m := testModel(t, "first", "second", "third")
	for _, layout := range []ticketLayout{layoutCard, layoutLarge, layoutCondensed} {
		m.layout = layout
		m.scrollStart = [5]int{}
		block := m.renderTicketList(m.board.Tickets, 1, 30, 20, 1, blue, point{})
		for _, heavy := range []string{"━", "┃", "┏", "┓", "┗", "┛", "┝", "┥", "┢", "┪", "┡", "┩"} {
			if strings.Contains(block, heavy) {
				t.Errorf("%s layout used the heavy glyph %q:\n%s", layout.label(), heavy, ansi.Strip(block))
			}
		}
	}
}

func TestLargeLayoutBoxesEachTicket(t *testing.T) {
	m := testModel(t, "first", "second")
	m.layout = layoutLarge
	block := m.renderTicketList(m.board.Tickets, 1, 30, 20, 0, blue, point{})
	lines := strings.Split(block, "\n")

	// 2 tickets × (2 borders + title + meta); no descriptions on these.
	if len(lines) != 8 {
		t.Fatalf("stack has %d lines, want 8:\n%s", len(lines), block)
	}
	if !strings.Contains(lines[3], "╰") || !strings.Contains(lines[4], "╭") {
		t.Errorf("large tickets should each get a box: %q / %q", lines[3], lines[4])
	}
	for i, l := range lines {
		if w := lipgloss.Width(l); w != 30 {
			t.Errorf("line %d width = %d, want 30 (%q)", i, w, l)
		}
	}
}

// colorOpener is the escape sequence lipgloss emits for a foreground colour,
// so a test can ask what colour a rendered line opens in without caring how
// many cells the span covers.
func colorOpener(c lipgloss.Color) string {
	rendered := lipgloss.NewStyle().Foreground(c).Render("X")
	return strings.SplitN(rendered, "X", 2)[0]
}

// withColor forces a colour profile for the duration of a test: lipgloss
// detects a non-tty under `go test` and strips every escape, which makes any
// assertion about colour pass vacuously.
func withColor(t *testing.T) func() {
	t.Helper()
	prev := lipgloss.ColorProfile()
	lipgloss.SetColorProfile(termenv.TrueColor)
	return func() { lipgloss.SetColorProfile(prev) }
}

// An unfocused column has no selection, so nothing in it may render accented.
func TestUnfocusedColumnHasNoSelection(t *testing.T) {
	defer withColor(t)()

	m := testModel(t, "first", "second", "third")
	block := m.renderTicketList(m.board.Tickets, 2, 30, 20, -1, blue, point{})
	// cursor < 0 once meant the first row read its own top rule as "the block
	// above me is selected" and drew it accented — a bright line across the top
	// of every unfocused column.
	// tableRule colours the whole ╭────╮ span in one escape, so looking for a
	// rendered single glyph finds nothing whether or not the rule is accented.
	// Check what colour each line opens in instead.
	accent := colorOpener(blue)
	for i, line := range strings.Split(block, "\n") {
		if strings.HasPrefix(line, accent) {
			t.Errorf("line %d of an unfocused column is accented: %q", i, line)
		}
	}
}

func TestOverflowingListFillsThePanel(t *testing.T) {
	m := testModel(t, "first", "second", "third", "fourth", "fifth")

	for _, layout := range []ticketLayout{layoutCard, layoutLarge} {
		for _, height := range []int{7, 8, 9, 10} {
			m.layout = layout
			block := m.renderTicketList(m.board.Tickets, 1, 30, height, 0, blue, point{})
			if got := len(strings.Split(block, "\n")); got != height {
				t.Errorf("%s at height %d rendered %d lines — the tail should be cropped, not blank:\n%s",
					layout.label(), height, got, block)
			}
		}
	}
}

func TestSelectionNeverShiftsTheList(t *testing.T) {
	m := testModel(t, "first", "second", "third")
	m.layout = layoutCard

	var heights []int
	for cursor := 0; cursor < 3; cursor++ {
		block := m.renderTicketList(m.board.Tickets, 1, 30, 20, cursor, blue, point{})
		heights = append(heights, len(strings.Split(block, "\n")))
	}
	for i, h := range heights {
		if h != heights[0] {
			t.Fatalf("moving the cursor to %d changed the list height: %d vs %d", i, h, heights[0])
		}
	}
}

func TestScrollWindowIsSticky(t *testing.T) {
	m := &Model{}
	costs := make([]int, 10) // ten single-line tickets
	for i := range costs {
		costs[i] = 1
	}
	const avail = 5

	// Going down: the cursor travels inside the window, then pushes it.
	for cursor := 0; cursor < 5; cursor++ {
		if got := m.scrollWindow(1, costs, cursor, avail); got != 0 {
			t.Fatalf("cursor %d scrolled to %d before reaching the bottom edge", cursor, got)
		}
	}
	if got := m.scrollWindow(1, costs, 5, avail); got != 1 {
		t.Fatalf("cursor past the bottom edge: start = %d, want 1", got)
	}
	if got := m.scrollWindow(1, costs, 6, avail); got != 2 {
		t.Fatalf("cursor past the bottom edge: start = %d, want 2", got)
	}

	// Coming back up: the window holds still until the cursor reaches its
	// top edge, mirroring the way down.
	for _, cursor := range []int{5, 4, 3, 2} {
		if got := m.scrollWindow(1, costs, cursor, avail); got != 2 {
			t.Fatalf("cursor %d moved the window to %d — it should hold at 2 until the top edge", cursor, got)
		}
	}
	if got := m.scrollWindow(1, costs, 1, avail); got != 1 {
		t.Fatalf("cursor above the top edge: start = %d, want 1", got)
	}
	if got := m.scrollWindow(1, costs, 0, avail); got != 0 {
		t.Fatalf("cursor at the list head: start = %d, want 0", got)
	}
}

func TestScrollWindowLeavesNoDeadSpace(t *testing.T) {
	m := &Model{}
	m.scrollStart[1] = 4 // as if the column had been scrolled, then emptied out
	costs := []int{1, 1, 1}

	if got := m.scrollWindow(1, costs, -1, 5); got != 0 {
		t.Errorf("a short list should pull the window back to the top, got %d", got)
	}
}

func TestScrollWindowRemembersUnfocusedColumns(t *testing.T) {
	m := &Model{}
	costs := make([]int, 10)
	for i := range costs {
		costs[i] = 1
	}
	m.scrollWindow(1, costs, 7, 5) // scrolled while focused
	before := m.scrollStart[1]

	if got := m.scrollWindow(1, costs, -1, 5); got != before {
		t.Errorf("unfocused column moved from %d to %d", before, got)
	}
}

// testModel builds a Model over a throwaway board directory. KANBAN_FILE
// redirects the whole root — boards, archive and id counters — into it.
func testModel(t *testing.T, titles ...string) *Model {
	t.Helper()
	sandboxRoot(t)
	s := store.New("")
	for _, title := range titles {
		if _, err := s.Add(title, "", model.StatusTodo, nil, "", "test"); err != nil {
			t.Fatalf("add: %v", err)
		}
	}
	m, err := NewModel(s, "")
	if err != nil {
		t.Fatalf("new model: %v", err)
	}
	m.width, m.height, m.ready = 160, 40, true
	return m
}

func TestBoardClickZonesMapToTickets(t *testing.T) {
	m := testModel(t, "first", "second", "third")
	m.View()

	// Todo is column 1 of 5 across 160 cells → x ∈ [32, 64). Each ticket
	// costs 3 rows: the rule above it, plus title and meta.
	rows := map[int]int{0: 2, 1: 5, 2: 8}
	for idx, y := range rows {
		z := m.zoneAt(40, y)
		if z == nil {
			t.Fatalf("no zone at (40, %d)", y)
		}
		if z.kind != zoneTicket || z.col != 1 || z.idx != idx {
			t.Errorf("zone at (40, %d) = %+v, want ticket %d in column 1", y, *z, idx)
		}
	}

	// Below the last ticket the column itself answers.
	if z := m.zoneAt(40, 30); z == nil || z.kind != zoneColumn || z.col != 1 {
		t.Errorf("empty column space did not resolve to the column: %+v", z)
	}
}

func TestClickSelectsTicketAndColumn(t *testing.T) {
	m := testModel(t, "first", "second", "third")
	m.View()

	m.mouseClick(mouseAt(40, 8)) // third ticket in the Todo column
	if m.focusedCol != 1 || m.cursors[1] != 2 {
		t.Errorf("click selected col %d cursor %d, want col 1 cursor 2", m.focusedCol, m.cursors[1])
	}

	m.View()
	m.mouseClick(mouseAt(100, 5)) // a different (empty) column
	if m.focusedCol != 3 {
		t.Errorf("click focused col %d, want 3", m.focusedCol)
	}
}

func TestWheelNeedsSeveralNotchesPerTicket(t *testing.T) {
	m := testModel(t, "first", "second", "third")
	m.View()

	for i := 1; i < wheelNotchesPerTicket; i++ {
		m.mouseScroll(mouseAt(40, 2), 1)
		if m.cursors[1] != 0 {
			t.Fatalf("notch %d already moved the cursor to %d", i, m.cursors[1])
		}
	}
	m.mouseScroll(mouseAt(40, 2), 1)
	if m.cursors[1] != 1 {
		t.Errorf("a full bank of notches left the cursor at %d, want 1", m.cursors[1])
	}

	// Reversing responds on the next notch rather than spending the bank.
	m.mouseScroll(mouseAt(40, 2), 1)
	for i := 0; i < wheelNotchesPerTicket; i++ {
		m.mouseScroll(mouseAt(40, 2), -1)
	}
	if m.cursors[1] != 0 {
		t.Errorf("wheel back up left the cursor at %d, want 0", m.cursors[1])
	}
}

func TestDescriptionScrollClampsToContent(t *testing.T) {
	m := testModel(t, "first")
	// Twenty lines of description in a panel that shows far fewer.
	m.descScroll = 0
	body := strings.Repeat("line\n", 20)
	m.renderDescBody(body, 40, 5)
	if m.descScrollMax == 0 {
		t.Fatal("descScrollMax should be positive for an overflowing description")
	}
	m.descScroll = m.descScrollMax + 10
	m.renderDescBody(body, 40, 5)
	if m.descScroll != m.descScrollMax {
		t.Errorf("scroll = %d, want clamped to %d", m.descScroll, m.descScrollMax)
	}
}

// mouseAt builds a left-click / wheel event at a cell.
func mouseAt(x, y int) tea.MouseMsg {
	return tea.MouseMsg{X: x, Y: y, Action: tea.MouseActionPress, Button: tea.MouseButtonLeft}
}

// renderPanel must honour the width it is given whatever its title. Mouse
// zones are registered at the requested width, so a panel that renders wider
// silently shifts every zone to its right.
func TestPanelHonoursItsWidth(t *testing.T) {
	long := "[2] Doing — a title far longer than the panel it belongs to (12)"
	for _, width := range []int{8, 15, 16, 30, 80} {
		panel := renderPanel(long, "body", width, 5, blue, true)
		for i, line := range strings.Split(panel, "\n") {
			if w := lipgloss.Width(line); w != width {
				t.Errorf("width %d, line %d: rendered %d cells (%q)", width, i, w, ansi.Strip(line))
			}
		}
	}
}

// The zoomed single-column view (`+`) used to render from ticket zero with a
// hard cut at the panel height: select past the bottom and the cursor was
// simply not on screen, and no click target existed to bring it back.
func TestZoomedColumnScrollsAndRegistersZones(t *testing.T) {
	titles := make([]string, 40)
	for i := range titles {
		titles[i] = fmt.Sprintf("ticket %02d", i)
	}
	m := testModel(t, titles...)
	m.view = columnView
	m.focusedCol = 1 // Todo, where testModel puts them
	m.height, m.width = 20, 80

	last := len(m.board.ByStatus(model.StatusTodo)) - 1
	m.cursors[m.focusedCol] = last

	m.resetZones()
	view := ansi.Strip(m.View())

	if !strings.Contains(view, titles[last]) {
		t.Errorf("cursor at the last ticket is off screen:\n%s", view)
	}
	var zoned bool
	for _, z := range m.zones {
		if z.kind == zoneTicket && z.idx == last {
			zoned = true
		}
	}
	if !zoned {
		t.Error("no click target registered for the selected ticket in the zoomed view")
	}

	// And back up: the window has to follow the cursor in both directions.
	m.cursors[m.focusedCol] = 0
	m.resetZones()
	if view := ansi.Strip(m.View()); !strings.Contains(view, titles[0]) {
		t.Errorf("cursor back at the first ticket is off screen:\n%s", view)
	}
}
