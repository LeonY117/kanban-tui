package store

import (
	"errors"
	"strings"
	"testing"
)

func TestValidateDescriptionBoundary(t *testing.T) {
	if err := ValidateDescription(strings.Repeat("y", MaxDescriptionLen)); err != nil {
		t.Errorf("a description exactly at the cap was rejected: %v", err)
	}
	if err := ValidateDescription(strings.Repeat("y", MaxDescriptionLen+1)); err == nil {
		t.Error("one character over the cap was accepted")
	}
}

// The cap counts characters, not bytes — a description of emoji would otherwise
// be rejected at a quarter of its apparent length.
func TestValidateDescriptionCountsRunes(t *testing.T) {
	emoji := strings.Repeat("🔧", MaxDescriptionLen)
	if len(emoji) <= MaxDescriptionLen {
		t.Fatalf("setup: wanted a string whose byte length exceeds the cap, got %d bytes", len(emoji))
	}
	if err := ValidateDescription(emoji); err != nil {
		t.Errorf("%d emoji rejected under a %d-character cap: %v", MaxDescriptionLen, MaxDescriptionLen, err)
	}
}

func TestDescriptionHeaderIsTheFirstLine(t *testing.T) {
	for _, tc := range []struct{ desc, want string }{
		{"", ""},
		{"one line only", "one line only"},
		{"The kanban tool's own board.\n\nEverything else follows.", "The kanban tool's own board."},
		{"  padded  \nsecond", "padded"},
		{"\nleading blank line", ""},
	} {
		if got := DescriptionHeader(tc.desc); got != tc.want {
			t.Errorf("DescriptionHeader(%q) = %q, want %q", tc.desc, got, tc.want)
		}
	}
}

func TestSetDescriptionPersistsAndTrims(t *testing.T) {
	sandboxRoot(t)
	s := New("")

	if err := s.SetDescription("\n  Main board.\n\n  Catch-all.  \n\n"); err != nil {
		t.Fatal(err)
	}
	board, err := s.Load()
	if err != nil {
		t.Fatal(err)
	}
	if want := "Main board.\n\n  Catch-all."; board.Description != want {
		t.Errorf("description = %q, want %q — outer whitespace trimmed, inner kept", board.Description, want)
	}
}

// Setting a description must not disturb the prefix or the tickets sharing
// board.json with it.
func TestSetDescriptionLeavesTheRestOfTheBoardAlone(t *testing.T) {
	sandboxRoot(t)
	mustCreateSprint(t, "demo")
	s, err := NewSprint("demo")
	if err != nil {
		t.Fatal(err)
	}
	ticket := addDone(t, s, "a ticket")

	if err := s.SetDescription("Demo sprint."); err != nil {
		t.Fatal(err)
	}
	board, err := s.Load()
	if err != nil {
		t.Fatal(err)
	}
	if board.Prefix != "DE" {
		t.Errorf("prefix = %q, want DE", board.Prefix)
	}
	if len(board.Tickets) != 1 || board.Tickets[0].ID != ticket.ID {
		t.Errorf("tickets = %+v, want just %s", board.Tickets, ticket.ID)
	}
}

func TestSetDescriptionRefusedOnArchivedSprint(t *testing.T) {
	sandboxRoot(t)
	mustCreateSprint(t, "demo")
	s, err := NewSprint("demo")
	if err != nil {
		t.Fatal(err)
	}
	if err := s.SetDescription("before archiving"); err != nil {
		t.Fatal(err)
	}
	if err := ArchiveSprint("demo"); err != nil {
		t.Fatal(err)
	}

	archived, err := NewSprint("demo")
	if err != nil {
		t.Fatal(err)
	}
	if err := archived.SetDescription("after archiving"); !errors.Is(err, ErrArchived) {
		t.Errorf("writing a description to an archived sprint returned %v, want ErrArchived", err)
	}
	board, err := archived.Load()
	if err != nil {
		t.Fatalf("an archived sprint must still read: %v", err)
	}
	if board.Description != "before archiving" {
		t.Errorf("description = %q, want it untouched by the refused write", board.Description)
	}
}

// The cap is enforced in the store, not only at the CLI, so the TUI and any
// future caller inherit it.
func TestSetDescriptionRejectsOverCap(t *testing.T) {
	sandboxRoot(t)
	s := New("")
	if err := s.SetDescription(strings.Repeat("x", MaxDescriptionLen+1)); err == nil {
		t.Fatal("an over-cap description was written")
	}
	board, err := s.Load()
	if err != nil {
		t.Fatal(err)
	}
	if board.Description != "" {
		t.Errorf("description = %q, want empty — the rejected write must not land", board.Description)
	}
}

func TestListSprintsCarriesOnlyTheHeader(t *testing.T) {
	sandboxRoot(t)
	mustCreateSprint(t, "demo")
	s, err := NewSprint("demo")
	if err != nil {
		t.Fatal(err)
	}
	if err := s.SetDescription("Demo sprint.\n\nA second paragraph that a survey of every board should not pay for."); err != nil {
		t.Fatal(err)
	}

	sprints, err := ListSprints()
	if err != nil {
		t.Fatal(err)
	}
	if len(sprints) != 1 {
		t.Fatalf("got %d sprints, want 1", len(sprints))
	}
	if got := sprints[0].Description; got != "Demo sprint." {
		t.Errorf("SprintInfo.Description = %q, want just the first line", got)
	}
}
