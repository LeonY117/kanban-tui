package tui

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/LeonY117/kanban-tui/internal/model"
	"github.com/LeonY117/kanban-tui/internal/store"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
)

// sandboxRoot points every board path (main + sprints) at a temp dir so tests
// never touch the real ~/.kanban.
func sandboxRoot(t *testing.T) {
	t.Helper()
	t.Setenv("KANBAN_FILE", filepath.Join(t.TempDir(), "board.json"))
}

// moveKey sends one key to the open popup.
func moveKey(m *Model, k string) {
	switch k {
	case "enter":
		m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	case "esc":
		m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	default:
		m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(k)})
	}
}

func TestMovePopupOpensOnThisBoardsColumns(t *testing.T) {
	m, _ := boardWith(t, "ship it|DOING")
	withSprint(t, "demo")
	m.focusedCol = 2

	m.enterMovePopup()
	if m.view != moveView {
		t.Fatalf("view = %v, want moveView", m.view)
	}
	// Columns pane first: the common move is to another column of the board
	// you are already on, and it should not cost a keystroke to get there.
	if m.move.pane != movePaneColumns {
		t.Errorf("pane = %v, want the columns", m.move.pane)
	}
	if got := model.ColumnOrder[m.move.colIdx]; got != model.StatusDoing {
		t.Errorf("column cursor on %s, want the ticket's own column", got)
	}
	if e, _ := m.move.board(); e.name != "" {
		t.Errorf("board cursor on %q, want the board the ticket is on", e.name)
	}
	// Every active board is a destination, including the one we're standing on.
	if len(m.move.boards) != 2 {
		t.Errorf("boards = %+v, want main and demo", m.move.boards)
	}
}

func TestMovePopupSameBoardChangesColumn(t *testing.T) {
	m, main := boardWith(t, "ship it|TODO")

	m.enterMovePopup()
	moveKey(m, "j") // Todo → Doing
	moveKey(m, "enter")

	if m.view == moveView {
		t.Error("popup stayed open after the move")
	}
	board, err := main.Load()
	if err != nil {
		t.Fatal(err)
	}
	if board.Tickets[0].Status != model.StatusDoing {
		t.Errorf("status = %s, want doing", board.Tickets[0].Status)
	}
	if m.focusedCol != 2 {
		t.Errorf("focus followed to column %d, want 2", m.focusedCol)
	}
}

func TestMovePopupWalksToAnotherBoard(t *testing.T) {
	m, main := boardWith(t, "ship it|TODO")
	withSprint(t, "demo")

	m.enterMovePopup()
	moveKey(m, "h") // to the boards pane
	if m.move.pane != movePaneBoards {
		t.Fatalf("pane = %v, want the boards", m.move.pane)
	}
	moveKey(m, "j") // main → demo
	moveKey(m, "enter")
	if m.move.pane != movePaneColumns {
		t.Fatalf("pane = %v, want enter to hand over to the columns", m.move.pane)
	}
	// The columns are the destination board's now, so nothing is "current".
	if view := m.View(); strings.Contains(view, moveCurrentTag) {
		t.Error("another board's columns still carry the (current) marker")
	}

	moveKey(m, "j") // Todo → Doing
	moveKey(m, "enter")

	mainBoard, err := main.Load()
	if err != nil {
		t.Fatal(err)
	}
	if len(mainBoard.Tickets) != 0 {
		t.Errorf("main board still holds %d tickets", len(mainBoard.Tickets))
	}
	sprint, err := store.NewSprint("demo")
	if err != nil {
		t.Fatal(err)
	}
	sprintBoard, err := sprint.Load()
	if err != nil {
		t.Fatal(err)
	}
	if len(sprintBoard.Tickets) != 1 || sprintBoard.Tickets[0].Status != model.StatusDoing {
		t.Fatalf("demo holds %+v, want the ticket in doing", sprintBoard.Tickets)
	}
}

// One press, one exit, from either pane. Both lists are on screen the whole
// time, so an esc that stepped back to the boards only put a second key between
// a mistaken `m` and the board.
func TestMovePopupEscClosesFromEitherPane(t *testing.T) {
	for _, pane := range []movePane{movePaneColumns, movePaneBoards} {
		m, _ := boardWith(t, "ship it|TODO")

		m.enterMovePopup()
		m.move.pane = pane
		moveKey(m, "esc")

		if m.view != boardView {
			t.Errorf("pane %v: view = %v, want the board in one press", pane, m.view)
		}
	}
}

// The left pane lays its pins out exactly as the board picker does: pinned
// boards, a divider, then the rest.
func TestMovePopupShowsPinnedBoardsFirst(t *testing.T) {
	m, _ := boardWith(t, "ship it|TODO")
	withSprint(t, "alpha")
	withSprint(t, "beta")
	if _, err := store.TogglePin("beta"); err != nil {
		t.Fatalf("pin: %v", err)
	}

	m.enterMovePopup()
	lines := m.moveBoardLines()
	if len(lines) != 4 {
		t.Fatalf("lines = %+v, want main, beta, divider, alpha", lines)
	}
	names := make([]string, 0, len(lines))
	for _, l := range lines {
		if l.boardIdx < 0 {
			names = append(names, "──")
			continue
		}
		names = append(names, boardDisplayName(m.move.boards[l.boardIdx].name))
	}
	want := []string{"main", "beta", "──", "alpha"}
	for i := range want {
		if names[i] != want[i] {
			t.Fatalf("left pane = %v, want %v", names, want)
		}
	}
}

// Clicking selects; clicking the row already under the cursor acts.
func TestMovePopupClicksSelectBeforeActing(t *testing.T) {
	m, main := boardWith(t, "ship it|TODO")
	withSprint(t, "demo")

	m.enterMovePopup()
	m.View() // register zones

	demoRow := zoneOf(t, m, zoneMoveRow, int(movePaneBoards), 1)
	m.mouseClick(clickAt(demoRow.x, demoRow.y))
	if m.move.pane != movePaneBoards || m.move.boardIdx != 1 {
		t.Fatalf("pane %v board %d, want the first click to select demo", m.move.pane, m.move.boardIdx)
	}
	if m.view != moveView {
		t.Fatal("the first click acted instead of selecting")
	}
	m.mouseClick(clickAt(demoRow.x, demoRow.y))
	if m.move.pane != movePaneColumns {
		t.Fatalf("pane = %v, want the second click to hand over to the columns", m.move.pane)
	}

	m.View()
	done := zoneOf(t, m, zoneMoveRow, int(movePaneColumns), 3)
	m.mouseClick(clickAt(done.x, done.y))
	if m.view != moveView {
		t.Fatal("a first click on a column committed the move")
	}
	m.mouseClick(clickAt(done.x, done.y))
	if m.view == moveView {
		t.Fatal("the second click did not commit")
	}
	if board, err := main.Load(); err != nil || len(board.Tickets) != 0 {
		t.Errorf("ticket did not leave main (err %v)", err)
	}
}

// The popup opens with the ticket's own column under the cursor. Reading the
// selection alone made that first click commit the move — a write, from the one
// click that is supposed to only look.
func TestMovePopupFirstClickOnThePreselectedColumnOnlySelects(t *testing.T) {
	m, main := boardWith(t, "ship it|TODO")

	m.enterMovePopup()
	m.View()
	before, err := main.Load()
	if err != nil {
		t.Fatal(err)
	}

	current := zoneOf(t, m, zoneMoveRow, int(movePaneColumns), m.move.colIdx)
	m.mouseClick(clickAt(current.x, current.y))

	if m.view != moveView {
		t.Fatalf("view = %v, want the popup still open after one click", m.view)
	}
	after, err := main.Load()
	if err != nil {
		t.Fatal(err)
	}
	if !after.Tickets[0].UpdatedAt.Equal(before.Tickets[0].UpdatedAt) {
		t.Error("a single click rewrote the ticket")
	}

	// And the second click on it still commits.
	m.mouseClick(clickAt(current.x, current.y))
	if m.view == moveView {
		t.Error("the second click did not commit")
	}
}

// Both panes scroll to their cursor. A pane that cannot reach its own cursor
// draws no cursor at all, which on a short terminal left the destination column
// off the list while enter still committed to it.
func TestMovePopupColumnsScrollToTheCursor(t *testing.T) {
	m, _ := boardWith(t, "ship it|HOLD")
	m.focusedCol = 4
	m.enterMovePopup()

	// Three rows of body is what the minimum supported terminal height leaves.
	rows := m.renderMoveColumns(30, 3, point{})
	if len(rows) != 3 {
		t.Fatalf("rendered %d rows, want 3", len(rows))
	}
	joined := strings.Join(rows, "\n")
	if !strings.Contains(ansi.Strip(joined), statusDisplay[model.StatusHold]) {
		t.Errorf("the selected column is not on screen:\n%s", ansi.Strip(joined))
	}
	if !strings.Contains(ansi.Strip(joined), "*") {
		t.Errorf("no cursor drawn in the focused pane:\n%s", ansi.Strip(joined))
	}
}

// A popup shorter than its list must not leave click targets where its dropped
// rows would have gone — those land on the bottom border and the backdrop.
func TestMovePopupZonesStayInsideThePanel(t *testing.T) {
	m := testModel(t, "one")
	for _, name := range []string{"a", "b", "c", "d", "e", "f"} {
		withSprint(t, name)
	}
	m.enterMovePopup()
	m.resetZones()

	const height = 8
	origin := point{x: 4, y: 3}
	panel := m.renderMovePopup("Move 1", 60, height, origin)

	if h := len(strings.Split(panel, "\n")); h != height {
		t.Fatalf("popup rendered %d rows, want %d", h, height)
	}
	for _, z := range m.zones {
		if z.kind != zoneMoveRow {
			continue
		}
		if z.y < origin.y+2 || z.y > origin.y+height-2 {
			t.Errorf("move row zone at y=%d is outside the popup body (rows %d..%d)",
				z.y, origin.y+2, origin.y+height-2)
		}
	}
}

// zoneOf finds a registered zone by kind, pane/column and index.
func zoneOf(t *testing.T, m *Model, kind zoneKind, col, idx int) hitZone {
	t.Helper()
	for _, z := range m.zones {
		if z.kind == kind && z.col == col && z.idx == idx {
			return z
		}
	}
	t.Fatalf("no zone for kind %v col %d idx %d", kind, col, idx)
	return hitZone{}
}

func clickAt(x, y int) tea.MouseMsg {
	return tea.MouseMsg{X: x, Y: y, Button: tea.MouseButtonLeft, Action: tea.MouseActionPress}
}
