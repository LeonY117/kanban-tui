package store

import (
	"path/filepath"
	"testing"

	"github.com/LeonY117/kanban-tui/internal/model"
)

// sandboxRoot points every board path — main, sprints and the shared id
// counters — at a temp dir, so tests never touch the real ~/.kanban.
func sandboxRoot(t *testing.T) {
	t.Helper()
	t.Setenv("KANBAN_FILE", filepath.Join(t.TempDir(), "board.json"))
}

func TestMoveTicketAcrossBoards(t *testing.T) {
	sandboxRoot(t)
	src := New("")
	if err := CreateSprint("demo", ""); err != nil {
		t.Fatal(err)
	}
	dst, err := NewSprint("demo")
	if err != nil {
		t.Fatal(err)
	}

	ticket, err := src.Add("hello", "body", model.StatusTodo, []string{"x"}, "leon", "test")
	if err != nil {
		t.Fatalf("add: %v", err)
	}

	if err := MoveTicket(src, dst, ticket.ID, model.StatusDoing); err != nil {
		t.Fatalf("move: %v", err)
	}

	srcBoard, err := src.Load()
	if err != nil {
		t.Fatal(err)
	}
	if len(srcBoard.Tickets) != 0 {
		t.Fatalf("source board still holds %d tickets", len(srcBoard.Tickets))
	}

	dstBoard, err := dst.Load()
	if err != nil {
		t.Fatal(err)
	}
	if len(dstBoard.Tickets) != 1 {
		t.Fatalf("destination board holds %d tickets, want 1", len(dstBoard.Tickets))
	}
	moved := dstBoard.Tickets[0]
	if moved.ID != ticket.ID {
		t.Errorf("id changed: %s → %s", ticket.ID, moved.ID)
	}
	if moved.Status != model.StatusDoing {
		t.Errorf("status = %s, want doing", moved.Status)
	}
	if moved.Title != "hello" || moved.Description != "body" || moved.AssignedTo != "leon" {
		t.Errorf("content not preserved: %+v", moved)
	}
	if moved.ShortID == "" {
		t.Error("short id not re-derived")
	}
}

func TestMoveTicketSameBoardIsStatusChange(t *testing.T) {
	sandboxRoot(t)
	s := New("")
	ticket, err := s.Add("hello", "", model.StatusTodo, nil, "", "test")
	if err != nil {
		t.Fatalf("add: %v", err)
	}

	if err := MoveTicket(s, s, ticket.ID, model.StatusDone); err != nil {
		t.Fatalf("move: %v", err)
	}

	board, err := s.Load()
	if err != nil {
		t.Fatal(err)
	}
	if len(board.Tickets) != 1 {
		t.Fatalf("board holds %d tickets, want 1", len(board.Tickets))
	}
	if board.Tickets[0].Status != model.StatusDone {
		t.Errorf("status = %s, want done", board.Tickets[0].Status)
	}
	if board.Tickets[0].ShortID != ticket.ShortID {
		t.Errorf("short id changed on a same-board move")
	}
}
