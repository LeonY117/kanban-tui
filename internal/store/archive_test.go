package store

import (
	"os"
	"testing"

	"github.com/LeonY117/kanban-tui/internal/model"
)

// A failed archive write must leave source board untouched. Otherwise ticket
// disappears from both board.json and archive.json.
func TestArchiveByIDKeepsTicketOnArchiveWriteFailure(t *testing.T) {
	sandboxRoot(t)
	s := New("")
	ticket, err := s.Add("keep me", "", model.StatusDone, nil, "", "test")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(s.archivePath()+".tmp", 0755); err != nil {
		t.Fatal(err)
	}

	if err := s.ArchiveByID(ticket.ID); err == nil {
		t.Fatal("ArchiveByID succeeded despite archive write failure")
	}

	board, err := s.Load()
	if err != nil {
		t.Fatal(err)
	}
	if found, _ := board.FindByUUID(ticket.ID); found == nil {
		t.Error("ticket disappeared after archive write failure")
	}
}

// A failed board write during unarchive must leave archive untouched. This
// preserves recoverable duplicate state instead of dropping ticket entirely.
func TestUnarchiveKeepsTicketOnBoardWriteFailure(t *testing.T) {
	sandboxRoot(t)
	s := New("")
	ticket, err := s.Add("restore me", "", model.StatusDone, nil, "", "test")
	if err != nil {
		t.Fatal(err)
	}
	if err := s.ArchiveByID(ticket.ID); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(s.boardPath()+".tmp", 0755); err != nil {
		t.Fatal(err)
	}

	if err := s.Unarchive(ticket.ID); err == nil {
		t.Fatal("Unarchive succeeded despite board write failure")
	}

	archive, err := s.LoadArchive()
	if err != nil {
		t.Fatal(err)
	}
	if found, _ := archive.FindByUUID(ticket.ID); found == nil {
		t.Error("ticket disappeared after board write failure")
	}
}
