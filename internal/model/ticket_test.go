package model

import "testing"

// A ticket moved in from another board keeps its id, so a prefixed board can
// hold both `1` and `KA1`. Typing `1` has to mean the ticket actually called
// `1`, not whichever of the two the implied prefix reaches first.
func TestFindByIDPrefersAnExactMatchOverTheImpliedPrefix(t *testing.T) {
	board := &Board{
		Prefix: "KA",
		Tickets: []Ticket{
			{ID: "aaaaaaaa-0000-0000-0000-000000000000", ShortID: "KA1", Title: "native"},
			{ID: "bbbbbbbb-0000-0000-0000-000000000000", ShortID: "1", Title: "moved in"},
		},
	}

	got, _ := board.FindByID("1")
	if got == nil || got.Title != "moved in" {
		t.Errorf("FindByID(\"1\") = %v, want the ticket whose short id is exactly 1", got)
	}

	// The implied prefix still resolves when nothing matches exactly.
	board.Tickets = board.Tickets[:1]
	got, _ = board.FindByID("1")
	if got == nil || got.ShortID != "KA1" {
		t.Errorf("FindByID(\"1\") = %v, want KA1 via the implied prefix", got)
	}
}

// The short id wins over an unrelated ticket whose UUID merely starts with the
// same characters — otherwise ids handed out by the counter would be shadowed
// by legacy UUIDs at random.
func TestFindByIDPrefersShortIDOverAUUIDPrefix(t *testing.T) {
	board := &Board{
		Tickets: []Ticket{
			{ID: "12345678-0000-0000-0000-000000000000", ShortID: "993899", Title: "legacy"},
			{ID: "cccccccc-0000-0000-0000-000000000000", ShortID: "12", Title: "counter-issued"},
		},
	}

	got, _ := board.FindByID("12")
	if got == nil || got.Title != "counter-issued" {
		t.Errorf("FindByID(\"12\") = %v, want the ticket whose short id is 12", got)
	}
}

// WAITING shipped as a sixth column in a fork before "waiting" and "hold" were
// settled as one state. Anyone who got used to typing it should still land in
// the column they meant.
func TestParseStatusAcceptsWaitingAsHold(t *testing.T) {
	for _, in := range []string{"WAITING", "waiting", " Waiting ", "waiting on", "WAITING_ON", "waiting-on"} {
		got, err := ParseStatus(in)
		if err != nil {
			t.Errorf("ParseStatus(%q) errored: %v", in, err)
			continue
		}
		if got != StatusHold {
			t.Errorf("ParseStatus(%q) = %q, want %q", in, got, StatusHold)
		}
	}
}

func TestParseStatusStillRejectsNonsense(t *testing.T) {
	if _, err := ParseStatus("blocked"); err == nil {
		t.Error("ParseStatus(\"blocked\") = nil error, want a rejection")
	}
}

// A board written by the build that had a WAITING column would otherwise hold
// tickets in a status no column renders, so they'd vanish from the TUI while
// still sitting in board.json.
func TestNormalizeStatusesRewritesAliasedStatuses(t *testing.T) {
	board := &Board{Tickets: []Ticket{
		{ShortID: "1", Status: "WAITING"},
		{ShortID: "2", Status: StatusHold},
		{ShortID: "3", Status: StatusTodo},
	}}

	if changed := board.NormalizeStatuses(); changed != 1 {
		t.Errorf("NormalizeStatuses() = %d, want 1", changed)
	}
	want := []Status{StatusHold, StatusHold, StatusTodo}
	for i, w := range want {
		if board.Tickets[i].Status != w {
			t.Errorf("ticket %s status = %q, want %q", board.Tickets[i].ShortID, board.Tickets[i].Status, w)
		}
	}
	if len(board.ByStatus(StatusHold)) != 2 {
		t.Errorf("ByStatus(HOLD) = %d tickets, want 2", len(board.ByStatus(StatusHold)))
	}
}
