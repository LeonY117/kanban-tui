package tui

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/LeonY117/kanban-tui/internal/model"
	"github.com/LeonY117/kanban-tui/internal/store"
)

// sandboxRoot points every board path (main + sprints) at a temp dir so tests
// never touch the real ~/.kanban.
func sandboxRoot(t *testing.T) {
	t.Helper()
	t.Setenv("KANBAN_FILE", filepath.Join(t.TempDir(), "board.json"))
}

func TestMovePopupWalksToAnotherBoard(t *testing.T) {
	sandboxRoot(t)
	if err := store.CreateSprint("demo", ""); err != nil {
		t.Fatalf("create sprint: %v", err)
	}

	main := store.New("")
	ticket, err := main.Add("ship it", "body", model.StatusTodo, []string{"tui"}, "leon", "test")
	if err != nil {
		t.Fatalf("add: %v", err)
	}

	m, err := NewModel(main, "")
	if err != nil {
		t.Fatal(err)
	}
	m.width, m.height, m.ready = 160, 40, true

	m.enterMovePopup()
	if m.view != moveView {
		t.Fatalf("view = %v, want moveView", m.view)
	}
	// The cursor lands on the ticket's current column.
	if !m.moveRows[m.moveIdx].current {
		t.Errorf("cursor did not start on the current column: %+v", m.moveRows[m.moveIdx])
	}

	// Jump to "Other board…" and step through board → column.
	m.moveIdx = len(m.moveRows) - 1
	m.moveActivate()
	if m.moveStage != moveStageBoard {
		t.Fatalf("stage = %v, want board list", m.moveStage)
	}
	if len(m.moveRows) != 1 || m.moveRows[0].board != "demo" {
		t.Fatalf("board rows = %+v, want just the demo sprint", m.moveRows)
	}

	m.moveActivate()
	if m.moveStage != moveStageTargetColumn {
		t.Fatalf("stage = %v, want target column list", m.moveStage)
	}
	m.moveIdx = 2 // Doing
	m.moveActivate()

	if m.view == moveView {
		t.Error("popup stayed open after the move")
	}

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
	if len(sprintBoard.Tickets) != 1 {
		t.Fatalf("sprint holds %d tickets, want 1", len(sprintBoard.Tickets))
	}
	got := sprintBoard.Tickets[0]
	if got.ID != ticket.ID || got.Status != model.StatusDoing {
		t.Errorf("moved ticket = %s/%s, want %s/doing", got.ID, got.Status, ticket.ID)
	}
}

func TestMovePopupSameBoardChangesColumn(t *testing.T) {
	sandboxRoot(t)
	main := store.New("")
	if _, err := main.Add("ship it", "", model.StatusTodo, nil, "", "test"); err != nil {
		t.Fatal(err)
	}
	m, err := NewModel(main, "")
	if err != nil {
		t.Fatal(err)
	}
	m.width, m.height, m.ready = 160, 40, true

	doneIdx := 0
	for i, s := range model.ColumnOrder {
		if s == model.StatusDone {
			doneIdx = i
		}
	}

	m.enterMovePopup()
	m.moveIdx = doneIdx
	m.moveActivate()

	board, err := main.Load()
	if err != nil {
		t.Fatal(err)
	}
	if board.Tickets[0].Status != model.StatusDone {
		t.Errorf("status = %s, want done", board.Tickets[0].Status)
	}
	if m.focusedCol != doneIdx {
		t.Errorf("focus followed to column %d, want %d", m.focusedCol, doneIdx)
	}
}

// A popup shorter than its list must not leave click targets where its dropped
// rows would have gone — those land on the bottom border and the backdrop.
func TestMovePopupZonesStayInsideThePanel(t *testing.T) {
	m := testModel(t, "one")
	m.resetZones()

	m.moveRows = nil
	for i := 0; i < 20; i++ {
		m.moveRows = append(m.moveRows, moveRow{label: "board", isBoard: true})
	}

	const height = 6
	origin := point{x: 4, y: 3}
	panel := m.renderMovePopup(40, height, origin)

	if h := len(strings.Split(panel, "\n")); h != height {
		t.Fatalf("popup rendered %d rows, want %d", h, height)
	}
	for _, z := range m.zones {
		if z.kind != zoneMoveRow {
			continue
		}
		if z.y < origin.y+1 || z.y > origin.y+height-2 {
			t.Errorf("move row zone at y=%d is outside the panel body (rows %d..%d)",
				z.y, origin.y+1, origin.y+height-2)
		}
	}
}
