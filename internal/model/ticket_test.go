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
