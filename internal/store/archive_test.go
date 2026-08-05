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

var archiveOperations = []struct {
	name    string
	archive func(*Store, string) error
}{
	{"ArchiveByID", func(s *Store, id string) error {
		return s.ArchiveByID(id)
	}},
	{"Archive", func(s *Store, _ string) error {
		_, err := s.Archive(nil)
		return err
	}},
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
	for _, operation := range archiveOperations {
		t.Run(operation.name, func(t *testing.T) {
			sandboxRoot(t)
			s := New("")
			ticket := addDone(t, s, "archive me twice")

			unblock := blockWrites(t, s.boardPath())
			if err := operation.archive(s, ticket.ID); err == nil {
				t.Fatalf("%s succeeded despite the board write failing", operation.name)
			}
			unblock()

			if err := operation.archive(s, ticket.ID); err != nil {
				t.Fatalf("retry: %v", err)
			}
			if n := copiesOf(t, s.LoadArchive, ticket.ID); n != 1 {
				t.Errorf("archive holds %d copies after the retry, want 1", n)
			}
			if n := copiesOf(t, s.Load, ticket.ID); n != 0 {
				t.Errorf("board still holds %d copies after the retry, want 0", n)
			}
		})
	}
}

// The board copy stays editable after a partial failure, so by the time the
// retry runs it can be the newer of the two. The retry must carry that version
// into the archive rather than keep the snapshot the first attempt froze.
func TestRetryingAnInterruptedArchiveRefreshesTheEntry(t *testing.T) {
	for _, operation := range archiveOperations {
		t.Run(operation.name, func(t *testing.T) {
			sandboxRoot(t)
			s := New("")
			ticket := addDone(t, s, "v1 title")

			unblock := blockWrites(t, s.boardPath())
			if err := operation.archive(s, ticket.ID); err == nil {
				t.Fatalf("%s succeeded despite the board write failing", operation.name)
			}
			unblock()

			// The date the failed attempt stamped. The archive browser groups by
			// it, so a refresh that kept it would file the entry under the day
			// the archive failed rather than the day it succeeded.
			frozen, err := s.LoadArchive()
			if err != nil {
				t.Fatal(err)
			}
			stale, _ := frozen.FindByUUID(ticket.ID)
			if stale == nil || stale.ArchivedAt == nil {
				t.Fatal("the failed attempt should have left a dated archive entry")
			}
			staleDate := *stale.ArchivedAt

			if err := s.Update(ticket.ID, func(tk *model.Ticket) { tk.Title = "v2 title" }); err != nil {
				t.Fatal(err)
			}
			if err := operation.archive(s, ticket.ID); err != nil {
				t.Fatalf("retry: %v", err)
			}

			archive, err := s.LoadArchive()
			if err != nil {
				t.Fatal(err)
			}
			found, _ := archive.FindByUUID(ticket.ID)
			if found == nil {
				t.Fatal("ticket is in neither file")
			}
			if found.Title != "v2 title" {
				t.Errorf("archive holds %q, want %q — the retry kept the stale copy", found.Title, "v2 title")
			}
			if found.ArchivedAt == nil || !found.ArchivedAt.After(staleDate) {
				t.Errorf("ArchivedAt is %v, want later than the failed attempt's %v", found.ArchivedAt, staleDate)
			}
		})
	}
}

// A ticket that stops matching the bulk filter between the failed attempt and
// the retry is left live and archived at once — accepted, because archiving it
// by id reconciles it. That escape hatch is what this pins.
func TestArchivingByIDReconcilesATicketTheBulkRetrySkipped(t *testing.T) {
	sandboxRoot(t)
	s := New("")
	ticket := addDone(t, s, "ghost me")

	unblock := blockWrites(t, s.boardPath())
	if _, err := s.Archive(nil); err == nil {
		t.Fatal("Archive succeeded despite the board write failing")
	}
	unblock()

	// Out of DONE, so the bulk retry no longer selects it.
	if err := s.Update(ticket.ID, func(tk *model.Ticket) { tk.Status = model.StatusDoing }); err != nil {
		t.Fatal(err)
	}
	// Nothing matches, so nothing is reconciled and the count says so — which is
	// what `kanban archive` prints back.
	if n, err := s.Archive(nil); err != nil {
		t.Fatalf("bulk retry: %v", err)
	} else if n != 0 {
		t.Errorf("bulk retry archived %d, want 0 — nothing still matched", n)
	}
	if copiesOf(t, s.Load, ticket.ID) != 1 || copiesOf(t, s.LoadArchive, ticket.ID) != 1 {
		t.Fatal("expected the documented ghost: live on the board and in the archive")
	}

	if err := s.ArchiveByID(ticket.ID); err != nil {
		t.Fatalf("archive by id: %v", err)
	}
	if n := copiesOf(t, s.Load, ticket.ID); n != 0 {
		t.Errorf("board still holds %d copies, want 0", n)
	}
	if n := copiesOf(t, s.LoadArchive, ticket.ID); n != 1 {
		t.Errorf("archive holds %d copies, want 1", n)
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

	// The restored copy is live on the board before the retry runs, so an edit
	// to it must survive — the archive's copy is the older of the two here.
	if err := s.Update(ticket.ID, func(tk *model.Ticket) { tk.Title = "edited while restored" }); err != nil {
		t.Fatal(err)
	}
	if err := s.Unarchive(ticket.ID); err != nil {
		t.Fatalf("retry: %v", err)
	}
	if n := copiesOf(t, s.Load, ticket.ID); n != 1 {
		t.Errorf("board holds %d copies after the retry, want 1", n)
	}
	if n := copiesOf(t, s.LoadArchive, ticket.ID); n != 0 {
		t.Errorf("archive still holds %d copies after the retry, want 0", n)
	}
	board, err := s.Load()
	if err != nil {
		t.Fatal(err)
	}
	restored, _ := board.FindByUUID(ticket.ID)
	if restored == nil {
		t.Fatal("ticket on neither board")
	}
	if restored.Title != "edited while restored" {
		t.Errorf("board holds %q, want %q — the retry took the archive's older copy", restored.Title, "edited while restored")
	}
	if restored.ArchivedAt != nil {
		t.Error("restored ticket still carries an ArchivedAt")
	}
}
