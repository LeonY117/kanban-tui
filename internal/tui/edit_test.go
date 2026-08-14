package tui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

func keyPress(s string) tea.KeyMsg {
	switch s {
	case "enter":
		return tea.KeyMsg{Type: tea.KeyEnter}
	case "esc":
		return tea.KeyMsg{Type: tea.KeyEsc}
	case "alt+enter":
		return tea.KeyMsg{Type: tea.KeyEnter, Alt: true}
	case "tab":
		return tea.KeyMsg{Type: tea.KeyTab}
	case "backspace":
		return tea.KeyMsg{Type: tea.KeyBackspace}
	}
	return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(s)}
}

func TestDescriptionEnterSavesAndShiftEnterAddsLine(t *testing.T) {
	m := testModel(t, "first")
	m.enterSplit()
	m.splitFocus = 1
	m.editField = 2

	// enter starts editing
	m.updateSplitDetailDesc(keyPress("enter"))
	if !m.editDesc.Focused() {
		t.Fatal("enter did not start editing the description")
	}

	m.updateSplitDetailDesc(keyPress("a"))
	m.updateSplitDetailDesc(keyPress("alt+enter")) // shift+enter, as Ghostty sends it
	m.updateSplitDetailDesc(keyPress("b"))
	if got := m.editDesc.Value(); got != "a\nb" {
		t.Errorf("description = %q, want %q", got, "a\nb")
	}

	// enter again confirms
	m.updateSplitDetailDesc(keyPress("enter"))
	if m.editDesc.Focused() {
		t.Error("enter did not stop editing")
	}
	board, _ := m.store.Load()
	if board.Tickets[0].Description != "a\nb" {
		t.Errorf("saved description = %q, want %q", board.Tickets[0].Description, "a\nb")
	}
}

func TestAddPopupEnterInDescriptionCreatesTicket(t *testing.T) {
	m := testModel(t)
	m.enterAddPopup()
	for _, r := range "new ticket" {
		m.updateAdd(keyPress(string(r)))
	}
	m.updateAdd(keyPress("tab")) // title → description
	m.updateAdd(keyPress("enter"))
	if !m.addDescEditing {
		t.Fatal("enter did not start editing the description")
	}
	m.updateAdd(keyPress("d"))
	m.updateAdd(keyPress("alt+enter"))
	m.updateAdd(keyPress("e"))
	m.updateAdd(keyPress("enter")) // confirms the whole ticket

	if m.view == addView {
		t.Error("popup stayed open after confirming")
	}
	board, _ := m.store.Load()
	if len(board.Tickets) != 1 {
		t.Fatalf("board holds %d tickets, want 1", len(board.Tickets))
	}
	if board.Tickets[0].Title != "new ticket" || board.Tickets[0].Description != "d\ne" {
		t.Errorf("created %+v, want title %q desc %q", board.Tickets[0], "new ticket", "d\ne")
	}
}

func TestAddPopupEscAsksBeforeDiscarding(t *testing.T) {
	m := testModel(t)
	m.enterAddPopup()

	// Nothing typed yet — esc just closes.
	m.updateAdd(keyPress("esc"))
	if m.view == addView {
		t.Fatal("esc on an empty popup should close it outright")
	}

	m.enterAddPopup()
	m.updateAdd(keyPress("x"))
	m.updateAdd(keyPress("esc"))
	if !m.addConfirmQuit {
		t.Fatal("esc with content should ask for confirmation")
	}
	if m.view != addView {
		t.Fatal("popup closed before the user confirmed")
	}
	if !strings.Contains(m.addHelpLine(), "discard") {
		t.Errorf("confirm prompt not shown: %q", m.addHelpLine())
	}

	m.updateAdd(keyPress("n"))
	if m.addConfirmQuit || m.view != addView {
		t.Fatal("answering no should return to the popup")
	}

	m.updateAdd(keyPress("esc"))
	m.updateAdd(keyPress("y"))
	if m.view == addView {
		t.Error("answering yes should discard the ticket")
	}
	board, _ := m.store.Load()
	if len(board.Tickets) != 0 {
		t.Errorf("discarded popup created %d tickets", len(board.Tickets))
	}
}
