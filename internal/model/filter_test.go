package model

import "testing"

// Tag: "" already means "don't filter by tag", so "carries no tags at all"
// needs a field of its own.
func TestFilterUntagged(t *testing.T) {
	board := &Board{Tickets: []Ticket{
		{ShortID: "1", Status: StatusTodo, Tags: []string{"team"}},
		{ShortID: "2", Status: StatusTodo},
		{ShortID: "3", Status: StatusDone},
	}}

	got := board.Filter(FilterOptions{Untagged: true})
	if len(got) != 2 {
		t.Fatalf("Untagged matched %d tickets, want 2", len(got))
	}

	todo := StatusTodo
	got = board.Filter(FilterOptions{Status: &todo, Untagged: true})
	if len(got) != 1 || got[0].ShortID != "2" {
		t.Errorf("Untagged + Status = %+v, want just ticket 2", got)
	}

	// Every field narrows, so asking for both can't match anything.
	if got = board.Filter(FilterOptions{Tag: "team", Untagged: true}); len(got) != 0 {
		t.Errorf("Tag + Untagged = %+v, want nothing", got)
	}
}
