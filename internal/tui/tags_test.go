package tui

import (
	"strings"
	"testing"

	"github.com/LeonY117/kanban-tui/internal/model"
	tea "github.com/charmbracelet/bubbletea"
)

// openTags walks the real route: tab opens the board picker, t switches it to
// the tag list.
func openTags(m *Model) {
	m.Update(tea.KeyMsg{Type: tea.KeyTab})
	m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("t")})
}

func TestTagPickerListsEveryTagWithItsCount(t *testing.T) {
	m, _ := boardWith(t, "a|TODO|cli,ui", "b|DOING|cli", "c|DONE|release", "d|TODO")

	openTags(m)

	if m.view != tagView {
		t.Fatalf("view = %v, want the tag popup", m.view)
	}
	got := map[string]int{}
	for _, c := range m.tags.tags {
		got[c.Tag] = c.Count
	}
	// A done card's tag is still a tag — the bug PR #5 shipped was building
	// this list from open cards only.
	want := map[string]int{"cli": 2, "ui": 1, "release": 1}
	if len(got) != len(want) {
		t.Fatalf("tags = %v, want %v", got, want)
	}
	for tag, n := range want {
		if got[tag] != n {
			t.Errorf("#%s = %d, want %d", tag, got[tag], n)
		}
	}
}

func TestTagPickerCountIsWhatSelectingItShows(t *testing.T) {
	m, _ := boardWith(t, "a|TODO|cli", "b|DONE|cli", "c|TODO|ui")

	openTags(m)
	offered := 0
	for _, c := range m.tags.tags {
		if c.Tag == "cli" {
			offered = c.Count
		}
	}

	m.tags.idx = 0
	for i, c := range m.tags.tags {
		if c.Tag == "cli" {
			m.tags.idx = i
		}
	}
	m.Update(tea.KeyMsg{Type: tea.KeyEnter})

	shown, _ := m.searchCounts()
	if shown != offered {
		t.Errorf("the picker offered %d cards, selecting it showed %d", offered, shown)
	}
	if offered != 2 {
		t.Errorf("offered %d, want 2 including the done card", offered)
	}
}

func TestTagPickerAppliesTheSameFilterAsTyping(t *testing.T) {
	m, _ := boardWith(t, "a|TODO|cli", "b|TODO|ui")

	openTags(m)
	for i, c := range m.tags.tags {
		if c.Tag == "cli" {
			m.tags.idx = i
		}
	}
	m.Update(tea.KeyMsg{Type: tea.KeyEnter})

	if m.view != boardView {
		t.Errorf("view = %v, want the popup closed", m.view)
	}
	if m.search.query != "#cli" {
		t.Errorf("query = %q, want the query you could have typed", m.search.query)
	}
	if got := titlesOf(m.visibleTickets(model.StatusTodo)); len(got) != 1 || got[0] != "a" {
		t.Errorf("Todo = %v, want the board filtered by the tag", got)
	}
	// Reopening the search shows the query, so it can be refined by hand.
	searchKeys(m, "/")
	if got := m.search.input.Value(); got != "#cli" {
		t.Errorf("input = %q, want the picked tag ready to edit", got)
	}
}

func TestTagPickerQuotesAMultiWordTag(t *testing.T) {
	m, _ := boardWith(t, "a|TODO|needs review", "b|TODO|cli")

	openTags(m)
	for i, c := range m.tags.tags {
		if c.Tag == "needs review" {
			m.tags.idx = i
		}
	}
	m.Update(tea.KeyMsg{Type: tea.KeyEnter})

	if m.search.query != `#"needs review"` {
		t.Errorf("query = %q, want the tag quoted so it stays one term", m.search.query)
	}
	if got := titlesOf(m.visibleTickets(model.StatusTodo)); len(got) != 1 || got[0] != "a" {
		t.Errorf("Todo = %v, want only the card carrying the tag", got)
	}
}

func TestTagPickerEscLeavesTheBoardAlone(t *testing.T) {
	m, _ := boardWith(t, "a|TODO|cli", "b|TODO|ui")

	openTags(m)
	m.Update(tea.KeyMsg{Type: tea.KeyEsc})

	// esc steps back to the board picker it was opened from, not past it.
	if m.view != pickerView {
		t.Errorf("view = %v, want the board picker back", m.view)
	}
	m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if m.view != boardView {
		t.Errorf("view = %v, want the board after a second esc", m.view)
	}
	if m.searchActive() {
		t.Error("esc applied a filter anyway")
	}
}

func TestTagPickerOpensOnTheActiveTag(t *testing.T) {
	m, _ := boardWith(t, "a|TODO|cli", "b|TODO|ui", "c|TODO|zed")

	typeSearch(m, "#ui")
	searchKeys(m, "enter")
	openTags(m)

	if got := m.tags.tags[m.tags.idx].Tag; got != "ui" {
		t.Errorf("cursor on %q, want the tag already filtering the board", got)
	}
}

func TestTagPickerRegistersClickZones(t *testing.T) {
	// PR #5's picker was keyboard-only, which broke the pattern every other
	// popup follows.
	m, _ := boardWith(t, "a|TODO|cli", "b|TODO|ui")
	openTags(m)
	m.View()

	found := false
	for _, z := range m.zones {
		if z.kind == zoneTagRow {
			found = true
		}
	}
	if !found {
		t.Fatal("no tag rows registered as click targets")
	}

	// A click on the second row must reach that row, not the board behind it.
	var second *hitZone
	for i := range m.zones {
		if m.zones[i].kind == zoneTagRow && m.zones[i].idx == 1 {
			second = &m.zones[i]
		}
	}
	if second == nil {
		t.Fatal("second tag row has no zone")
	}
	z := m.zoneAt(second.x, second.y)
	if z == nil || z.kind != zoneTagRow || z.idx != 1 {
		t.Errorf("zone at the second row = %+v, want the tag row on top of the backdrop", z)
	}
}

func TestTagPickerEmptyBoardSaysSo(t *testing.T) {
	m, _ := boardWith(t, "a|TODO", "b|TODO")
	openTags(m)

	if len(m.tags.tags) != 0 {
		t.Fatalf("tags = %v, want none", m.tags.tags)
	}
	if view := m.View(); !strings.Contains(view, "no tags") {
		t.Error("the empty picker gives no explanation")
	}
}

func TestTagPickerScrollsPastTheWindow(t *testing.T) {
	var specs []string
	for _, tag := range []string{"a", "b", "c", "d", "e", "f", "g", "h", "i", "j", "k", "l", "m", "n"} {
		specs = append(specs, "card "+tag+"|TODO|"+tag)
	}
	m, _ := boardWith(t, specs...)
	openTags(m)

	if len(m.tags.tags) != 14 {
		t.Fatalf("tags = %d, want 14", len(m.tags.tags))
	}
	// Walk to the last tag; the window has to follow.
	for i := 0; i < 13; i++ {
		m.Update(tea.KeyMsg{Type: tea.KeyDown})
	}
	if m.tags.idx != 13 {
		t.Fatalf("cursor at %d, want the last tag", m.tags.idx)
	}
	start, rows := m.tagPickerWindow()
	if m.tags.idx < start || m.tags.idx >= start+rows {
		t.Errorf("cursor %d outside the window [%d,%d)", m.tags.idx, start, start+rows)
	}
	if view := m.View(); !strings.Contains(view, "card n") && !strings.Contains(view, "#n") {
		t.Error("the last tag is not on screen after scrolling to it")
	}
}

func TestTagPickerIsReachedThroughTheBoardPicker(t *testing.T) {
	m, _ := boardWith(t, "a|TODO|cli")

	// It is no longer a top-level key: t on the board does nothing.
	m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("t")})
	if m.view != boardView {
		t.Fatalf("view = %v, want t on the board to do nothing", m.view)
	}

	m.Update(tea.KeyMsg{Type: tea.KeyTab})
	if m.view != pickerView {
		t.Fatalf("view = %v, want the board picker", m.view)
	}
	// And the picker's footer is where t is documented.
	if help := m.helpText(); !strings.Contains(help, "t tags") {
		t.Errorf("picker help %q does not offer t", help)
	}

	m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("t")})
	if m.view != tagView {
		t.Errorf("view = %v, want the tag list", m.view)
	}
}

func TestTagRowsCarryPerColumnCounts(t *testing.T) {
	// The rows mirror the board picker's shape, so the counts have to break
	// down by column and still sum to what picking the tag yields.
	m, _ := boardWith(t, "a|TODO|cli", "b|DOING|cli", "c|DONE|cli")

	openTags(m)
	if len(m.tags.tags) != 1 {
		t.Fatalf("tags = %v, want just cli", m.tags.tags)
	}
	got := m.tags.tags[0]

	sum := 0
	for _, n := range got.Counts {
		sum += n
	}
	if sum != got.Count {
		t.Errorf("per-column counts sum to %d, total says %d", sum, got.Count)
	}
	for _, s := range []model.Status{model.StatusTodo, model.StatusDoing, model.StatusDone} {
		if got.Counts[s] != 1 {
			t.Errorf("%s = %d, want 1", s, got.Counts[s])
		}
	}
}

func TestTagNamesAreNotTruncatedByTheCountsBlock(t *testing.T) {
	// Sizing the popup off the name alone left no room for the counts, so
	// every name collapsed to an ellipsis.
	m, _ := boardWith(t, "a|TODO|n")

	openTags(m)
	if view := m.View(); !strings.Contains(view, "#n") {
		t.Error("the tag name was truncated away by the counts block")
	}
}
