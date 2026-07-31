package tui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/LeonY117/kanban-tui/internal/model"
	"github.com/LeonY117/kanban-tui/internal/store"
)

type seedCard struct {
	title  string
	status model.Status
	tags   []string
}

func boardWith(t *testing.T, cards ...seedCard) *Model {
	t.Helper()
	sandboxRoot(t)
	st := store.New("")
	for _, c := range cards {
		if _, err := st.Add(c.title, "", c.status, c.tags, "", ""); err != nil {
			t.Fatalf("seeding %q: %v", c.title, err)
		}
	}
	m, err := NewModel(st, "")
	if err != nil {
		t.Fatal(err)
	}
	m.width, m.height, m.ready = 160, 40, true
	return m
}

// seedTagBoard builds a board with a mix of tagged, untagged, and DONE cards.
func seedTagBoard(t *testing.T) *Model {
	t.Helper()
	return boardWith(t,
		seedCard{"team todo", model.StatusTodo, []string{"team"}},
		seedCard{"team doing", model.StatusDoing, []string{"team"}},
		seedCard{"customer todo", model.StatusTodo, []string{"customer"}},
		seedCard{"untagged todo", model.StatusTodo, nil},
		seedCard{"done team", model.StatusDone, []string{"team"}},
		seedCard{"done only-tag", model.StatusDone, []string{"ghost"}},
	)
}

// The tag picker lists open cards only: DONE cards don't contribute counts,
// and a tag that exists only on DONE cards doesn't get a row.
func TestBuildTagEntriesExcludesDone(t *testing.T) {
	m := seedTagBoard(t)
	entries := buildTagEntries(m.board)

	want := []tagEntry{
		{value: "", count: 4}, // all open cards
		{value: "customer", count: 1},
		{value: "team", count: 2}, // done team card doesn't count
		{value: untaggedFilter, count: 1},
	}
	if len(entries) != len(want) {
		t.Fatalf("entries = %+v, want %+v", entries, want)
	}
	for i, w := range want {
		if entries[i] != w {
			t.Errorf("entries[%d] = %+v, want %+v", i, entries[i], w)
		}
	}
}

func TestVisibleTicketsAppliesTagFilter(t *testing.T) {
	m := seedTagBoard(t)

	m.tagFilter = "team"
	if got := len(m.visibleTickets(model.StatusTodo)); got != 1 {
		t.Errorf("team TODO count = %d, want 1", got)
	}
	// The filter applies to every column, including DONE.
	if got := len(m.visibleTickets(model.StatusDone)); got != 1 {
		t.Errorf("team DONE count = %d, want 1", got)
	}

	m.tagFilter = untaggedFilter
	if got := len(m.visibleTickets(model.StatusTodo)); got != 1 {
		t.Errorf("untagged TODO count = %d, want 1", got)
	}

	m.tagFilter = ""
	if got := len(m.visibleTickets(model.StatusTodo)); got != 3 {
		t.Errorf("unfiltered TODO count = %d, want 3", got)
	}
}

// Tags are matched case-insensitively everywhere else, so the filter has to
// agree — it shares Board.Filter with `kanban list --tag`.
func TestVisibleTicketsMatchesTagsCaseInsensitively(t *testing.T) {
	m := boardWith(t, seedCard{"shouty", model.StatusTodo, []string{"Backend"}})

	m.tagFilter = "backend"
	if got := len(m.visibleTickets(model.StatusTodo)); got != 1 {
		t.Errorf("count = %d, want 1 — tag matching should ignore case", got)
	}
}

// Applying a filter must clamp cursors into the shrunken columns, and the
// picker must reopen with the active filter highlighted.
func TestTagFilterClampsCursorsAndHighlights(t *testing.T) {
	m := seedTagBoard(t)
	m.focusedCol = 1 // TODO column, 3 tickets unfiltered
	m.cursors[1] = 2

	m.tagFilter = "customer"
	m.clampCursors()
	if m.cursors[1] != 0 {
		t.Errorf("cursor = %d, want 0 after filtering to 1 ticket", m.cursors[1])
	}
	if tk := m.selectedTicket(); tk == nil || tk.Title != "customer todo" {
		t.Errorf("selectedTicket = %+v, want customer todo", tk)
	}

	m.enterTagPicker()
	if m.view != tagPickerView {
		t.Fatalf("view = %d, want tagPickerView", m.view)
	}
	if got := m.tagEntries[m.tagIdx].value; got != "customer" {
		t.Errorf("picker opened on %q, want customer", got)
	}
}

// End-to-end through Update/View: press t, see the picker, pick a tag, see
// the filtered board and footer badge, then clear the filter again.
func TestTagPickerKeyFlow(t *testing.T) {
	m := seedTagBoard(t)

	press := func(r rune) {
		m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
	}
	enter := func() { m.Update(tea.KeyMsg{Type: tea.KeyEnter}) }

	if out := m.View(); !strings.Contains(out, "customer todo") {
		t.Fatalf("unfiltered board missing seeded card:\n%s", out)
	}

	press('t')
	out := m.View()
	for _, want := range []string{"Tags", "all tickets", "#customer", "#team", untaggedFilter} {
		if !strings.Contains(out, want) {
			t.Errorf("tag picker missing %q", want)
		}
	}
	// Checked against the popup rather than the screen: cards render their own
	// tags, so the DONE card behind the popup puts #ghost on screen either way.
	popup := m.renderTagPickerPopup(tagPickerPopupWidth(m.tagEntries), len(m.tagEntries)+2)
	if strings.Contains(popup, "#ghost") {
		t.Error("tag picker lists #ghost, which only exists on a DONE card")
	}

	press('j') // move from "all tickets" to #customer
	enter()    // apply
	out = m.View()
	if !strings.Contains(out, "customer todo") || strings.Contains(out, "team todo") {
		t.Errorf("filtered board wrong, want only customer cards:\n%s", out)
	}
	if !strings.Contains(out, "#customer") {
		t.Error("footer badge missing #customer")
	}

	press('t')
	press('k') // back up to "all tickets"
	enter()
	if out = m.View(); !strings.Contains(out, "team todo") {
		t.Error("clearing the filter did not restore the board")
	}
}

// Cursors index what's on screen, so following a moved card has to use the
// filtered column.
//
// The seed order matters. A same-board move only flips the ticket's status, so
// the card keeps its slice position, and clampCursors repairs an out-of-range
// index on its own — which hides the bug in any layout where the moved card
// ends up last. Two hidden cards ahead of it put the unfiltered index (2) inside
// the filtered column's range, so a wrong read survives clamping and lands the
// cursor on a different card.
func TestCommitMoveFollowsTheCardInTheFilteredColumn(t *testing.T) {
	m := boardWith(t,
		seedCard{"hidden one", model.StatusDoing, nil},
		seedCard{"hidden two", model.StatusDoing, nil},
		seedCard{"moved card", model.StatusTodo, []string{"customer"}},
		seedCard{"other customer", model.StatusDoing, []string{"customer"}},
	)
	moved, _ := m.board.FindByID("3")
	if moved == nil || moved.Title != "moved card" {
		t.Fatalf("seed lookup = %+v, want moved card", moved)
	}

	m.tagFilter = "customer"
	m.moveTicketID = moved.ID
	m.commitMove("", model.StatusDoing)

	doingCol := 2
	if m.focusedCol != doingCol {
		t.Fatalf("focusedCol = %d, want %d", m.focusedCol, doingCol)
	}
	if tk := m.selectedTicket(); tk == nil || tk.Title != "moved card" {
		t.Errorf("selectedTicket = %+v, want the card we just moved", tk)
	}
	if m.cursors[doingCol] != 0 {
		t.Errorf("cursor = %d, want 0 — the moved card is first in the filtered column", m.cursors[doingCol])
	}
}

// Reordering under a filter swaps the two cards you can see. Cards hidden
// between them keep their place rather than being dragged along.
func TestReorderUnderFilterSwapsVisibleNeighbours(t *testing.T) {
	m := boardWith(t,
		seedCard{"first team", model.StatusTodo, []string{"team"}},
		seedCard{"hidden middle", model.StatusTodo, []string{"other"}},
		seedCard{"second team", model.StatusTodo, []string{"team"}},
	)
	m.tagFilter = "team"
	m.focusedCol = 1
	m.cursors[1] = 0

	m.moveTicketInColumn(1)

	if m.cursors[1] != 1 {
		t.Errorf("cursor = %d, want 1", m.cursors[1])
	}
	var order []string
	for _, tk := range m.board.ByStatus(model.StatusTodo) {
		order = append(order, tk.Title)
	}
	want := []string{"second team", "hidden middle", "first team"}
	for i, w := range want {
		if i >= len(order) || order[i] != w {
			t.Fatalf("column order = %v, want %v", order, want)
		}
	}
}

// A filter held across a board switch would show an empty board, because tags
// don't carry between boards.
func TestSwitchingBoardsClearsTheFilter(t *testing.T) {
	m := seedTagBoard(t)
	m.tagFilter = "team"

	if err := store.CreateSprint("demo", ""); err != nil {
		t.Fatalf("create sprint: %v", err)
	}
	if err := m.switchBoard("demo"); err != nil {
		t.Fatalf("switch board: %v", err)
	}

	if m.tagFilter != "" {
		t.Errorf("tagFilter = %q, want it cleared by the switch", m.tagFilter)
	}
}

// The card is saved either way; the notice is there so it doesn't look like the
// add silently failed.
func TestAddingACardTheFilterHidesSaysSo(t *testing.T) {
	m := seedTagBoard(t)
	m.tagFilter = "customer"
	m.focusedCol = 1

	m.enterAddPopup()
	m.addTitle.SetValue("no tags on me")
	m.submitAdd()

	if !strings.Contains(m.notice, "hidden") {
		t.Errorf("notice = %q, want it to mention the card is hidden", m.notice)
	}
	if _, i := m.board.FindByID("7"); i < 0 {
		t.Error("the card was not saved")
	}
	if got := len(m.visibleTickets(model.StatusTodo)); got != 1 {
		t.Errorf("visible TODO = %d, want 1 — the new card stays hidden", got)
	}
}

func TestHiddenByFilter(t *testing.T) {
	m := seedTagBoard(t)
	tagged := &model.Ticket{Tags: []string{"customer"}}
	untagged := &model.Ticket{}

	m.tagFilter = ""
	if m.hiddenByFilter(tagged) || m.hiddenByFilter(untagged) {
		t.Error("nothing is hidden with no filter")
	}

	m.tagFilter = "customer"
	if m.hiddenByFilter(tagged) {
		t.Error("a card carrying the filtered tag is not hidden")
	}
	if !m.hiddenByFilter(untagged) {
		t.Error("an untagged card is hidden by a tag filter")
	}

	m.tagFilter = untaggedFilter
	if !m.hiddenByFilter(tagged) {
		t.Error("a tagged card is hidden by the untagged filter")
	}
	if m.hiddenByFilter(untagged) {
		t.Error("an untagged card is not hidden by the untagged filter")
	}
}
