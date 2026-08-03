package tui

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/LeonY117/kanban-tui/internal/model"
	"github.com/LeonY117/kanban-tui/internal/store"
)

// findAction returns the row index of an action id in the shortcuts list.
func findAction(t *testing.T, id string) int {
	t.Helper()
	for i, a := range bindActions {
		if a.id == id {
			return i
		}
	}
	t.Fatalf("no action %q", id)
	return -1
}

func TestRebindingSurvivesAReopen(t *testing.T) {
	restoreBindings(t)
	m := settingsModel(t) // sandboxRoot has already pointed KANBAN_FILE at a temp dir

	m.settings.idx = findAction(t, "card.archive")
	press(m, "enter", "z") // capture mode: the next key becomes the binding
	press(m, "esc")        // close, which saves

	root := filepath.Dir(os.Getenv("KANBAN_FILE"))
	data, err := os.ReadFile(filepath.Join(root, "config.json"))
	if err != nil {
		t.Fatalf("config.json was not written: %v", err)
	}
	var onDisk store.Config
	if err := json.Unmarshal(data, &onDisk); err != nil {
		t.Fatal(err)
	}
	if onDisk.Keys["card.archive"] != "z" {
		t.Errorf("on disk keys = %v, want card.archive -> z", onDisk.Keys)
	}
	// Only the difference is stored, so a later change of default still lands.
	if _, stored := onDisk.Keys["card.add"]; stored {
		t.Errorf("an unchanged action was written: %v", onDisk.Keys)
	}
	if keys.Archive.Keys()[0] != "z" {
		t.Errorf("live keymap = %v, want z applied immediately", keys.Archive.Keys())
	}

	// A fresh start reads it back.
	ApplyConfig(store.Config{})
	if keys.Archive.Keys()[0] != "x" {
		t.Fatalf("reset failed: %v", keys.Archive.Keys())
	}
	ApplyConfig(store.LoadConfig())
	if keys.Archive.Keys()[0] != "z" {
		t.Errorf("after reload keymap = %v, want z", keys.Archive.Keys())
	}
}

func TestRenamingAColumnSurvivesAReopen(t *testing.T) {
	restoreBindings(t)
	m := settingsModel(t)
	m.settings.section = sectionColumns
	m.settings.idx = 4 // HOLD

	press(m, "enter")
	for _, r := range "    " { // clear "Hold"
		_ = r
		press(m, "backspace")
	}
	press(m, "W", "a", "i", "t", "enter", "esc")

	ApplyConfig(store.LoadConfig())
	if statusDisplay[model.StatusHold] != "Wait" {
		t.Errorf("HOLD label = %q, want Wait", statusDisplay[model.StatusHold])
	}
	// The point of the whole feature: stored data is untouched.
	board, err := store.New("").Load()
	if err != nil {
		t.Fatal(err)
	}
	for _, tk := range board.Tickets {
		if strings.EqualFold(string(tk.Status), "Wait") {
			t.Errorf("ticket %s stored the label instead of the status", tk.ShortID)
		}
	}
}

// Nothing was touched, so nothing should be written — an untouched visit must
// not leave a config file behind.
func TestClosingWithoutChangesWritesNothing(t *testing.T) {
	restoreBindings(t)
	m := settingsModel(t)
	press(m, "j", "j", "2", "3", "esc")

	root := filepath.Dir(os.Getenv("KANBAN_FILE"))
	if _, err := os.Stat(filepath.Join(root, "config.json")); !os.IsNotExist(err) {
		t.Errorf("config.json exists after a read-only visit (err=%v)", err)
	}
}

// A conflict is undone on the way out, so it must never reach the file.
func TestAConflictIsNotSaved(t *testing.T) {
	restoreBindings(t)
	m := settingsModel(t)

	m.settings.idx = findAction(t, "card.archive")
	press(m, "enter", "m") // "m" is card.move
	if m.settings.conflictCount() != 1 {
		t.Fatalf("expected a conflict, got %d", m.settings.conflictCount())
	}
	press(m, "esc") // refused
	if m.view != settingsView {
		t.Fatal("esc left the page while conflicted")
	}
	press(m, "esc") // undo the clash and close

	ApplyConfig(store.LoadConfig())
	if keys.Archive.Keys()[0] != "x" {
		t.Errorf("archive = %v, want the default x — the clash must not persist",
			keys.Archive.Keys())
	}
	if keys.Move.Keys()[0] != "m" {
		t.Errorf("move = %v, want to still hold m", keys.Move.Keys())
	}
}
