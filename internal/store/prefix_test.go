package store

import (
	"testing"

	"github.com/LeonY117/kanban-tui/internal/model"
)

func TestDerivePrefixTakesFirstTwoLetters(t *testing.T) {
	cases := map[string]string{
		"kanban":         "KA",
		"kb-server":      "KB",
		"0719-demo":      "DE", // digits skipped
		"live":           "LI",
		"x":              "X",
		"0625-JA-review": "JA",
		"123":            "X", // nothing to derive from
	}
	for name, want := range cases {
		if got := DerivePrefix(name); got != want {
			t.Errorf("DerivePrefix(%q) = %q, want %q", name, got, want)
		}
	}
}

func TestTicketIDsAreSequentialPerBoard(t *testing.T) {
	sandboxRoot(t)
	main := New("")

	for i, want := range []string{"1", "2", "3"} {
		ticket, err := main.Add("t", "", model.StatusTodo, nil, "", "test")
		if err != nil {
			t.Fatalf("add %d: %v", i, err)
		}
		if ticket.ShortID != want {
			t.Errorf("main ticket %d has id %q, want %q", i, ticket.ShortID, want)
		}
	}

	if err := CreateSprint("kanban", ""); err != nil {
		t.Fatal(err)
	}
	sprint, err := NewSprint("kanban")
	if err != nil {
		t.Fatal(err)
	}
	ticket, err := sprint.Add("t", "", model.StatusTodo, nil, "", "test")
	if err != nil {
		t.Fatal(err)
	}
	if ticket.ShortID != "KA1" {
		t.Errorf("sprint ticket has id %q, want KA1", ticket.ShortID)
	}
}

// Two boards sharing a prefix share its number line, so an id is never handed
// out twice — even after the first board is archived.
func TestSharedPrefixContinuesNumbering(t *testing.T) {
	sandboxRoot(t)

	if err := CreateSprint("kanban", "K"); err != nil {
		t.Fatal(err)
	}
	first, err := NewSprint("kanban")
	if err != nil {
		t.Fatal(err)
	}
	var last string
	for i := 0; i < 12; i++ {
		ticket, err := first.Add("t", "", model.StatusTodo, nil, "", "test")
		if err != nil {
			t.Fatal(err)
		}
		last = ticket.ShortID
	}
	if last != "K12" {
		t.Fatalf("twelfth ticket is %q, want K12", last)
	}
	if err := ArchiveSprint("kanban"); err != nil {
		t.Fatal(err)
	}

	if err := CreateSprint("kb-tools", "K"); err != nil {
		t.Fatal(err)
	}
	second, err := NewSprint("kb-tools")
	if err != nil {
		t.Fatal(err)
	}
	ticket, err := second.Add("t", "", model.StatusTodo, nil, "", "test")
	if err != nil {
		t.Fatal(err)
	}
	if ticket.ShortID != "K13" {
		t.Errorf("new board on prefix K starts at %q, want K13", ticket.ShortID)
	}
}

// Losing counters.json must not restart a prefix's numbering on top of ids
// that already exist.
func TestCounterReseedsFromDisk(t *testing.T) {
	sandboxRoot(t)
	if err := CreateSprint("kanban", "K"); err != nil {
		t.Fatal(err)
	}
	s, err := NewSprint("kanban")
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 3; i++ {
		if _, err := s.Add("t", "", model.StatusTodo, nil, "", "test"); err != nil {
			t.Fatal(err)
		}
	}

	if err := removeCountersFile(); err != nil {
		t.Fatal(err)
	}
	ticket, err := s.Add("t", "", model.StatusTodo, nil, "", "test")
	if err != nil {
		t.Fatal(err)
	}
	if ticket.ShortID != "K4" {
		t.Errorf("after losing the counter file the next id was %q, want K4", ticket.ShortID)
	}
}

// Boards created before prefixes existed pick one up the next time a ticket is
// added, without disturbing the ids already on them.
func TestLegacyBoardAdoptsAPrefix(t *testing.T) {
	sandboxRoot(t)
	if err := CreateSprint("legacy-board", ""); err != nil {
		t.Fatal(err)
	}
	s, err := NewSprint("legacy-board")
	if err != nil {
		t.Fatal(err)
	}
	board, err := s.Load()
	if err != nil {
		t.Fatal(err)
	}
	board.Prefix = ""
	board.Tickets = append(board.Tickets, model.Ticket{ID: "abc", ShortID: "4ad9b9", Title: "old", Status: model.StatusTodo})
	if err := s.Save(board); err != nil {
		t.Fatal(err)
	}

	ticket, err := s.Add("new", "", model.StatusTodo, nil, "", "test")
	if err != nil {
		t.Fatal(err)
	}
	if ticket.ShortID != "LE1" {
		t.Errorf("legacy board issued %q, want LE1", ticket.ShortID)
	}
	board, _ = s.Load()
	if board.Prefix != "LE" {
		t.Errorf("prefix not persisted: %q", board.Prefix)
	}
	if t2, _ := board.FindByID("4ad9b9"); t2 == nil {
		t.Error("the old hex id stopped resolving")
	}
}

func TestFindByIDAcceptsBareNumberOnPrefixedBoard(t *testing.T) {
	sandboxRoot(t)
	if err := CreateSprint("kanban", "K"); err != nil {
		t.Fatal(err)
	}
	s, _ := NewSprint("kanban")
	if _, err := s.Add("t", "", model.StatusTodo, nil, "", "test"); err != nil {
		t.Fatal(err)
	}
	board, _ := s.Load()

	for _, id := range []string{"K1", "k1", "1"} {
		if found, _ := board.FindByID(id); found == nil {
			t.Errorf("FindByID(%q) found nothing", id)
		}
	}
	if found, _ := board.FindByID("K2"); found != nil {
		t.Error("FindByID matched an id that doesn't exist")
	}
}

// A legacy short id that happens to be all digits ("993899") must not be read
// as a counter value, or the main board's numbering starts in the millions.
func TestLegacyAllDigitShortIDsDoNotSeedTheCounter(t *testing.T) {
	sandboxRoot(t)
	main := New("")
	board, err := main.Load()
	if err != nil {
		t.Fatal(err)
	}
	board.Tickets = append(board.Tickets,
		model.Ticket{ID: "993899ab-0000-4000-8000-000000000000", ShortID: "993899", Title: "legacy", Status: model.StatusTodo},
		model.Ticket{ID: "4ad9b972-0000-4000-8000-000000000000", ShortID: "4ad9b9", Title: "legacy", Status: model.StatusTodo},
	)
	if err := main.Save(board); err != nil {
		t.Fatal(err)
	}

	ticket, err := main.Add("first real id", "", model.StatusTodo, nil, "", "test")
	if err != nil {
		t.Fatal(err)
	}
	if ticket.ShortID != "1" {
		t.Errorf("main board started at %q, want 1", ticket.ShortID)
	}
}

// A board that predates prefixes must report the one it will use before any
// ticket forces the assignment — otherwise the TUI has nothing to display.
func TestEffectivePrefixBeforeFirstTicket(t *testing.T) {
	sandboxRoot(t)
	if err := CreateSprint("legacy", ""); err != nil {
		t.Fatal(err)
	}
	s, _ := NewSprint("legacy")
	board, _ := s.Load()
	board.Prefix = "" // as every board created before this scheme looks
	if err := s.Save(board); err != nil {
		t.Fatal(err)
	}

	board, _ = s.Load()
	if got := EffectivePrefix(board, "legacy"); got != "LE" {
		t.Errorf("EffectivePrefix = %q, want LE", got)
	}
	if got := EffectivePrefix(board, ""); got != "" {
		t.Errorf("main board should have no prefix, got %q", got)
	}

	sprints, err := ListSprints()
	if err != nil {
		t.Fatal(err)
	}
	if len(sprints) != 1 || sprints[0].Prefix != "LE" {
		t.Errorf("ListSprints reported %+v, want prefix LE", sprints)
	}
}
