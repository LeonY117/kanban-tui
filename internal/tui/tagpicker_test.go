package tui

import (
	"strings"
	"testing"

	"github.com/LeonY117/kanban-tui/internal/model"
	"github.com/LeonY117/kanban-tui/internal/store"
	tea "github.com/charmbracelet/bubbletea"
)

// seedTagBoard builds a board with a mix of tagged, untagged, and DONE cards.
func seedTagBoard(t *testing.T) *Model {
	t.Helper()
	st := store.New(t.TempDir())
	seed := []struct {
		title  string
		status model.Status
		tags   []string
	}{
		{"team todo", model.StatusTodo, []string{"team"}},
		{"team doing", model.StatusDoing, []string{"team"}},
		{"customer todo", model.StatusTodo, []string{"customer"}},
		{"untagged todo", model.StatusTodo, nil},
		{"done team", model.StatusDone, []string{"team"}},
		{"done only-tag", model.StatusDone, []string{"ghost"}},
	}
	for _, s := range seed {
		if _, err := st.Add(s.title, "", s.status, s.tags, "", ""); err != nil {
			t.Fatalf("seeding %q: %v", s.title, err)
		}
	}
	m, err := NewModel(st, "")
	if err != nil {
		t.Fatal(err)
	}
	return m
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

func TestTicketsForAppliesTagFilter(t *testing.T) {
	m := seedTagBoard(t)

	m.tagFilter = "team"
	if got := len(m.ticketsFor(model.StatusTodo)); got != 1 {
		t.Errorf("team TODO count = %d, want 1", got)
	}
	// The filter applies to every column, including DONE.
	if got := len(m.ticketsFor(model.StatusDone)); got != 1 {
		t.Errorf("team DONE count = %d, want 1", got)
	}

	m.tagFilter = untaggedFilter
	if got := len(m.ticketsFor(model.StatusTodo)); got != 1 {
		t.Errorf("untagged TODO count = %d, want 1", got)
	}

	m.tagFilter = ""
	if got := len(m.ticketsFor(model.StatusTodo)); got != 3 {
		t.Errorf("unfiltered TODO count = %d, want 3", got)
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
	m.width, m.height = 160, 40
	m.ready = true

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
	// The board backdrop renders card tags, so #ghost can appear on the DONE
	// card behind the popup — check the picker's own rows instead.
	for _, e := range m.tagEntries {
		if e.value == "ghost" {
			t.Error("tag picker lists ghost, which only exists on a DONE card")
		}
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
