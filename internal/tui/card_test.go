package tui

import (
	"strings"
	"testing"

	"github.com/LeonY117/kanban-tui/internal/model"
	"github.com/LeonY117/kanban-tui/internal/store"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
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

func TestSelectionMarkers(t *testing.T) {
	m := testModel(t, "first", "second")
	m.layout = layoutCard
	block := m.renderTicketList(m.board.Tickets, 1, 30, 20, 0, blue, point{})
	lines := strings.Split(block, "\n")

	// 2 tickets × (rule + title + meta) + the closing rule.
	if len(lines) != 7 {
		t.Fatalf("stack has %d lines, want 7:\n%s", len(lines), block)
	}
	for i, l := range lines {
		if w := lipgloss.Width(l); w != 30 {
			t.Errorf("line %d width = %d, want 30 (%q)", i, w, l)
		}
	}
	if !strings.Contains(lines[1], "▌") || !strings.Contains(lines[2], "▌") {
		t.Errorf("selected block should carry a left bar: %q / %q", lines[1], lines[2])
	}
	if strings.Contains(lines[4], "▌") {
		t.Errorf("unselected block should not: %q", lines[4])
	}
	if !strings.Contains(lines[0], "━") || !strings.Contains(lines[3], "━") {
		t.Errorf("selected block should be bracketed by heavy rules: %q / %q", lines[0], lines[3])
	}
	if strings.Contains(lines[6], "━") {
		t.Errorf("the closing rule should stay light: %q", lines[6])
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

func TestFitScrollStart(t *testing.T) {
	heights := []int{4, 4, 4, 4, 4}
	if got := fitScrollStart(heights, 0, 20); got != 0 {
		t.Errorf("everything fits: start = %d, want 0", got)
	}
	if got := fitScrollStart(heights, 4, 12); got != 2 {
		t.Errorf("cursor at 4 with room for 3: start = %d, want 2", got)
	}
	if got := fitScrollStart(heights, 4, 4); got != 4 {
		t.Errorf("room for one card: start = %d, want 4", got)
	}
}

// testModel builds a Model over a throwaway board directory.
func testModel(t *testing.T, titles ...string) *Model {
	t.Helper()
	s := store.New(t.TempDir())
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

func TestWheelScrollsOneTicketAtATime(t *testing.T) {
	m := testModel(t, "first", "second", "third")
	m.View()

	m.mouseScroll(mouseAt(40, 2), 1)
	if m.cursors[1] != 1 {
		t.Errorf("one wheel notch moved the cursor to %d, want 1", m.cursors[1])
	}
	m.mouseScroll(mouseAt(40, 2), -1)
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
