package tui

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

// Every list selects on the first click and acts on a second click of the row
// already under the cursor. One click that did both meant the mouse could never
// be used to look around.

func TestClickSelectsACardBeforeOpeningIt(t *testing.T) {
	m := testModel(t, "first", "second")
	m.View()

	second := zoneOf(t, m, zoneTicket, 1, 1)
	m.mouseClick(mouseAt(second.x, second.y))
	if m.cursors[1] != 1 {
		t.Fatalf("cursor = %d, want the clicked card selected", m.cursors[1])
	}
	if m.view != boardView {
		t.Fatalf("view = %v, want the first click to select only", m.view)
	}

	m.mouseClick(mouseAt(second.x, second.y))
	if m.view != splitView {
		t.Errorf("view = %v, want the second click to open the card", m.view)
	}
	if sel := m.selectedTicket(); sel == nil || sel.Title != "second" {
		t.Errorf("opened %v, want the card that was clicked", sel)
	}
}

func TestClickSelectsABoardBeforeSwitchingToIt(t *testing.T) {
	m, _ := boardWith(t, "a|TODO")
	withSprint(t, "demo")

	m.enterPicker()
	m.View()

	demo := zoneOf(t, m, zonePickerRow, 0, 1)
	m.mouseClick(mouseAt(demo.x, demo.y))
	if m.pickerIdx != 1 {
		t.Fatalf("pickerIdx = %d, want the clicked board selected", m.pickerIdx)
	}
	if m.view != pickerView {
		t.Fatalf("view = %v, want the popup still open", m.view)
	}

	m.mouseClick(mouseAt(demo.x, demo.y))
	if m.sprintName != "demo" {
		t.Errorf("board = %q, want the second click to switch", m.sprintName)
	}
}

func TestClickSelectsATagBeforeFilteringByIt(t *testing.T) {
	m, _ := boardWith(t, "a|TODO|cli", "b|TODO|ui")

	openTags(m)
	m.View()

	row := zoneOf(t, m, zoneTagRow, 0, 1) // the first tag, under "all tickets"
	m.mouseClick(mouseAt(row.x, row.y))
	if m.view != tagView {
		t.Fatalf("view = %v, want the list still open", m.view)
	}
	if m.searchActive() {
		t.Fatal("the first click applied a filter")
	}

	m.mouseClick(mouseAt(row.x, row.y))
	if m.search.query == "" {
		t.Error("the second click did not apply the tag")
	}
}

// The meta bar is three fields on one line. A click used to land on the panel
// as a whole, leaving h and l as the only way to reach the field you wanted.
func TestClickPicksAMetaField(t *testing.T) {
	m := testModel(t, "first")
	m.enterSplit()
	m.splitFocus = 1
	m.View()

	tags := zoneOf(t, m, zoneMetaField, 0, 2)
	m.mouseClick(mouseAt(tags.x, tags.y))
	if m.editField != 0 || m.metaIdx != 2 {
		t.Fatalf("editField %d metaIdx %d, want the tags field selected", m.editField, m.metaIdx)
	}
	if m.inputMode != inputNone {
		t.Fatal("the first click started editing")
	}

	m.mouseClick(mouseAt(tags.x, tags.y))
	if m.inputMode == inputNone || m.input.Prompt != "Tags: " {
		t.Errorf("inputMode %v prompt %q, want the second click to edit the tags",
			m.inputMode, m.input.Prompt)
	}
}

// Title and description take a second click into the editor, the way they take
// enter — and a click inside an editor already open leaves it alone rather than
// saving and reopening it under the cursor.
func TestClickAgainEditsTitleAndDescription(t *testing.T) {
	for _, tc := range []struct {
		name    string
		field   int
		editing func(m *Model) bool
	}{
		{"title", 1, func(m *Model) bool { return m.editTitle.Focused() }},
		{"description", 2, func(m *Model) bool { return m.editDesc.Focused() }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m := testModel(t, "first")
			m.enterSplit()
			m.View()

			panel := zoneOf(t, m, zoneField, 0, tc.field)
			m.mouseClick(mouseAt(panel.x+1, panel.y+1))
			if m.splitFocus != 1 || m.editField != tc.field {
				t.Fatalf("splitFocus %d editField %d, want the panel selected from the list",
					m.splitFocus, m.editField)
			}
			if tc.editing(m) {
				t.Fatal("the click that crossed from the list started editing")
			}

			m.mouseClick(mouseAt(panel.x+1, panel.y+1))
			if !tc.editing(m) {
				t.Fatal("the second click did not open the editor")
			}

			m.mouseClick(mouseAt(panel.x+1, panel.y+1))
			if !tc.editing(m) {
				t.Error("a click inside the open editor closed it")
			}
		})
	}
}

func TestClickFocusesAnAddPopupField(t *testing.T) {
	m := testModel(t, "first")
	m.enterAddPopup()
	m.View()

	tags := zoneOf(t, m, zoneAddField, 0, addFocusTags)
	m.mouseClick(mouseAt(tags.x, tags.y))
	if m.addFocusIdx != addFocusTags {
		t.Fatalf("focus = %d, want the tags field", m.addFocusIdx)
	}

	// The description is the one field with two states: selected, then typed
	// into — so it takes a second click, the way it takes enter.
	m.View()
	desc := zoneOf(t, m, zoneAddField, 0, addFocusDesc)
	m.mouseClick(mouseAt(desc.x, desc.y))
	if m.addFocusIdx != addFocusDesc || m.addDescEditing {
		t.Fatalf("focus %d editing %v, want the description selected only", m.addFocusIdx, m.addDescEditing)
	}
	m.mouseClick(mouseAt(desc.x, desc.y))
	if !m.addDescEditing {
		t.Error("the second click did not open the description editor")
	}
}

func TestClickPicksARenameFormField(t *testing.T) {
	m, _ := boardWith(t, "a|TODO")
	withSprint(t, "demo")

	m.enterPicker()
	m.pickerIdx = 1
	m.startPickerRename()
	m.View()

	prefix := zoneOf(t, m, zoneRenameField, 0, renameFocusPrefix)
	m.mouseClick(mouseAt(prefix.x, prefix.y))
	if m.renameFocus != renameFocusPrefix {
		t.Fatalf("focus = %d, want the prefix field", m.renameFocus)
	}
	// The highlight is not the point — a blurred input drops every key it is
	// handed, so moving the label alone left the user typing into nothing.
	before := m.renamePrefix.Value()
	m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("Z")})
	if m.renamePrefix.Value() == before {
		t.Errorf("prefix still %q after typing — the clicked field never took the keyboard", before)
	}
}

// The Info panel's fields are only clickable while it holds the keyboard.
// Focusing it fills its empty slots with `+assign` / `+tag` prompts, which
// shifts the fields right of them: a zone registered before that reflow put the
// second click of a pair on the field next door.
func TestMetaFieldsAreClickableOnlyWhileTheyHoldStill(t *testing.T) {
	m := testModel(t, "first")
	m.enterSplit()
	m.View()

	for _, z := range m.zones {
		if z.kind == zoneMetaField {
			t.Fatalf("meta field %d is a click target while the list has focus", z.idx)
		}
	}

	m.splitFocus = 1
	m.View()
	found := false
	for _, z := range m.zones {
		if z.kind == zoneMetaField {
			found = true
		}
	}
	if !found {
		t.Error("no meta fields are clickable once the panel has focus")
	}
}

// Nothing acts on a first click, however the surface happened to open. Popups
// that preselect a row — the move popup's current column, the picker's current
// board, the tag list's active filter — would otherwise act on click one.
func TestAFirstClickNeverActs(t *testing.T) {
	m, _ := boardWith(t, "a|TODO|cli", "b|TODO|ui")

	openTags(m)
	m.View()
	active := zoneOf(t, m, zoneTagRow, 0, m.tags.idx)
	m.mouseClick(mouseAt(active.x, active.y))
	if m.view != tagView {
		t.Errorf("the tag list acted on the first click of its preselected row")
	}
}

// The archive browser is a list like any other: a borrowed row follows itself
// home on the second click, which is what enter does to it.
func TestClickAgainFollowsABorrowedArchiveRowHome(t *testing.T) {
	m, _ := boardWith(t, "local|TODO")
	s := withSprint(t, "demo", "remote|TODO")
	remote, err := s.Load()
	if err != nil {
		t.Fatal(err)
	}
	if err := s.ArchiveByID(remote.Tickets[0].ID); err != nil {
		t.Fatal(err)
	}

	m.enterArchive()
	m.archiveSearch.global = true
	if !m.loadForeignArchive() {
		t.Fatal("could not borrow the other board's archive")
	}
	m.View()

	row := zoneOf(t, m, zoneArchiveRow, 0, m.archiveCursor)
	m.mouseClick(mouseAt(row.x, row.y))
	if m.sprintName != "" {
		t.Fatalf("board = %q, want the first click to select only", m.sprintName)
	}
	m.mouseClick(mouseAt(row.x, row.y))
	if m.sprintName != "demo" {
		t.Errorf("board = %q, want the second click to follow the row home", m.sprintName)
	}
}
