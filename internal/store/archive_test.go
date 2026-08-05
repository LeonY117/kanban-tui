package store

import (
	"os"
	"testing"

	"github.com/LeonY117/kanban-tui/internal/model"
)

// blockWrites makes atomic writes to path fail until the returned undo runs.
// Save and saveArchive both write <path>.tmp before renaming it into place, and
// WriteFile cannot open a directory — which is what a full disk gets you for
// real, without needing one.
func blockWrites(t *testing.T, path string) func() {
	t.Helper()
	tmp := path + ".tmp"
	if err := os.Mkdir(tmp, 0755); err != nil {
		t.Fatal(err)
	}
	return func() {
		t.Helper()
		if err := os.Remove(tmp); err != nil {
			t.Fatal(err)
		}
	}
}

func addDone(t *testing.T, s *Store, title string) *model.Ticket {
	t.Helper()
	ticket, err := s.Add(title, "", model.StatusDone, nil, "", "test")
	if err != nil {
		t.Fatalf("add %q: %v", title, err)
	}
	return ticket
}

func copiesOf(t *testing.T, load func() (*model.Board, error), id string) int {
	t.Helper()
	board, err := load()
	if err != nil {
		t.Fatal(err)
	}
	n := 0
	for _, ticket := range board.Tickets {
		if ticket.ID == id {
			n++
		}
	}
	return n
}

// A failed archive write must leave the board untouched, or the ticket is gone
// from board.json and archive.json alike — with no backup to recover it from.
func TestArchiveByIDKeepsTicketWhenArchiveWriteFails(t *testing.T) {
	sandboxRoot(t)
	s := New("")
	ticket := addDone(t, s, "keep me")
	defer blockWrites(t, s.archivePath())()

	if err := s.ArchiveByID(ticket.ID); err == nil {
		t.Fatal("ArchiveByID succeeded despite the archive write failing")
	}
	if n := copiesOf(t, s.Load, ticket.ID); n != 1 {
		t.Errorf("board holds %d copies, want 1 — the ticket was dropped", n)
	}
}

// The bulk path takes the whole DONE column at once, so it has the most to lose.
func TestArchiveKeepsTicketsWhenArchiveWriteFails(t *testing.T) {
	sandboxRoot(t)
	s := New("")
	first := addDone(t, s, "keep me")
	addDone(t, s, "and me")
	defer blockWrites(t, s.archivePath())()

	if _, err := s.Archive(nil); err == nil {
		t.Fatal("Archive succeeded despite the archive write failing")
	}
	board, err := s.Load()
	if err != nil {
		t.Fatal(err)
	}
	if len(board.Tickets) != 2 {
		t.Errorf("board holds %d tickets, want 2 — the column was dropped", len(board.Tickets))
	}
	if n := copiesOf(t, s.Load, first.ID); n != 1 {
		t.Errorf("board holds %d copies of the first ticket, want 1", n)
	}
}

// Unarchive is the mirror: a failed board write must leave the archive intact.
func TestUnarchiveKeepsTicketWhenBoardWriteFails(t *testing.T) {
	sandboxRoot(t)
	s := New("")
	ticket := addDone(t, s, "restore me")
	if err := s.ArchiveByID(ticket.ID); err != nil {
		t.Fatal(err)
	}
	defer blockWrites(t, s.boardPath())()

	if err := s.Unarchive(ticket.ID); err == nil {
		t.Fatal("Unarchive succeeded despite the board write failing")
	}
	if n := copiesOf(t, s.LoadArchive, ticket.ID); n != 1 {
		t.Errorf("archive holds %d copies, want 1 — the ticket was dropped", n)
	}
}

// Writing the copy before dropping the original means an interruption between
// the two writes leaves the ticket in both files, and the natural response is to
// run the command again. The retry must finish the job rather than append a
// second entry sharing the first one's UUID.
func TestRetryingAnInterruptedArchiveDoesNotDuplicate(t *testing.T) {
	sandboxRoot(t)
	s := New("")
	ticket := addDone(t, s, "archive me twice")

	unblock := blockWrites(t, s.boardPath())
	if err := s.ArchiveByID(ticket.ID); err == nil {
		t.Fatal("ArchiveByID succeeded despite the board write failing")
	}
	unblock()

	if err := s.ArchiveByID(ticket.ID); err != nil {
		t.Fatalf("retry: %v", err)
	}
	if n := copiesOf(t, s.LoadArchive, ticket.ID); n != 1 {
		t.Errorf("archive holds %d copies after the retry, want 1", n)
	}
	if n := copiesOf(t, s.Load, ticket.ID); n != 0 {
		t.Errorf("board still holds %d copies after the retry, want 0", n)
	}
}

func TestRetryingAnInterruptedBulkArchiveDoesNotDuplicate(t *testing.T) {
	sandboxRoot(t)
	s := New("")
	ticket := addDone(t, s, "bulk me twice")

	unblock := blockWrites(t, s.boardPath())
	if _, err := s.Archive(nil); err == nil {
		t.Fatal("Archive succeeded despite the board write failing")
	}
	unblock()

	if _, err := s.Archive(nil); err != nil {
		t.Fatalf("retry: %v", err)
	}
	if n := copiesOf(t, s.LoadArchive, ticket.ID); n != 1 {
		t.Errorf("archive holds %d copies after the retry, want 1", n)
	}
}

func TestRetryingAnInterruptedUnarchiveDoesNotDuplicate(t *testing.T) {
	sandboxRoot(t)
	s := New("")
	ticket := addDone(t, s, "restore me twice")
	if err := s.ArchiveByID(ticket.ID); err != nil {
		t.Fatal(err)
	}

	unblock := blockWrites(t, s.archivePath())
	if err := s.Unarchive(ticket.ID); err == nil {
		t.Fatal("Unarchive succeeded despite the archive write failing")
	}
	unblock()

	if err := s.Unarchive(ticket.ID); err != nil {
		t.Fatalf("retry: %v", err)
	}
	if n := copiesOf(t, s.Load, ticket.ID); n != 1 {
		t.Errorf("board holds %d copies after the retry, want 1", n)
	}
	if n := copiesOf(t, s.LoadArchive, ticket.ID); n != 0 {
		t.Errorf("archive still holds %d copies after the retry, want 0", n)
	}
}
