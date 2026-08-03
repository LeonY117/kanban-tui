package cmd

import (
	"bytes"
	"path/filepath"
	"strings"
	"testing"

	"github.com/LeonY117/kanban-tui/internal/model"
	"github.com/LeonY117/kanban-tui/internal/store"
)

// sandboxTicket points every board path at a temp dir and seeds one TODO
// ticket, so tests drive the real cobra tree without touching ~/.kanban.
func sandboxTicket(t *testing.T) (*store.Store, *model.Ticket) {
	t.Helper()
	t.Setenv("KANBAN_FILE", filepath.Join(t.TempDir(), "board.json"))
	s := store.New("")
	ticket, err := s.Add("hello", "body", model.StatusTodo, nil, "", "test")
	if err != nil {
		t.Fatalf("add: %v", err)
	}
	rootCmd.SetOut(&bytes.Buffer{})
	rootCmd.SetErr(&bytes.Buffer{})
	return s, ticket
}

func TestUpdateRejectsInvalidStatus(t *testing.T) {
	s, ticket := sandboxTicket(t)

	rootCmd.SetArgs([]string{"update", ticket.ShortID, "--status", "BOGUS"})
	err := rootCmd.Execute()
	if err == nil {
		t.Fatal("expected an error for an invalid status, got nil")
	}
	if !strings.Contains(err.Error(), "invalid status") {
		t.Errorf("error should name the problem, got: %v", err)
	}

	board, err := s.Load()
	if err != nil {
		t.Fatal(err)
	}
	got, _ := board.FindByID(ticket.ShortID)
	if got == nil {
		t.Fatal("ticket vanished")
	}
	if got.Status != model.StatusTodo {
		t.Errorf("status changed on a failed update: got %s, want %s", got.Status, model.StatusTodo)
	}
}

func TestUpdateAppliesValidStatus(t *testing.T) {
	s, ticket := sandboxTicket(t)

	rootCmd.SetArgs([]string{"update", ticket.ShortID, "--status", "doing"})
	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("update: %v", err)
	}

	board, err := s.Load()
	if err != nil {
		t.Fatal(err)
	}
	got, _ := board.FindByID(ticket.ShortID)
	if got.Status != model.StatusDoing {
		t.Errorf("got %s, want %s", got.Status, model.StatusDoing)
	}
}

// The four commands that take --status must all name the valid values, and
// name them from model.AllStatuses so they can't drift apart.
func TestStatusFlagsEnumerateValues(t *testing.T) {
	for _, c := range []struct{ name, flag string }{
		{"add", "status"},
		{"update", "status"},
		{"list", "status"},
		{"move", "status"},
	} {
		cmd, _, err := rootCmd.Find([]string{c.name})
		if err != nil {
			t.Fatalf("find %s: %v", c.name, err)
		}
		usage := cmd.Flags().Lookup(c.flag).Usage
		for _, s := range model.AllStatuses {
			if !strings.Contains(usage, string(s)) {
				t.Errorf("%s --status help omits %s: %q", c.name, s, usage)
			}
		}
	}
}
