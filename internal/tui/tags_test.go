package tui

import (
	"strings"
	"testing"

	"github.com/LeonY117/kanban-tui/internal/model"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// openTags walks the real route: tab opens the board picker, t switches it to
// the tag list.
func openTags(m *Model) {
	m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("#")})
}

func TestTagPickerListsEveryTagWithItsCount(t *testing.T) {
	m, _ := boardWith(t, "a|TODO|cli,ui", "b|DOING|cli", "c|DONE|release", "d|TODO")

	openTags(m)

	if m.view != tagView {
		t.Fatalf("view = %v, want the tag popup", m.view)
	}
	got := map[string]int{}
	for _, r := range tagRowsOnly(m) {
		got[r.label] = totalOf(r)
	}
	// A done card's tag is still a tag — the bug PR #5 shipped was building
	// this list from open cards only.
	want := map[string]int{"#cli": 2, "#ui": 1, "#release": 1}
	if len(got) != len(want) {
		t.Fatalf("tags = %v, want %v", got, want)
	}
	for tag, n := range want {
		if got[tag] != n {
			t.Errorf("%s = %d, want %d", tag, got[tag], n)
		}
	}
}

func TestTagPickerCountIsWhatSelectingItShows(t *testing.T) {
	m, _ := boardWith(t, "a|TODO|cli", "b|DONE|cli", "c|TODO|ui")

	openTags(m)
	offered := 0
	for _, r := range m.tags.rows {
		if r.label == "#cli" {
			offered = totalOf(r)
		}
	}
	selectTagRow(t, m, "#cli")
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
	selectTagRow(t, m, "#cli")
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
	selectTagRow(t, m, `#"needs review"`)
	m.Update(tea.KeyMsg{Type: tea.KeyEnter})

	if m.search.query != `#"needs review"` {
		t.Errorf("query = %q, want the tag quoted so it stays one term", m.search.query)
	}
	if got := titlesOf(m.visibleTickets(model.StatusTodo)); len(got) != 1 || got[0] != "a" {
		t.Errorf("Todo = %v, want only the card carrying the tag", got)
	}
}

func TestTagPickerEscLeavesTheBoardAlone(t *testing.T) {
	// One press, one exit. The list is opened through the board picker, but
	// esc goes to the board rather than back to that picker: the point of the
	// list is the board it filters, and "up one level" is a meaning esc
	// carries nowhere else in this TUI.
	for _, k := range []tea.KeyType{tea.KeyEsc, tea.KeyTab} {
		m, _ := boardWith(t, "a|TODO|cli", "b|TODO|ui")

		openTags(m)
		m.Update(tea.KeyMsg{Type: k})

		if m.view != boardView {
			t.Errorf("%v: view = %v, want the board in one press", k, m.view)
		}
		if m.searchActive() {
			t.Errorf("%v: leaving applied a filter anyway", k)
		}
	}
}

func TestTagPickerOpensOnTheActiveTag(t *testing.T) {
	m, _ := boardWith(t, "a|TODO|cli", "b|TODO|ui", "c|TODO|zed")

	typeSearch(m, "#ui")
	searchKeys(m, "enter")
	openTags(m)

	if got := m.tags.rows[m.tags.idx].label; got != "#ui" {
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

	if got := tagRowsOnly(m); len(got) != 0 {
		t.Fatalf("tags = %v, want none", got)
	}
	// The bookends still stand: everything, and the everything that is untagged.
	if view := m.View(); !strings.Contains(view, "all tickets") || !strings.Contains(view, "no tags") {
		t.Error("the empty picker lost its bookends")
	}
}

func TestTagPickerScrollsPastTheWindow(t *testing.T) {
	var specs []string
	for _, tag := range []string{"a", "b", "c", "d", "e", "f", "g", "h", "i", "j", "k", "l", "m", "n"} {
		specs = append(specs, "card "+tag+"|TODO|"+tag)
	}
	m, _ := boardWith(t, specs...)
	openTags(m)

	if got := len(tagRowsOnly(m)); got != 14 {
		t.Fatalf("tags = %d, want 14", got)
	}
	// Walk to the last row; the window has to follow.
	for i := 0; i < len(m.tags.rows); i++ {
		m.Update(tea.KeyMsg{Type: tea.KeyDown})
	}
	if want := len(m.tags.rows) - 1; m.tags.idx != want {
		t.Fatalf("cursor at %d, want the last row %d", m.tags.idx, want)
	}
	// A short terminal is what forces a window at all — at 40 rows the whole
	// list fits, which is the point of dropping the fixed 12-row cap.
	m.height = 14
	_, height := m.listPopupSize(0, len(m.tags.rows))
	start, rows := m.tagPickerWindow(height)
	if rows >= len(m.tags.rows) {
		t.Fatalf("setup: %d rows fit of %d, wanted a window", rows, len(m.tags.rows))
	}
	if m.tags.idx < start || m.tags.idx >= start+rows {
		t.Errorf("cursor %d outside the window [%d,%d)", m.tags.idx, start, start+rows)
	}
	if view := m.View(); !strings.Contains(view, "no tags") {
		t.Error("the last row is not on screen after scrolling to it")
	}

	// And at full height it does not window at all.
	m.height = 40
	if _, h := m.listPopupSize(0, len(m.tags.rows)); h < len(m.tags.rows)+2 {
		t.Errorf("height %d truncates %d rows on a tall terminal", h, len(m.tags.rows))
	}
}

func TestTagListAndBoardListAreTheSameShape(t *testing.T) {
	// They are one object at two moments — same rows, same marker, same
	// right-aligned counts — so a reader should not be able to tell them apart
	// by size. They used to differ by 16 columns of minimum width and a hard
	// 12-row cap that the board list never had.
	m, _ := boardWith(t, "a|TODO|cli", "b|TODO|ui")
	withSprint(t, "demo", "remote|TODO|cli")

	m.Update(tea.KeyMsg{Type: tea.KeyTab})
	boardPopup := m.View()
	m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("t")})
	tagPopup := m.View()

	widthOf := func(view string) int {
		widest := 0
		for _, line := range strings.Split(view, "\n") {
			if strings.ContainsAny(line, "╭╰│") {
				if w := lipgloss.Width(strings.TrimRight(line, " ")); w > widest {
					widest = w
				}
			}
		}
		return widest
	}
	b, tg := widthOf(boardPopup), widthOf(tagPopup)
	if b == 0 || tg == 0 {
		t.Fatalf("setup: measured no popup border (board %d, tag %d)", b, tg)
	}
	if b != tg {
		t.Errorf("board popup renders %d cells wide, tag popup %d — same rows, same rule", b, tg)
	}
}

// The tag list is one action on one key, reachable from wherever a filter makes
// sense — the board itself and the board picker. It used to be picker-only on
// `t`, which put two keystrokes between a board and the tags filtering it.
func TestTagPickerOpensOnHashFromBoardAndPicker(t *testing.T) {
	for _, tc := range []struct {
		name string
		open func(m *Model)
	}{
		{"board", func(m *Model) {}},
		{"picker", func(m *Model) { m.Update(tea.KeyMsg{Type: tea.KeyTab}) }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m, _ := boardWith(t, "a|TODO|cli")
			tc.open(m)

			if help := m.helpText(); !strings.Contains(help, "# tags") {
				t.Errorf("help %q does not offer #", help)
			}
			m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("#")})
			if m.view != tagView {
				t.Fatalf("view = %v, want the tag list", m.view)
			}
		})
	}
}

func TestTagRowsCarryPerColumnCounts(t *testing.T) {
	// The rows mirror the board picker's shape, so the counts have to break
	// down by column and still sum to what picking the tag yields.
	m, _ := boardWith(t, "a|TODO|cli", "b|DOING|cli", "c|DONE|cli")

	openTags(m)
	rows := tagRowsOnly(m)
	if len(rows) != 1 {
		t.Fatalf("tags = %v, want just cli", rows)
	}
	got := rows[0]
	for _, s := range []model.Status{model.StatusTodo, model.StatusDoing, model.StatusDone} {
		if got.counts[s] != 1 {
			t.Errorf("%s = %d, want 1", s, got.counts[s])
		}
	}
	if totalOf(got) != 3 {
		t.Errorf("counts sum to %d, want 3", totalOf(got))
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

// tagRowsOnly drops the two bookends, leaving the real tags.
func tagRowsOnly(m *Model) []tagRow {
	var out []tagRow
	for _, r := range m.tags.rows {
		if r.query != "" && r.query != model.Untagged {
			out = append(out, r)
		}
	}
	return out
}

func totalOf(r tagRow) int {
	n := 0
	for _, c := range r.counts {
		n += c
	}
	return n
}

func selectTagRow(t *testing.T, m *Model, label string) {
	t.Helper()
	for i, r := range m.tags.rows {
		if r.label == label {
			m.tags.idx = i
			return
		}
	}
	t.Fatalf("no row labelled %q in %v", label, m.tags.rows)
}

func TestTagListIsBookendedByAllAndNone(t *testing.T) {
	m, _ := boardWith(t, "a|TODO|cli", "b|DOING|cli", "c|TODO", "d|DONE")

	openTags(m)

	first, last := m.tags.rows[0], m.tags.rows[len(m.tags.rows)-1]
	if first.label != "all tickets" || first.query != "" {
		t.Errorf("first row = %+v, want all tickets clearing the filter", first)
	}
	if last.label != "no tags" || last.query != model.Untagged {
		t.Errorf("last row = %+v, want no tags selecting the untagged", last)
	}
	if got := totalOf(first); got != 4 {
		t.Errorf("all tickets counts %d, want every card", got)
	}
	if got := totalOf(last); got != 2 {
		t.Errorf("no tags counts %d, want the two untagged cards", got)
	}
}

func TestPickingNoTagsFiltersToUntagged(t *testing.T) {
	m, _ := boardWith(t, "tagged one|TODO|cli", "bare one|TODO", "bare two|TODO")

	openTags(m)
	selectTagRow(t, m, "no tags")
	offered := totalOf(m.tags.rows[m.tags.idx])
	m.Update(tea.KeyMsg{Type: tea.KeyEnter})

	got := titlesOf(m.visibleTickets(model.StatusTodo))
	if len(got) != 2 || got[0] != "bare one" || got[1] != "bare two" {
		t.Errorf("Todo = %v, want only the untagged cards", got)
	}
	shown, _ := m.searchCounts()
	if shown != offered {
		t.Errorf("the row offered %d cards, selecting it showed %d", offered, shown)
	}
}

func TestPickingAllTicketsClearsTheFilter(t *testing.T) {
	m, _ := boardWith(t, "a|TODO|cli", "b|TODO")

	openTags(m)
	selectTagRow(t, m, "#cli")
	m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if !m.searchActive() {
		t.Fatal("setup: expected a filter")
	}

	openTags(m)
	selectTagRow(t, m, "all tickets")
	m.Update(tea.KeyMsg{Type: tea.KeyEnter})

	if m.searchActive() {
		t.Error("all tickets left a filter standing")
	}
	if got := titlesOf(m.visibleTickets(model.StatusTodo)); len(got) != 2 {
		t.Errorf("Todo = %v, want the whole column back", got)
	}
}

func TestFilterRidesInTheFooterBadge(t *testing.T) {
	m, _ := boardWith(t, "a|TODO|cli", "b|TODO")

	openTags(m)
	selectTagRow(t, m, "#cli")
	m.Update(tea.KeyMsg{Type: tea.KeyEnter})

	if got := m.filterBadge(); !strings.Contains(got, "#cli") {
		t.Errorf("badge = %q, want the tag beside the board name", got)
	}
	// The badge sits between the board name and the id prefix, and survives
	// however narrow the terminal gets — it is never fed to fitHints.
	m.width = 52
	footer := m.footerLine()
	if !strings.Contains(footer, "#cli") {
		t.Errorf("footer at 52 cols lost the filter: %q", footer)
	}
	name := strings.Index(footer, "main")
	tag := strings.Index(footer, "#cli")
	prefix := strings.LastIndex(footer, "[")
	if !(name < tag && tag < prefix) {
		t.Errorf("footer order wrong (name %d, filter %d, prefix %d): %q", name, tag, prefix, footer)
	}
}

func TestNoTagsReadsAsWordsInTheBadge(t *testing.T) {
	m, _ := boardWith(t, "a|TODO|cli", "b|TODO")

	openTags(m)
	selectTagRow(t, m, "no tags")
	m.Update(tea.KeyMsg{Type: tea.KeyEnter})

	if got := m.filterBadge(); !strings.Contains(got, "no tags") {
		t.Errorf("badge = %q, want it to read as words rather than -#", got)
	}
}
