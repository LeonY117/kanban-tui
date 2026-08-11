package tui

import (
	"strings"
	"testing"

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
