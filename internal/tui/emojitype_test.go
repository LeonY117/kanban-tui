package tui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"

	"github.com/LeonY117/kanban-tui/internal/emoji"
)

func emojiFragileFor(s string) bool { return len(emoji.Fragile(s)) != 0 }

// typeText drives a whole string through the model one keystroke at a time.
func typeText(m *Model, s string) {
	for _, r := range s {
		m.Update(keyPress(string(r)))
	}
}

func TestTypeaheadPicksIntoATitle(t *testing.T) {
	m := testModel(t)
	m.Update(keyPress("a"))
	typeText(m, "Slice 2 :fir")

	if !m.typeaheadShowing() {
		t.Fatal("`:fir` should be showing suggestions")
	}
	// Shortest shortcode wins inside a bucket, so :fire: leads :fireworks:.
	if got := m.emojiType.matches[0].Emoji; got != "🔥" {
		t.Errorf("first match = %s, want 🔥", got)
	}

	m.Update(keyPress("enter"))
	if got := m.addTitle.Value(); got != "Slice 2 🔥" {
		t.Errorf("title = %q, want %q", got, "Slice 2 🔥")
	}
	if m.emojiType.active {
		t.Error("accepting should close the typeahead")
	}
	if m.view != addView {
		t.Error("enter was consumed by the typeahead, so it must not also submit")
	}
}

// tab is the other accept key, and down moves the selection first.
func TestTypeaheadDownThenTab(t *testing.T) {
	m := testModel(t)
	m.Update(keyPress("a"))
	typeText(m, ":fir")
	second := m.emojiType.matches[1].Emoji

	m.Update(keyPress("down"))
	m.Update(keyPress("tab"))

	if got := m.addTitle.Value(); got != second {
		t.Errorf("title = %q, want the second suggestion %q", got, second)
	}
}

// The whole point of the min-length and match gates: until the popup is up,
// the field behaves exactly as it did before this existed.
func TestTypeaheadLeavesOrdinaryTypingAlone(t *testing.T) {
	m := testModel(t)
	m.Update(keyPress("a"))
	typeText(m, ":wip")
	if m.typeaheadShowing() {
		t.Fatal(":wip matches nothing, so nothing should be showing")
	}

	m.Update(keyPress("enter")) // must still create the ticket
	if m.view == addView {
		t.Fatal("enter should have submitted the ticket")
	}
	board, _ := m.store.Load()
	if len(board.Tickets) != 1 || board.Tickets[0].Title != ":wip" {
		t.Errorf("board holds %+v, want one ticket titled :wip", board.Tickets)
	}
}

// A shortcode ends at a space, the way it does everywhere else.
func TestTypeaheadDropsOnSpace(t *testing.T) {
	m := testModel(t)
	m.Update(keyPress("a"))
	typeText(m, ":fire")
	if !m.typeaheadShowing() {
		t.Fatal("setup: expected suggestions")
	}
	m.Update(keyPress(" "))
	if m.typeaheadShowing() || m.emojiType.active {
		t.Error("a space should end the shortcode")
	}
}

// esc dismisses the list without eating the text — `:wip` stays typed.
func TestTypeaheadEscKeepsTheText(t *testing.T) {
	m := testModel(t)
	m.Update(keyPress("a"))
	typeText(m, ":fire")
	m.Update(keyPress("esc"))

	if m.typeaheadShowing() {
		t.Error("esc should dismiss the list")
	}
	if got := m.addTitle.Value(); got != ":fire" {
		t.Errorf("title = %q, want the typed text left alone", got)
	}
	if m.view != addView {
		t.Error("esc was consumed by the list, so it must not also close the popup")
	}
}

// Backspacing over the ':' ends it, like backspacing past the board search's
// slash. Backspacing within the query just narrows back.
func TestTypeaheadBackspaceShortensThenEnds(t *testing.T) {
	m := testModel(t)
	m.Update(keyPress("a"))
	typeText(m, ":fire")
	m.Update(keyPress("backspace"))
	if m.emojiType.query != "fir" {
		t.Errorf("query = %q, want fir", m.emojiType.query)
	}
	for i := 0; i < 3; i++ {
		m.Update(keyPress("backspace"))
	}
	if !m.emojiType.active || m.emojiType.query != "" {
		t.Fatalf("expected an armed, empty query, got active=%v %q", m.emojiType.active, m.emojiType.query)
	}
	m.Update(keyPress("backspace")) // deletes the ':' itself
	if m.emojiType.active {
		t.Error("backspacing over the colon should end the shortcode")
	}
}

// The description is a textarea, which has no splice — the deletion goes
// through the same backspaces a user would type.
func TestTypeaheadInDescription(t *testing.T) {
	m := testModel(t)
	m.Update(keyPress("a"))
	m.focusAddField(addFocusDesc)
	m.Update(keyPress("enter"))
	typeText(m, "ship it :fire")
	if !m.typeaheadShowing() {
		t.Fatal("setup: expected suggestions in the description")
	}
	m.Update(keyPress("enter"))

	if got := m.addDesc.Value(); got != "ship it 🔥" {
		t.Errorf("description = %q, want %q", got, "ship it 🔥")
	}
	if !m.addDescEditing {
		t.Error("editing should continue after accepting")
	}
}

// The typeahead draws from the same allow-list the picker does, so it can no
// more skew a board than the picker can. Membership, not just the width
// property — a match has to be one of the 997, however it was found.
func TestTypeaheadOffersOnlySafeEmoji(t *testing.T) {
	safe := make(map[string]bool, len(emoji.Safe))
	for _, e := range emoji.Safe {
		safe[e.Emoji] = true
	}
	for _, q := range []string{"war", "pen", "card", "file", "fla", "point", "hand"} {
		matches := typeaheadMatches(q)
		if len(matches) == 0 {
			t.Errorf("%q matched nothing — the probe has stopped proving anything", q)
		}
		for _, e := range matches {
			if !safe[e.Emoji] {
				t.Errorf("%q offered %s, which is not in the safe set", q, e.Emoji)
			}
			if emojiFragileFor(e.Emoji) {
				t.Errorf("%q offered fragile emoji %s", q, e.Emoji)
			}
		}
	}
}

// The selection marks the glyph and nothing else — not the spaces around it,
// not the colons. Asserted against the escape codes, since that is the only
// place the distinction exists.
func TestTypeaheadHighlightsOnlyTheEmoji(t *testing.T) {
	old := lipgloss.ColorProfile()
	lipgloss.SetColorProfile(termenv.ANSI)
	defer lipgloss.SetColorProfile(old)

	m := testModel(t)
	m.Update(keyPress("a"))
	typeText(m, ":fir")
	list, _, _ := m.renderTypeaheadList()

	const reverse = "\x1b[7m"
	if n := strings.Count(list, reverse); n != 1 {
		t.Fatalf("reverse video appears %d times, want exactly one — the selected glyph", n)
	}
	want := reverse + m.emojiType.matches[0].Emoji + "\x1b[0m"
	if !strings.Contains(list, want) {
		t.Errorf("the highlight should wrap just %q", m.emojiType.matches[0].Emoji)
	}
}

func TestTypeaheadRendersTheList(t *testing.T) {
	m := testModel(t)
	m.Update(keyPress("a"))
	typeText(m, ":fir")

	view := m.View()
	if !strings.Contains(view, ":fire:") {
		t.Error("the list should show the :shortcode: form")
	}
	if !strings.Contains(view, "🔥") {
		t.Error("the list should show the emoji itself")
	}
}
