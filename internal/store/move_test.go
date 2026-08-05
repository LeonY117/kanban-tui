package store

import (
	"path/filepath"
	"testing"
	"time"

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

// A move that dies between the destination write and the source removal leaves
// the ticket on both boards, and the obvious response is to run it again. The
// retry has to finish the move, not append a second copy carrying the first
// one's UUID — two tickets with one UUID can't be told apart by any lookup.
func TestRetryingAnInterruptedMoveDoesNotDuplicate(t *testing.T) {
	sandboxRoot(t)
	src := New("")
	if err := CreateSprint("demo", ""); err != nil {
		t.Fatal(err)
	}
	dst, err := NewSprint("demo")
	if err != nil {
		t.Fatal(err)
	}

	ticket, err := src.Add("hello", "body", model.StatusTodo, nil, "", "test")
	if err != nil {
		t.Fatal(err)
	}

	// Simulate the interrupted first attempt: the destination has the ticket,
	// the source still does too.
	board, err := dst.Load()
	if err != nil {
		t.Fatal(err)
	}
	board.Tickets = append(board.Tickets, *ticket)
	if err := dst.Save(board); err != nil {
		t.Fatal(err)
	}

	if err := MoveTicket(src, dst, ticket.ShortID, model.StatusDoing); err != nil {
		t.Fatalf("retry: %v", err)
	}

	dstBoard, err := dst.Load()
	if err != nil {
		t.Fatal(err)
	}
	if n := len(dstBoard.Tickets); n != 1 {
		t.Fatalf("destination has %d tickets, want 1 — the retry duplicated it", n)
	}
	srcBoard, err := src.Load()
	if err != nil {
		t.Fatal(err)
	}
	if len(srcBoard.Tickets) != 0 {
		t.Errorf("source still holds the ticket after the retry: %+v", srcBoard.Tickets)
	}
}

// The source copy stays editable after an interrupted move, so by the time the
// retry runs it can be the newer of the two. The retry must carry that version
// across rather than keep the copy the first attempt left on the destination —
// while keeping the short id that board already minted.
func TestRetryingAnInterruptedMoveRefreshesTheCopy(t *testing.T) {
	sandboxRoot(t)
	src := New("")
	if err := CreateSprint("demo", ""); err != nil {
		t.Fatal(err)
	}
	dst, err := NewSprint("demo")
	if err != nil {
		t.Fatal(err)
	}

	ticket, err := src.Add("v1 title", "", model.StatusTodo, nil, "", "test")
	if err != nil {
		t.Fatal(err)
	}
	// Park the source's short id on the destination, so arriving there forces a
	// renumber and the retry has a minted id it must not clobber.
	blocker := addTicket(t, dst, "already here")
	dstBoard, err := dst.Load()
	if err != nil {
		t.Fatal(err)
	}
	held, _ := dstBoard.FindByUUID(blocker.ID)
	held.ShortID = ticket.ShortID
	if err := dst.Save(dstBoard); err != nil {
		t.Fatal(err)
	}

	unblock := blockWrites(t, src.boardPath())
	if err := MoveTicket(src, dst, ticket.ShortID, model.StatusDoing); err == nil {
		t.Fatal("expected the source write to fail")
	}
	unblock()

	minted := ""
	dstBoard, err = dst.Load()
	if err != nil {
		t.Fatal(err)
	}
	if arrived, _ := dstBoard.FindByUUID(ticket.ID); arrived != nil {
		minted = arrived.ShortID
	}
	if minted == "" || minted == ticket.ShortID {
		t.Fatalf("setup: destination should have minted a fresh id, got %q", minted)
	}

	if err := src.Update(ticket.ID, func(tk *model.Ticket) { tk.Title = "v2 title" }); err != nil {
		t.Fatal(err)
	}
	if err := MoveTicket(src, dst, ticket.ShortID, model.StatusDoing); err != nil {
		t.Fatalf("retry: %v", err)
	}

	dstBoard, err = dst.Load()
	if err != nil {
		t.Fatal(err)
	}
	moved, _ := dstBoard.FindByUUID(ticket.ID)
	if moved == nil {
		t.Fatal("ticket on neither board")
	}
	if moved.Title != "v2 title" {
		t.Errorf("destination holds %q, want %q — the retry kept the stale copy", moved.Title, "v2 title")
	}
	if moved.ShortID != minted {
		t.Errorf("short id changed to %q on the retry, want the minted %q", moved.ShortID, minted)
	}
	srcBoard, err := src.Load()
	if err != nil {
		t.Fatal(err)
	}
	if len(srcBoard.Tickets) != 0 {
		t.Errorf("source still holds the ticket after the retry: %+v", srcBoard.Tickets)
	}
}

// The mirror of the case above: an interrupted move leaves the ticket live on
// two ordinary boards, so the destination copy can be the edited one — the user
// finds it where they wanted it and works on it there. Whichever side carries
// the newer edit has to survive the retry, so the retry cannot simply prefer
// the direction the move runs in.
func TestRetryingAnInterruptedMoveKeepsADestinationEdit(t *testing.T) {
	sandboxRoot(t)
	src := New("")
	if err := CreateSprint("demo", ""); err != nil {
		t.Fatal(err)
	}
	dst, err := NewSprint("demo")
	if err != nil {
		t.Fatal(err)
	}
	ticket, err := src.Add("v1 title", "", model.StatusTodo, nil, "", "test")
	if err != nil {
		t.Fatal(err)
	}

	unblock := blockWrites(t, src.boardPath())
	if err := MoveTicket(src, dst, ticket.ShortID, model.StatusDoing); err == nil {
		t.Fatal("expected the source write to fail")
	}
	unblock()

	if err := dst.Update(ticket.ID, func(tk *model.Ticket) { tk.Title = "edited on the destination" }); err != nil {
		t.Fatal(err)
	}
	// A different status from the first attempt, so the assertion below can only
	// pass if the retry actually reapplies it. Asking for DOING again would be
	// satisfied by the copy the failed attempt already left in DOING.
	if err := MoveTicket(src, dst, ticket.ShortID, model.StatusHold); err != nil {
		t.Fatalf("retry: %v", err)
	}

	dstBoard, err := dst.Load()
	if err != nil {
		t.Fatal(err)
	}
	moved, _ := dstBoard.FindByUUID(ticket.ID)
	if moved == nil {
		t.Fatal("ticket on neither board")
	}
	if moved.Title != "edited on the destination" {
		t.Errorf("destination holds %q, want %q — the retry overwrote it from the stale source", moved.Title, "edited on the destination")
	}
	if moved.Status != model.StatusHold {
		t.Errorf("status = %s, want hold — the retry has to reapply the move's status", moved.Status)
	}
	srcBoard, err := src.Load()
	if err != nil {
		t.Fatal(err)
	}
	if len(srcBoard.Tickets) != 0 {
		t.Errorf("source still holds the ticket after the retry: %+v", srcBoard.Tickets)
	}
}

// Timestamps can tie: an Update always bumps UpdatedAt, so the two copies never
// tie through the API, but a hand-edited board or one written by an older
// version can. A tie is no evidence that the source is the newer copy, so the
// destination — already committed at the target — has to keep its content.
func TestRetryingAnInterruptedMoveKeepsTheDestinationOnATie(t *testing.T) {
	sandboxRoot(t)
	src := New("")
	if err := CreateSprint("demo", ""); err != nil {
		t.Fatal(err)
	}
	dst, err := NewSprint("demo")
	if err != nil {
		t.Fatal(err)
	}
	ticket, err := src.Add("v1 title", "", model.StatusTodo, nil, "", "test")
	if err != nil {
		t.Fatal(err)
	}

	unblock := blockWrites(t, src.boardPath())
	if err := MoveTicket(src, dst, ticket.ShortID, model.StatusDoing); err == nil {
		t.Fatal("expected the source write to fail")
	}
	unblock()

	if err := dst.Update(ticket.ID, func(tk *model.Ticket) { tk.Title = "edited on the destination" }); err != nil {
		t.Fatal(err)
	}

	// Force the tie the API cannot produce: give the source copy exactly the
	// destination's timestamp.
	dstBoard, err := dst.Load()
	if err != nil {
		t.Fatal(err)
	}
	landed, _ := dstBoard.FindByUUID(ticket.ID)
	if landed == nil {
		t.Fatal("the failed attempt should have left a copy on the destination")
	}
	srcBoard, err := src.Load()
	if err != nil {
		t.Fatal(err)
	}
	stranded, _ := srcBoard.FindByUUID(ticket.ID)
	if stranded == nil {
		t.Fatal("the failed attempt should have left the source copy in place")
	}
	stranded.UpdatedAt = landed.UpdatedAt
	if err := src.Save(srcBoard); err != nil {
		t.Fatal(err)
	}

	if err := MoveTicket(src, dst, ticket.ShortID, model.StatusHold); err != nil {
		t.Fatalf("retry: %v", err)
	}

	dstBoard, err = dst.Load()
	if err != nil {
		t.Fatal(err)
	}
	moved, _ := dstBoard.FindByUUID(ticket.ID)
	if moved == nil {
		t.Fatal("ticket on neither board")
	}
	if moved.Title != "edited on the destination" {
		t.Errorf("destination holds %q, want %q — a tie handed it to the source", moved.Title, "edited on the destination")
	}
}

// A copy dated in the future — hand-edited JSON, a clock walked back — must not
// be stamped backwards by the retry that keeps it. Lowering it under the copy it
// just beat would hand a second interrupted retry the opposite winner, undoing
// the edit this one preserved.
func TestRetryingAnInterruptedMoveDoesNotStampTheCopyBackwards(t *testing.T) {
	sandboxRoot(t)
	src := New("")
	if err := CreateSprint("demo", ""); err != nil {
		t.Fatal(err)
	}
	dst, err := NewSprint("demo")
	if err != nil {
		t.Fatal(err)
	}
	ticket, err := src.Add("v1 title", "", model.StatusTodo, nil, "", "test")
	if err != nil {
		t.Fatal(err)
	}

	unblock := blockWrites(t, src.boardPath())
	if err := MoveTicket(src, dst, ticket.ShortID, model.StatusDoing); err == nil {
		t.Fatal("expected the source write to fail")
	}
	unblock()

	if err := dst.Update(ticket.ID, func(tk *model.Ticket) { tk.Title = "edited on the destination" }); err != nil {
		t.Fatal(err)
	}

	// Both copies dated ahead of the clock, the destination the later of the two.
	// Written straight to the boards: Update stamps UpdatedAt itself after apply
	// runs, so it cannot be used to plant a date.
	now := time.Now()
	srcStamp := now.Add(time.Hour)
	dstBoard, err := dst.Load()
	if err != nil {
		t.Fatal(err)
	}
	landed, _ := dstBoard.FindByUUID(ticket.ID)
	if landed == nil {
		t.Fatal("the failed attempt should have left a copy on the destination")
	}
	landed.UpdatedAt = now.Add(24 * time.Hour)
	if err := dst.Save(dstBoard); err != nil {
		t.Fatal(err)
	}
	srcBoard, err := src.Load()
	if err != nil {
		t.Fatal(err)
	}
	stranded, _ := srcBoard.FindByUUID(ticket.ID)
	if stranded == nil {
		t.Fatal("the failed attempt should have left the source copy in place")
	}
	stranded.UpdatedAt = srcStamp
	if err := src.Save(srcBoard); err != nil {
		t.Fatal(err)
	}

	if err := MoveTicket(src, dst, ticket.ShortID, model.StatusHold); err != nil {
		t.Fatalf("retry: %v", err)
	}

	dstBoard, err = dst.Load()
	if err != nil {
		t.Fatal(err)
	}
	moved, _ := dstBoard.FindByUUID(ticket.ID)
	if moved == nil {
		t.Fatal("ticket on neither board")
	}
	if moved.Title != "edited on the destination" {
		t.Errorf("destination holds %q, want the destination's edit", moved.Title)
	}
	if moved.UpdatedAt.Before(srcStamp) {
		t.Errorf("stamped back to %v, below the source's %v — a second retry would flip the winner",
			moved.UpdatedAt, srcStamp)
	}
}

// Collision detection must compare short ids exactly. FindByID also matches
// UUID prefixes, so moving a ticket whose short id is "1" into a board holding
// an unrelated ticket whose UUID merely starts with "1" used to look like a
// collision and renamed the moved ticket for no reason.
func TestMoveKeepsIDWhenOnlyAUUIDPrefixMatches(t *testing.T) {
	sandboxRoot(t)
	src := New("")
	if err := CreateSprint("demo", "KA"); err != nil {
		t.Fatal(err)
	}
	dst, err := NewSprint("demo")
	if err != nil {
		t.Fatal(err)
	}

	ticket, err := src.Add("moved", "", model.StatusTodo, nil, "", "test")
	if err != nil {
		t.Fatal(err)
	}

	// A destination ticket whose UUID starts with the moved ticket's short id.
	board, err := dst.Load()
	if err != nil {
		t.Fatal(err)
	}
	board.Tickets = append(board.Tickets, model.Ticket{
		ID:      ticket.ShortID + "0000000-0000-0000-0000-000000000000"[len(ticket.ShortID)-1:],
		ShortID: "KA9",
		Title:   "unrelated",
		Status:  model.StatusTodo,
	})
	if err := dst.Save(board); err != nil {
		t.Fatal(err)
	}

	if err := MoveTicket(src, dst, ticket.ShortID, model.StatusDoing); err != nil {
		t.Fatal(err)
	}

	dstBoard, err := dst.Load()
	if err != nil {
		t.Fatal(err)
	}
	moved, _ := dstBoard.FindByUUID(ticket.ID)
	if moved == nil {
		t.Fatal("moved ticket is not on the destination board")
	}
	if moved.ShortID != ticket.ShortID {
		t.Errorf("moved ticket was renamed %s → %s on a UUID-prefix false collision", ticket.ShortID, moved.ShortID)
	}
}

// Holding both boards' locks is what makes a move atomic, and two locks invite
// deadlock: a move A→B holding A's lock while a move B→A holds B's would wait
// on each other forever. Ordering the acquisition by board path prevents it.
func TestOppositeMovesDoNotDeadlock(t *testing.T) {
	sandboxRoot(t)
	for _, name := range []string{"alpha", "beta"} {
		if err := CreateSprint(name, ""); err != nil {
			t.Fatal(err)
		}
	}
	alpha, err := NewSprint("alpha")
	if err != nil {
		t.Fatal(err)
	}
	beta, err := NewSprint("beta")
	if err != nil {
		t.Fatal(err)
	}

	there, err := alpha.Add("there", "", model.StatusTodo, nil, "", "test")
	if err != nil {
		t.Fatal(err)
	}
	back, err := beta.Add("back", "", model.StatusTodo, nil, "", "test")
	if err != nil {
		t.Fatal(err)
	}

	done := make(chan error, 2)
	go func() { done <- MoveTicket(alpha, beta, there.ShortID, model.StatusDoing) }()
	go func() { done <- MoveTicket(beta, alpha, back.ShortID, model.StatusDoing) }()

	for i := 0; i < 2; i++ {
		select {
		case err := <-done:
			if err != nil {
				t.Errorf("move %d: %v", i, err)
			}
		case <-time.After(10 * time.Second):
			t.Fatal("opposite moves deadlocked")
		}
	}
}
