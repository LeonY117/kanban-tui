package tui

import (
	"strings"
	"testing"
)

// openAddWithPicker drives the model into the add popup with the emoji picker
// open, the way a user gets there: 'a', then ctrl+e.
func openAddWithPicker(t *testing.T, m *Model) {
	t.Helper()
	m.Update(keyPress("a"))
	if m.view != addView {
		t.Fatal("expected the add popup")
	}
	m.Update(keyPress("ctrl+e"))
	if m.view != emojiView || !m.emojiPick.open {
		t.Fatal("ctrl+e should open the emoji picker")
	}
}

// typeFilter enters filter mode with `/`, types q, and commits with enter —
// the board search grammar in miniature.
func typeFilter(t *testing.T, m *Model, q string) {
	t.Helper()
	m.Update(keyPress("/"))
	if !m.emojiPick.filtering {
		t.Fatal("/ should enter filter mode")
	}
	for _, r := range q {
		m.Update(keyPress(string(r)))
	}
	m.Update(keyPress("enter"))
	if m.emojiPick.filtering {
		t.Fatal("enter should commit the filter back to nav mode")
	}
}

func TestEmojiPickerFilterAndPick(t *testing.T) {
	m := testModel(t)
	openAddWithPicker(t, m)

	typeFilter(t, m, "locked")
	filtered := m.emojiFiltered()
	if len(filtered) == 0 || filtered[0].Emoji != "🔒" {
		t.Fatalf("filter 'locked' should surface 🔒 first, got %v", filtered[:2])
	}

	m.Update(keyPress("enter"))
	if m.view != addView {
		t.Error("picking should return to the add popup")
	}
	if got := m.addTitle.Value(); got != "🔒 " {
		t.Errorf("title = %q, want %q", got, "🔒 ")
	}
}

func TestEmojiPickerNavIsVim(t *testing.T) {
	m := testModel(t)
	openAddWithPicker(t, m)

	m.Update(keyPress("l"))
	if m.emojiPick.sel != 1 {
		t.Fatalf("l should move right, got %d", m.emojiPick.sel)
	}
	w, _ := m.emojiPickerSize()
	cols := emojiGridCols(w - 2)
	m.Update(keyPress("j"))
	if m.emojiPick.sel != 1+cols {
		t.Errorf("j should jump a full row (%d), got %d", 1+cols, m.emojiPick.sel)
	}
	m.Update(keyPress("k"))
	m.Update(keyPress("h"))
	if m.emojiPick.sel != 0 {
		t.Errorf("k+h should return to 0, got %d", m.emojiPick.sel)
	}

	// Letters outside hjkl must not filter — nav mode is not search mode.
	m.Update(keyPress("x"))
	if m.emojiPick.filter.Value() != "" {
		t.Error("typing in nav mode should not touch the filter")
	}
}

func TestEmojiPickerEscClearsFilterThenCloses(t *testing.T) {
	m := testModel(t)
	openAddWithPicker(t, m)

	typeFilter(t, m, "bug")
	m.Update(keyPress("esc"))
	if m.view != emojiView {
		t.Fatal("first esc should only clear the filter")
	}
	if m.emojiPick.filter.Value() != "" {
		t.Fatal("first esc should clear the filter text")
	}
	m.Update(keyPress("esc"))
	if m.view != addView {
		t.Error("second esc should close the picker")
	}
}

func TestEmojiPickerBackspacePastSlashExitsFilter(t *testing.T) {
	m := testModel(t)
	openAddWithPicker(t, m)

	m.Update(keyPress("/"))
	for _, r := range "ab" {
		m.Update(keyPress(string(r)))
	}
	m.Update(keyPress("backspace"))
	m.Update(keyPress("backspace"))
	if !m.emojiPick.filtering {
		t.Fatal("deleting query characters should stay in filter mode")
	}
	m.Update(keyPress("backspace")) // past the start: deletes the slash itself
	if m.emojiPick.filtering {
		t.Fatal("backspace on an empty query should drop back to nav mode")
	}
	if m.view != emojiView {
		t.Fatal("the picker itself should stay open")
	}
	m.Update(keyPress("l"))
	if m.emojiPick.sel != 1 {
		t.Error("nav keys should work immediately after")
	}
}

func TestEmojiPickerReplacesLeadEmoji(t *testing.T) {
	m := testModel(t)
	m.Update(keyPress("a"))
	for _, r := range "🗄️ Slice 2" {
		m.Update(keyPress(string(r)))
	}
	m.Update(keyPress("ctrl+e"))
	typeFilter(t, m, "package")
	m.Update(keyPress("enter"))

	if got := m.addTitle.Value(); got != "📦 Slice 2" {
		t.Errorf("title = %q, want %q", got, "📦 Slice 2")
	}
}

func TestEmojiPickerInsertsIntoDescription(t *testing.T) {
	m := testModel(t)
	m.Update(keyPress("a"))
	m.focusAddField(addFocusDesc)
	m.Update(keyPress("enter")) // start editing the textarea
	for _, r := range "ab" {
		m.Update(keyPress(string(r)))
	}
	m.Update(keyPress("ctrl+e"))
	if m.view != emojiView {
		t.Fatal("ctrl+e should open the picker from the description")
	}
	typeFilter(t, m, "bug")
	m.Update(keyPress("enter"))

	if got := m.addDesc.Value(); got != "ab🐛" {
		t.Errorf("description = %q, want %q", got, "ab🐛")
	}
	if !m.addDescEditing {
		t.Error("description editing should survive the round trip")
	}
}

func TestEmojiPickerFromSplitTitleEdit(t *testing.T) {
	m := testModel(t, "first")
	m.enterSplit()
	m.splitFocus = 1
	m.editField = 1
	m.Update(keyPress("e")) // start editing the title
	if !m.editTitle.Focused() {
		t.Fatal("expected the title editor focused")
	}

	m.Update(keyPress("ctrl+e"))
	if m.view != emojiView {
		t.Fatal("ctrl+e should open the picker from a detail title edit")
	}
	typeFilter(t, m, "bug")
	m.Update(keyPress("enter"))

	if m.view != splitView {
		t.Errorf("picker should return to the split view, got %v", m.view)
	}
	if got := m.editTitle.Value(); got != "🐛 first" {
		t.Errorf("edited title = %q, want %q", got, "🐛 first")
	}
	if !m.editTitle.Focused() {
		t.Error("title editing should survive the round trip")
	}
}

func TestEmojiPickerStickyMostUsedSection(t *testing.T) {
	m := testModel(t, "🔧 one", "🔧 two", "🐛 three", "plain")
	openAddWithPicker(t, m)

	list := m.emojiPick.list
	if len(list) < 2 || list[0].Emoji != "🔧" || list[1].Emoji != "🐛" {
		t.Fatalf("most-used should lead by frequency, got %v %v", list[0], list[1])
	}
	if list[0].Group != "Most used" {
		t.Errorf("sticky group = %q, want Most used", list[0].Group)
	}
	if view := m.View(); !strings.Contains(view, "─ most used") {
		t.Error("unfiltered picker should render the most-used header")
	}
}

func TestEmojiPickerSectionsCollapseWhileFiltering(t *testing.T) {
	m := testModel(t)
	openAddWithPicker(t, m)

	if view := m.View(); !strings.Contains(view, "─ smileys") {
		t.Error("unfiltered picker should render section headers")
	}
	m.Update(keyPress("/"))
	m.Update(keyPress("b"))
	if view := m.View(); strings.Contains(view, "─ ") {
		t.Error("a filtered picker should be a flat grid, no headers")
	}
}

func TestEmojiPickerKeywordSearch(t *testing.T) {
	m := testModel(t)
	openAddWithPicker(t, m)

	m.emojiPick.filter.SetValue("happy")
	found := false
	for _, e := range m.emojiFiltered() {
		if e.Emoji == "😀" || e.Emoji == "🙂" {
			found = true
		}
	}
	if !found {
		t.Error("'happy' should reach the smileys via keywords")
	}

	// Name matches outrank keyword matches: 🐛 is named bug, everything else
	// merely mentions it.
	m.emojiPick.filter.SetValue("bug")
	if got := m.emojiFiltered()[0].Emoji; got != "🐛" {
		t.Errorf("'bug' should rank 🐛 first, got %s", got)
	}
}

func TestEmojiPickerClickPicks(t *testing.T) {
	m := testModel(t)
	openAddWithPicker(t, m)
	m.View() // register zones

	z := zoneOf(t, m, zoneEmojiCell, 0, 2)
	m.mouseClick(mouseAt(z.x, z.y))

	if m.view != addView {
		t.Error("a click on a cell should pick and close")
	}
	if got := m.addTitle.Value(); !strings.HasSuffix(got, " ") || got == " " {
		t.Errorf("clicked emoji should land as the title prefix, got %q", got)
	}
}

func TestEmojiPickerViewShowsGridAndName(t *testing.T) {
	m := testModel(t)
	openAddWithPicker(t, m)

	view := m.View()
	if !strings.Contains(view, "/ to filter") {
		t.Error("nav-mode picker should point at / in its title")
	}
	if !strings.Contains(view, m.emojiPick.list[0].Name) {
		t.Errorf("picker should show the selected emoji's name %q", m.emojiPick.list[0].Name)
	}
}
