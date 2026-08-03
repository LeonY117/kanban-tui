package tui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/LeonY117/kanban-tui/internal/store"
)

// The picker key is rebindable, so a hard-coded "tab" inside the title and
// description editors stayed live after a rebind and shadowed whatever took it.
func TestReboundPickerKeyWorksInsideTheTitleEditor(t *testing.T) {
	restoreBindings(t)
	ApplyConfig(store.Config{Keys: map[string]string{"board.picker": "b"}})

	m := testModel(t, "Wire up the auth callback")
	m.view = detailView
	m.editField = 1
	m.refreshDetailEditors()
	m.editTitle.Focus()

	m.updateDetailTitle(tea.KeyMsg{Type: tea.KeyTab})
	if m.view == pickerView {
		t.Error("tab still opened the picker after board.picker was rebound off it")
	}
	m.editTitle.Focus()
	m.updateDetailTitle(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("b")})
	if m.view != pickerView {
		t.Errorf("the rebound key did not open the picker from the title editor (view=%v)", m.view)
	}
}

// The tab reports which files this process is using; hard-coding ~/.kanban
// named the wrong board on every sprint and under KANBAN_FILE.
func TestAboutNamesTheBoardActuallyInUse(t *testing.T) {
	restoreBindings(t)
	m := settingsModel(t)
	m.settings.section = sectionAbout
	out := crop(m.View())

	if strings.Contains(out, "~/.kanban/board.json") {
		t.Errorf("About still hard-codes the default board path:\n%s", out)
	}
	// Long paths are shortened from the left, so assert on the tail that
	// actually identifies the board.
	want := m.store.BoardPath()
	tail := want[strings.LastIndex(want[:strings.LastIndex(want, "/")], "/")+1:]
	if !strings.Contains(out, tail) {
		t.Errorf("About does not name the live board (tail %q):\n%s", tail, out)
	}
}

// Leaving with a mix of clean and clashing edits used to report only
// "settings saved", so a rolled-back edit left no trace.
func TestSaveReportsRolledBackConflicts(t *testing.T) {
	restoreBindings(t)
	m := settingsModel(t)
	m.settings.idx = findAction(t, "card.copy")
	press(m, "enter", "z") // clean
	m.settings.idx = findAction(t, "card.add")
	press(m, "enter", "x") // clashes with archive
	press(m, "esc", "esc")

	if !strings.Contains(m.notice, "undid") {
		t.Errorf("notice = %q, want it to mention the rolled-back change", m.notice)
	}
	if got := store.LoadConfig().Keys["card.copy"]; got != "z" {
		t.Errorf("the clean edit did not survive: %q", got)
	}
}

func TestCtrlCQuitsFromSettings(t *testing.T) {
	m := settingsModel(t)
	if _, cmd := m.updateSettings(tea.KeyMsg{Type: tea.KeyCtrlC}); cmd == nil {
		t.Error("ctrl+c is inert while settings is open")
	}
}

// A paste is one KeyRunes message carrying every rune, so capturing it bound
// an action to a string no keypress can ever match.
func TestPastedTextIsRefusedAsAShortcut(t *testing.T) {
	restoreBindings(t)
	m := settingsModel(t)
	m.settings.idx = findAction(t, "card.archive")
	press(m, "enter")
	m.updateSettings(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("foo"), Paste: true})

	if got := m.settings.binds["card.archive"]; got != "x" {
		t.Errorf("card.archive = %q, want the paste refused", got)
	}
	if !strings.Contains(m.settings.notice, "pasted text") {
		t.Errorf("notice = %q, want it to say why", m.settings.notice)
	}
}

// A column name is an ordinary text field, so a paste should land in it.
func TestPastedTextLandsInAColumnName(t *testing.T) {
	restoreBindings(t)
	m := settingsModel(t)
	m.settings.section = sectionColumns
	m.settings.idx = 4 // HOLD
	press(m, "enter", "backspace", "backspace", "backspace", "backspace")
	m.updateSettings(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("Waiting"), Paste: true})
	press(m, "enter")

	if got := m.settings.labels[m.settings.labelStatus()]; got != "Waiting" {
		t.Errorf("HOLD label = %q, want the pasted Waiting", got)
	}
}
