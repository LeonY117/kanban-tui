package tui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"

	"github.com/LeonY117/kanban-tui/internal/model"
	"github.com/LeonY117/kanban-tui/internal/store"
)

// setDesc writes a description straight to a board's store, standing in for
// whatever set it — the CLI, another agent, an earlier session.
func setDesc(t *testing.T, sprintName, desc string) {
	t.Helper()
	s, err := boardStore(sprintName)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.SetDescription(desc); err != nil {
		t.Fatal(err)
	}
}

func TestInfoOpensOnTheCurrentBoard(t *testing.T) {
	m := testModel(t, "a ticket")
	setDesc(t, "", "The main board.\n\nCatch-all for loose work.")
	m.reload()

	m.Update(keyPress("i"))
	if m.view != infoView {
		t.Fatalf("view = %v, want infoView", m.view)
	}
	view := m.View()
	for _, want := range []string{"main", "Catch-all for loose work."} {
		if !strings.Contains(view, want) {
			t.Errorf("info popup missing %q:\n%s", want, view)
		}
	}
}

// A board with nothing said about it shows what the field is for, rather than
// an empty box that reads as broken.
func TestInfoOnAnUndescribedBoardInvitesOne(t *testing.T) {
	m := testModel(t, "a ticket")
	m.Update(keyPress("i"))
	if !strings.Contains(m.View(), "context about this board") {
		t.Errorf("expected the placeholder:\n%s", m.View())
	}
}

// enter is the way in: the popup lands read-only, and the one key you'd try
// starts the edit rather than closing what you just opened.
func TestInfoEnterStartsTheEdit(t *testing.T) {
	m := testModel(t, "a ticket")
	m.Update(keyPress("i"))
	m.Update(keyPress("enter"))

	if !m.infoEditing {
		t.Error("enter did not start an edit")
	}
	if m.view != infoView {
		t.Errorf("view = %v, want to stay on infoView", m.view)
	}
}

// The point of `i` in the picker is reading what a sprint covers before
// switching into it, so it describes the highlighted board, not the current one.
func TestInfoInPickerDescribesTheHighlightedBoard(t *testing.T) {
	m := pickerModel(t, "demo")
	setDesc(t, "", "main board")
	setDesc(t, "demo", "The demo sprint.")
	m.reload()

	selectBoard(t, m, "demo")
	m.Update(keyPress("i"))

	if m.view != infoView {
		t.Fatalf("view = %v, want infoView", m.view)
	}
	if m.infoBoard != "demo" {
		t.Errorf("infoBoard = %q, want demo", m.infoBoard)
	}
	if m.sprintName != "" {
		t.Errorf("sprintName = %q — reading a description must not switch board", m.sprintName)
	}
	if !strings.Contains(m.View(), "The demo sprint.") {
		t.Errorf("popup did not show the highlighted board's description:\n%s", m.View())
	}
}

func TestInfoEditSavesToTheBoard(t *testing.T) {
	m := testModel(t, "a ticket")
	m.Update(keyPress("i"))
	m.Update(keyPress("e"))
	if !m.infoEditing {
		t.Fatal("e did not start an edit")
	}

	m.infoDesc.SetValue("Written from the TUI.")
	m.Update(keyPress("enter"))

	if m.infoEditing {
		t.Error("still editing after enter")
	}
	s, err := boardStore("")
	if err != nil {
		t.Fatal(err)
	}
	board, err := s.Load()
	if err != nil {
		t.Fatal(err)
	}
	if board.Description != "Written from the TUI." {
		t.Errorf("persisted description = %q, want the edited text", board.Description)
	}
	if m.board.Description != "Written from the TUI." {
		t.Errorf("in-memory board = %q — the open model must see its own write", m.board.Description)
	}
}

// esc is discard, not save: the popup stays open on the text that is really
// stored, so an abandoned edit can't look like it landed.
func TestInfoEditEscapeDiscards(t *testing.T) {
	m := testModel(t, "a ticket")
	setDesc(t, "", "original")
	m.reload()

	m.Update(keyPress("i"))
	m.Update(keyPress("e"))
	m.infoDesc.SetValue("abandoned")
	m.Update(keyPress("esc"))

	if m.infoEditing {
		t.Error("esc did not leave the editor")
	}
	if m.view != infoView {
		t.Errorf("view = %v, want to stay on infoView", m.view)
	}
	if m.infoText != "original" {
		t.Errorf("infoText = %q, want the stored text back", m.infoText)
	}
	s, _ := boardStore("")
	board, err := s.Load()
	if err != nil {
		t.Fatal(err)
	}
	if board.Description != "original" {
		t.Errorf("persisted = %q, want it untouched by a discarded edit", board.Description)
	}
}

// An over-cap edit has to keep the editor open, or the rejected text is lost
// along with the error.
func TestInfoEditOverCapKeepsTheText(t *testing.T) {
	m := testModel(t, "a ticket")
	m.Update(keyPress("i"))
	m.Update(keyPress("e"))

	tooLong := strings.Repeat("x", store.MaxDescriptionLen+1)
	m.infoDesc.SetValue(tooLong)
	m.Update(keyPress("enter"))

	if !m.infoEditing {
		t.Error("editor closed on a rejected write — the text would be lost")
	}
	if m.notice == "" {
		t.Error("no notice explaining the refusal")
	}
	if m.infoDesc.Value() != tooLong {
		t.Error("the rejected text was cleared out of the editor")
	}
}

func TestInfoEditRefusedOnArchivedSprint(t *testing.T) {
	m := pickerModel(t, "demo")
	setDesc(t, "demo", "frozen")
	if err := store.ArchiveSprint("demo"); err != nil {
		t.Fatal(err)
	}
	m.pickerShowArchived = true
	m.reloadPickerEntries()
	selectBoard(t, m, "demo")
	m.notice = ""

	m.Update(keyPress("i"))
	if m.infoBoard != "demo" {
		t.Fatalf("infoBoard = %q, want demo", m.infoBoard)
	}
	if !strings.Contains(m.View(), "frozen") {
		t.Errorf("an archived board must still be readable:\n%s", m.View())
	}

	m.Update(keyPress("e"))
	if m.infoEditing {
		t.Error("an archived sprint's description was opened for editing")
	}
	if !strings.Contains(m.notice, "archived") {
		t.Errorf("notice = %q, want it to mention the sprint is archived", m.notice)
	}
}

// The board name in the footer is the other way in — no key to know about.
func TestClickingTheBoardBadgeOpensInfo(t *testing.T) {
	m := testModel(t, "a ticket")
	setDesc(t, "", "reached by mouse")
	m.reload()

	m.View() // register zones
	z := zoneOf(t, m, zoneBoardBadge, 0, 0)
	m.mouseClick(mouseAt(z.x, z.y))

	if m.view != infoView {
		t.Fatalf("view = %v, want infoView after clicking the badge", m.view)
	}
	if !strings.Contains(m.View(), "reached by mouse") {
		t.Errorf("popup did not show the description:\n%s", m.View())
	}
}

// Closing returns to whatever the popup was opened over, not to a fixed view.
func TestInfoClosesBackToItsSource(t *testing.T) {
	m := pickerModel(t, "demo")
	selectBoard(t, m, "demo")
	m.Update(keyPress("i"))
	m.Update(keyPress("esc"))
	if m.view != pickerView {
		t.Errorf("view = %v, want pickerView — info was opened from the picker", m.view)
	}
}

// The popup holds a snapshot of a board this Model may not be sitting on. A
// removed sprint reads back as an empty board and a write recreates its
// directory, so without a guard, saving resurrects it as a board with a
// description and no tickets.
func TestInfoSaveRefusesAResurrectedSprint(t *testing.T) {
	m := pickerModel(t, "demo")
	setDesc(t, "demo", "about the demo sprint")
	m.reloadPickerEntries()
	selectBoard(t, m, "demo")
	m.Update(keyPress("i"))
	m.Update(keyPress("enter")) // start editing
	if !m.infoEditing {
		t.Fatal("setup: never started editing")
	}

	if err := store.RemoveSprint("demo"); err != nil {
		t.Fatal(err)
	}
	m.infoDesc.SetValue("written after the sprint was deleted")
	m.Update(keyPress("enter")) // save

	if sprints, err := store.ListSprints(); err != nil {
		t.Fatal(err)
	} else if len(sprints) != 0 {
		t.Errorf("saving recreated the deleted sprint: %+v", sprints)
	}
	if m.notice == "" {
		t.Error("no notice explaining the refusal")
	}
	if !m.infoEditing {
		t.Error("editor closed on a refused save — the text would be lost")
	}
}

// An agent rewriting the description while the popup is open must not be
// silently overwritten by text that predates its edit.
func TestInfoSaveRefusesWhenTheBoardChangedUnderneath(t *testing.T) {
	m := testModel(t, "a ticket")
	setDesc(t, "", "original")
	m.reload()
	m.Update(keyPress("i"))
	m.Update(keyPress("enter"))

	setDesc(t, "", "an agent wrote this while the popup was open")

	m.infoDesc.SetValue("Leon's edit, from before the agent's")
	m.Update(keyPress("enter"))

	s, _ := boardStore("")
	board, err := s.Load()
	if err != nil {
		t.Fatal(err)
	}
	if board.Description != "an agent wrote this while the popup was open" {
		t.Errorf("description = %q, want the agent's text left alone", board.Description)
	}
	if m.notice == "" {
		t.Error("no notice explaining the refusal")
	}
}

// While the popup is just being read, the tick brings an agent's edit in, the
// way it does for a ticket.
func TestInfoRefreshesOnTheTick(t *testing.T) {
	m := testModel(t, "a ticket")
	setDesc(t, "", "before")
	m.reload()
	m.Update(keyPress("i"))
	if !strings.Contains(m.View(), "before") {
		t.Fatal("setup: popup did not show the original text")
	}

	setDesc(t, "", "after an agent's edit")
	m.Update(tickMsg{})

	if !strings.Contains(m.View(), "after an agent's edit") {
		t.Errorf("popup did not pick up the change:\n%s", m.View())
	}
}

// Not while editing, though — replacing the text under the cursor loses what
// was typed.
func TestInfoDoesNotRefreshWhileEditing(t *testing.T) {
	m := testModel(t, "a ticket")
	setDesc(t, "", "before")
	m.reload()
	m.Update(keyPress("i"))
	m.Update(keyPress("enter"))
	m.infoDesc.SetValue("half-typed thought")

	setDesc(t, "", "an agent's edit")
	m.Update(tickMsg{})

	if got := m.infoDesc.Value(); got != "half-typed thought" {
		t.Errorf("editor content = %q, want the typing left alone", got)
	}
}

// ─── Name and prefix ────────────────────────────────────────────────

// openRename does what `r` in the picker does, through the real binding.
func openRename(t *testing.T, m *Model, name string) {
	t.Helper()
	selectBoard(t, m, name)
	m.updatePicker(keyPress("r"))
	if m.view != infoView || !m.infoEditing || m.infoField != infoFieldName {
		t.Fatalf("r left view=%v editing=%v field=%d, want the name open for typing",
			m.view, m.infoEditing, m.infoField)
	}
}

func TestInfoRenameChangesTheName(t *testing.T) {
	m := pickerModel(t, "kanban")
	openRename(t, m, "kanban")

	if got := m.infoNameIn.Value(); got != "kanban" {
		t.Fatalf("name field seeded with %q, want kanban", got)
	}
	m.infoNameIn.SetValue("tools")
	m.Update(keyPress("enter"))

	if store.SprintExists("kanban") {
		t.Error("the old sprint name still resolves")
	}
	if !store.SprintExists("tools") {
		t.Fatal("the renamed sprint does not exist")
	}
	// The popup is describing the board that just moved, so its own subject and
	// the list behind it both have to follow.
	if m.infoBoard != "tools" || m.infoName != "tools" {
		t.Errorf("popup still on (%q, %q), want tools", m.infoBoard, m.infoName)
	}
	if got, want := pickerNames(m), []string{"main", "tools"}; strings.Join(got, ",") != strings.Join(want, ",") {
		t.Errorf("picker = %v, want %v", got, want)
	}
	if m.infoEditing {
		t.Error("still editing after a successful save")
	}
}

// The prefix field rewrites every short id on the board, keeping the number.
func TestInfoPrefixRetagsTickets(t *testing.T) {
	m := pickerModel(t, "kanban")
	sprint, err := store.NewSprint("kanban")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := sprint.Add("a task", "", model.StatusTodo, nil, "", "test"); err != nil {
		t.Fatal(err)
	}
	m.reloadPickerEntries()

	selectBoard(t, m, "kanban")
	m.Update(keyPress("i"))
	m.infoField = infoFieldPrefix
	m.Update(keyPress("enter"))
	if !m.infoEditing {
		t.Fatal("enter did not open the prefix field")
	}
	if got := m.infoPrefixIn.Value(); got != "KA" {
		t.Fatalf("prefix field seeded with %q, want KA", got)
	}

	// The hint is the part that isn't obvious from typing two letters.
	m.infoPrefixIn.SetValue("TL")
	if hint := m.infoIDHint(); !strings.Contains(hint, "KA1") || !strings.Contains(hint, "TL1") {
		t.Errorf("id hint = %q, want it to spell out KA1 → TL1", hint)
	}
	m.Update(keyPress("enter"))

	board, err := sprint.Load()
	if err != nil {
		t.Fatal(err)
	}
	if len(board.Tickets) != 1 || board.Tickets[0].ShortID != "TL1" {
		t.Errorf("tickets = %+v, want one TL1", board.Tickets)
	}
	if m.infoPrefix != "TL" {
		t.Errorf("popup still showing prefix %q, want TL", m.infoPrefix)
	}
	if !strings.Contains(m.notice, "TL") {
		t.Errorf("notice %q doesn't name the new prefix", m.notice)
	}
}

// Renaming the board you're sitting on has to re-point the model, or its next
// read hits a directory that no longer exists.
func TestInfoRenameFollowsTheLiveBoard(t *testing.T) {
	m := pickerModel(t, "kanban")
	if err := m.switchBoard("kanban"); err != nil {
		t.Fatal(err)
	}
	m.enterPicker()
	openRename(t, m, "kanban")

	m.infoNameIn.SetValue("tools")
	m.Update(keyPress("enter"))

	if m.sprintName != "tools" {
		t.Fatalf("model still on %q, want tools", m.sprintName)
	}
	if _, err := m.store.Load(); err != nil {
		t.Errorf("the live store no longer reads: %v", err)
	}
	if _, err := m.store.Add("after", "", model.StatusTodo, nil, "", "test"); err != nil {
		t.Errorf("the live store no longer writes: %v", err)
	}
}

// A rejected rename keeps the field open on what was typed — retyping a 40-char
// name to fix one character would be the worse trade.
func TestInfoRenameKeepsTheFieldOpenOnError(t *testing.T) {
	m := pickerModel(t, "alpha", "beta")
	openRename(t, m, "beta")

	m.infoNameIn.SetValue("alpha") // already taken
	m.Update(keyPress("enter"))

	if !m.infoEditing {
		t.Error("the field closed on a rejected rename")
	}
	if m.infoNameIn.Value() != "alpha" {
		t.Errorf("typed value lost: %q", m.infoNameIn.Value())
	}
	if !strings.Contains(m.notice, "exists") {
		t.Errorf("notice %q doesn't explain the refusal", m.notice)
	}
	if !store.SprintExists("beta") {
		t.Error("beta was renamed anyway")
	}
}

// `r` refuses before it opens anything: main has no directory name to change,
// and an archived sprint is frozen. The popup itself still opens on both — that
// is how you read them.
func TestInfoRenameRefusesMainAndArchived(t *testing.T) {
	m := pickerModel(t, "alpha")

	selectBoard(t, m, "")
	m.updatePicker(keyPress("r"))
	if m.view != pickerView {
		t.Errorf("r opened the rename on main (view %v)", m.view)
	}
	if !strings.Contains(m.notice, "main") {
		t.Errorf("notice %q doesn't say main can't be renamed", m.notice)
	}

	selectBoard(t, m, "alpha")
	m.startPickerArchive()
	m.updatePickerConfirm(keyPress("y"))
	m.pickerShowArchived = true
	m.reloadPickerEntries()
	selectBoard(t, m, "alpha")
	m.notice = ""

	m.updatePicker(keyPress("r"))
	if m.view != pickerView {
		t.Errorf("r opened the rename on an archived sprint (view %v)", m.view)
	}
	if !strings.Contains(m.notice, "unarchive") {
		t.Errorf("notice %q doesn't point at unarchive", m.notice)
	}
}

func TestInfoRenameEscapeCancels(t *testing.T) {
	m := pickerModel(t, "kanban")
	openRename(t, m, "kanban")

	m.infoNameIn.SetValue("something-else")
	m.Update(keyPress("esc"))

	if m.infoEditing {
		t.Error("the field stayed open after esc")
	}
	if !store.SprintExists("kanban") || store.SprintExists("something-else") {
		t.Error("esc applied the rename anyway")
	}
	// esc leaves the editor, not the popup — the board is still described.
	if m.view != infoView {
		t.Errorf("view = %v, want the popup still open", m.view)
	}
	if m.infoName != "kanban" {
		t.Errorf("name shows %q, want the stored name back", m.infoName)
	}
}

// An unedited save must not cost the user their place on the board — a rename
// re-points the whole model, and doing that for nothing throws away the cursor.
func TestInfoNoOpSaveKeepsBoardPosition(t *testing.T) {
	m := pickerModel(t, "kanban")
	if err := m.switchBoard("kanban"); err != nil {
		t.Fatal(err)
	}
	m.focusedCol = 2
	m.cursors = [5]int{0, 0, 3, 0, 0}
	m.enterPicker()
	openRename(t, m, "kanban")

	m.Update(keyPress("enter")) // nothing typed

	if m.focusedCol != 2 {
		t.Errorf("focusedCol = %d, want 2 — an unchanged save moved the user", m.focusedCol)
	}
	if m.cursors[2] != 3 {
		t.Errorf("cursors = %v, want the Doing cursor still on index 3", m.cursors)
	}
}

// Main's name is its root directory and its ids are bare numbers, so there is
// nothing to edit there — but the cursor still walks through both fields. Making
// them unreachable left j/k doing nothing at all on the board the TUI opens on,
// which is a broken popup, not a fixed one (Leon, 2026-08-16).
func TestInfoMainFieldsTakeTheCursorAndRefuseTheEdit(t *testing.T) {
	for _, split := range []bool{false, true} {
		name := map[bool]string{false: "stacked", true: "split"}[split]
		t.Run(name, func(t *testing.T) {
			m := testModel(t, "a ticket")
			m.infoSplitRow = split
			m.Update(keyPress("i"))
			if m.infoField != infoFieldDesc {
				t.Fatalf("landed on field %d, want the description", m.infoField)
			}

			m.Update(keyPress("k"))
			if m.infoField != infoFieldName {
				t.Fatalf("k left the cursor on field %d — main's name must still take it", m.infoField)
			}
			m.Update(keyPress("enter"))
			if m.infoEditing {
				t.Error("main's name was opened for editing")
			}
			if !strings.Contains(m.notice, "renamed") {
				t.Errorf("notice = %q, want it to say why main can't be renamed", m.notice)
			}

			// And the prefix, reached the way its layout reaches it.
			m.notice = ""
			if split {
				m.Update(keyPress("l"))
			} else {
				m.Update(keyPress("k"))
			}
			if m.infoField != infoFieldPrefix {
				t.Fatalf("cursor on field %d, want main's prefix", m.infoField)
			}
			m.Update(keyPress("enter"))
			if m.infoEditing {
				t.Error("main's prefix was opened for editing")
			}
			if !strings.Contains(m.notice, "prefix") {
				t.Errorf("notice = %q, want it to say why main has no prefix", m.notice)
			}

			// The mouse gets the same answer, so a click is never a dead end.
			m.View()
			zoneOf(t, m, zoneInfoField, 0, infoFieldName)
		})
	}
}

// j/k walk the panels, the way they do in the ticket detail. Stacked that is
// three stops; split, the name and prefix share a row and h/l picks between
// them. Both layouts live behind `z` until one is chosen.
func TestInfoFieldsWalkWithJK(t *testing.T) {
	for _, layout := range []struct {
		split bool
		name  string
		steps []struct {
			key  string
			want int
		}
	}{
		{false, "stacked", []struct {
			key  string
			want int
		}{
			{"k", infoFieldName},
			{"k", infoFieldPrefix},
			{"k", infoFieldPrefix}, // clamps at the top
			{"j", infoFieldName},
			{"j", infoFieldDesc},
			{"j", infoFieldDesc}, // clamps at the bottom
		}},
		{true, "split", []struct {
			key  string
			want int
		}{
			{"k", infoFieldName}, // the row, entered on its left cell
			{"l", infoFieldPrefix},
			{"l", infoFieldPrefix}, // clamps at the right edge
			{"h", infoFieldName},
			{"h", infoFieldName}, // clamps at the left edge
			{"k", infoFieldName}, // already on the top row
			{"j", infoFieldDesc},
			{"j", infoFieldDesc},
			{"h", infoFieldDesc}, // h/l is the row's business, not the description's
		}},
	} {
		t.Run(layout.name, func(t *testing.T) {
			m := pickerModel(t, "demo")
			selectBoard(t, m, "demo")
			m.infoSplitRow = layout.split
			m.Update(keyPress("i"))
			for _, step := range layout.steps {
				m.Update(keyPress(step.key))
				if m.infoField != step.want {
					t.Fatalf("%q left the cursor on field %d, want %d", step.key, m.infoField, step.want)
				}
			}
		})
	}
}

// The popup is three panels deep, and the smallest terminal we render at must
// still show all three — a name field scrolled off screen is one being typed
// into blind.
func TestInfoPanelsSurviveTheSmallestTerminal(t *testing.T) {
	m := pickerModel(t, "demo")
	setDesc(t, "demo", strings.Repeat("long enough to overflow. ", 40))
	selectBoard(t, m, "demo")
	m.width, m.height = minTerminalWidth, minTerminalHeight
	m.Update(keyPress("i"))

	view := m.View()
	for _, want := range []string{"demo", "Name", "Description"} {
		if !strings.Contains(view, want) {
			t.Errorf("%q lost at %dx%d:\n%s", want, minTerminalWidth, minTerminalHeight, view)
		}
	}
	for i, line := range strings.Split(view, "\n") {
		if w := lipgloss.Width(line); w > minTerminalWidth {
			t.Errorf("line %d overflows the terminal: %d cells", i, w)
			break
		}
	}
	if lines := strings.Split(view, "\n"); len(lines) > minTerminalHeight {
		t.Errorf("view is %d rows on a %d-row terminal", len(lines), minTerminalHeight)
	}
}

// First click selects the panel, second opens its editor — the same rule the
// ticket detail's panels follow, so a misjudged click can't start an edit.
func TestClickPicksAnInfoPanel(t *testing.T) {
	m := pickerModel(t, "demo")
	selectBoard(t, m, "demo")
	m.Update(keyPress("i"))
	m.View()

	name := zoneOf(t, m, zoneInfoField, 0, infoFieldName)
	m.mouseClick(mouseAt(name.x, name.y))
	if m.infoField != infoFieldName {
		t.Fatalf("field = %d, want the name panel", m.infoField)
	}
	if m.infoEditing {
		t.Fatal("the first click opened the editor")
	}
	m.mouseClick(mouseAt(name.x, name.y))
	if !m.infoEditing {
		t.Fatal("the second click did not open the editor")
	}
	// A blurred input drops every key it is handed, so moving the highlight
	// alone would leave the user typing into nothing.
	before := m.infoNameIn.Value()
	m.Update(keyPress("Z"))
	if m.infoNameIn.Value() == before {
		t.Errorf("name still %q after typing — the clicked field never took the keyboard", before)
	}
}

// The `:shortcode:` typeahead follows the field, not the popup. A sprint name
// is letters, digits, '_' and '-' and a prefix is four letters at most, so an
// expansion into either writes something the store would refuse.
func TestInfoTypeaheadOnlyArmsOverTheDescription(t *testing.T) {
	m := pickerModel(t, "demo")
	selectBoard(t, m, "demo")
	m.Update(keyPress("i"))

	for _, tc := range []struct {
		field int
		want  bool
		name  string
	}{
		{infoFieldDesc, true, "description"},
		{infoFieldName, false, "name"},
		{infoFieldPrefix, false, "prefix"},
	} {
		m.infoField = tc.field
		m.infoEditing = true
		if _, ok := m.focusedTextTarget(); ok != tc.want {
			t.Errorf("typeahead armed=%v over the %s field, want %v", ok, tc.name, tc.want)
		}
	}
}

// The board popup and the new-ticket form are the same box with different
// fields in it. If they drift apart, moving between them moves the frame.
func TestInfoPopupMatchesTheAddPopupGeometry(t *testing.T) {
	m := pickerModel(t, "demo")
	selectBoard(t, m, "demo")

	for _, size := range [][2]int{{110, 34}, {80, 24}, {60, 16}, {minTerminalWidth, minTerminalHeight}} {
		m.width, m.height = size[0], size[1]
		gotW, gotH := m.infoPopupSize()
		wantW, wantH := m.addPopupSize()
		if gotW != wantW || gotH != wantH {
			t.Errorf("at %dx%d the board popup is %dx%d, the add popup %dx%d",
				size[0], size[1], gotW, gotH, wantW, wantH)
		}
		if m.infoInnerWidth() != m.addInnerWidth() {
			t.Errorf("at %dx%d inner widths differ: %d vs %d",
				size[0], size[1], m.infoInnerWidth(), m.addInnerWidth())
		}
	}
}

// The description is the reason the box is a fixed size: it gets everything the
// frame has left over, rather than hugging however much text is there.
func TestInfoDescriptionTakesTheRemainingHeight(t *testing.T) {
	m := pickerModel(t, "demo")
	setDesc(t, "demo", "one short line")
	selectBoard(t, m, "demo")
	m.width, m.height = 110, 34
	m.Update(keyPress("i"))
	m.View()

	_, popupHeight := m.infoPopupSize()
	desc := zoneOf(t, m, zoneInfoField, 0, infoFieldDesc)
	// Frame borders (2) + meta (1) + name panel (3) + help (1).
	if want := popupHeight - 7; desc.h != want {
		t.Errorf("description panel is %d rows, want %d — a one-line board should still get the full box",
			desc.h, want)
	}
}
