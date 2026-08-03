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

func TestLegalTwoKeySwapPersists(t *testing.T) {
	restoreBindings(t)
	m := settingsModel(t)
	m.settings.idx = findAction(t, "card.add")
	press(m, "enter", "x")
	m.settings.idx = findAction(t, "card.archive")
	press(m, "enter", "a")
	if conflicts := m.settings.conflictCount(); conflicts != 0 {
		t.Fatalf("completed swap has %d conflict(s)", conflicts)
	}
	press(m, "esc")

	cfg := store.LoadConfig()
	if cfg.Keys["card.add"] != "x" || cfg.Keys["card.archive"] != "a" {
		t.Errorf("swap on disk = %v, want add=x and archive=a", cfg.Keys)
	}
	if keys.Add.Keys()[0] != "x" || keys.Archive.Keys()[0] != "a" {
		t.Errorf("live swap = add %v, archive %v", keys.Add.Keys(), keys.Archive.Keys())
	}
}

func TestRenamingAColumnSurvivesAReopen(t *testing.T) {
	restoreBindings(t)
	m := settingsModel(t)
	holdTicket, err := m.store.Add("Waiting on review", "", model.StatusHold, nil, "", "test")
	if err != nil {
		t.Fatal(err)
	}
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
	ticket, _ := board.FindByID(holdTicket.ShortID)
	if ticket == nil {
		t.Fatalf("renamed column lost ticket %s", holdTicket.ShortID)
	}
	if ticket.Status != model.StatusHold {
		t.Errorf("ticket %s status = %q, want canonical HOLD", ticket.ShortID, ticket.Status)
	}
}

func TestUnicodeColumnNameSurvivesAReopen(t *testing.T) {
	restoreBindings(t)
	m := settingsModel(t)
	m.settings.section = sectionColumns
	m.settings.idx = 4 // HOLD

	press(m, "enter", "backspace", "backspace", "backspace", "backspace")
	for _, r := range "待機✨" {
		press(m, string(r))
	}
	press(m, "enter", "esc")

	ApplyConfig(store.LoadConfig())
	if got := statusDisplay[model.StatusHold]; got != "待機✨" {
		t.Errorf("HOLD label = %q, want 待機✨", got)
	}
}

// The page is a stale working copy by the time it closes. A second writer's
// unrelated known and future settings must survive, while this visit's reset
// removes only the override the user actually reset.
func TestSavingSettingsMergesASecondWritersEntries(t *testing.T) {
	restoreBindings(t)
	m := testModel(t, "Wire up the auth callback")
	if err := store.SaveConfig(store.Config{Keys: map[string]string{
		"card.archive": "z",
	}}); err != nil {
		t.Fatal(err)
	}
	ApplyConfig(store.LoadConfig())
	m.enterSettings()
	m.settings.idx = findAction(t, "card.archive")

	// This lands after the page opened and before it saved.
	concurrentColumns := []store.ColumnConfig{
		{Status: "FUTURE", Label: "Later", Short: "L"},
		{Status: "DONE", Label: "Shipped"},
	}
	if err := store.SaveConfig(store.Config{
		Columns: concurrentColumns,
		Keys: map[string]string{
			"card.archive":  "z",
			"card.copy":     "w",
			"future.action": "!",
		},
	}); err != nil {
		t.Fatal(err)
	}

	press(m, "r", "esc") // reset only archive to its default

	cfg := store.LoadConfig()
	if _, ok := cfg.Keys["card.archive"]; ok {
		t.Errorf("reset override still on disk: %v", cfg.Keys)
	}
	if cfg.Keys["card.copy"] != "w" || cfg.Keys["future.action"] != "!" {
		t.Errorf("second writer's keys were lost: %v", cfg.Keys)
	}
	if len(cfg.Columns) != len(concurrentColumns) {
		t.Fatalf("second writer's columns changed: %+v", cfg.Columns)
	}
	for i := range concurrentColumns {
		if cfg.Columns[i] != concurrentColumns[i] {
			t.Errorf("column %d = %+v, want %+v", i, cfg.Columns[i], concurrentColumns[i])
		}
	}
	if m.settings.binds["card.copy"] != "w" || m.settings.labels[model.StatusDone] != "Shipped" {
		t.Errorf("working copy stayed stale: copy=%q DONE=%q",
			m.settings.binds["card.copy"], m.settings.labels[model.StatusDone])
	}
}

func TestConcurrentSettingsConflictDoesNotReachDisk(t *testing.T) {
	restoreBindings(t)
	m := settingsModel(t)
	m.settings.idx = findAction(t, "card.archive")
	press(m, "enter", "z")

	// A second page independently chose the same key after this one opened.
	if err := store.SaveConfig(store.Config{Keys: map[string]string{
		"card.copy": "z",
	}}); err != nil {
		t.Fatal(err)
	}
	press(m, "esc")

	if m.view != settingsView {
		t.Fatal("settings closed after its merged binding conflicted")
	}
	if !strings.Contains(m.settings.notice, "claimed by another config change") {
		t.Errorf("notice = %q, want the concurrent conflict", m.settings.notice)
	}
	cfg := store.LoadConfig()
	if _, ok := cfg.Keys["card.archive"]; ok {
		t.Errorf("conflicting archive binding reached disk: %v", cfg.Keys)
	}
	if cfg.Keys["card.copy"] != "z" {
		t.Errorf("second writer's binding changed: %v", cfg.Keys)
	}
}

func TestSaveFailureKeepsSettingsOpenAndDirty(t *testing.T) {
	restoreBindings(t)
	m := settingsModel(t)
	m.settings.idx = findAction(t, "card.archive")
	press(m, "enter", "z")

	originalBoard := os.Getenv("KANBAN_FILE")
	blocker := filepath.Join(t.TempDir(), "not-a-directory")
	if err := os.WriteFile(blocker, []byte("block config root"), 0644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("KANBAN_FILE", filepath.Join(blocker, "board.json"))
	press(m, "esc")

	if m.view != settingsView {
		t.Fatal("settings closed after config persistence failed")
	}
	if !m.settings.dirty || m.settings.binds["card.archive"] != "z" {
		t.Errorf("dirty working copy was lost: dirty=%v archive=%q",
			m.settings.dirty, m.settings.binds["card.archive"])
	}
	if !strings.Contains(m.settings.notice, "could not save settings") {
		t.Errorf("notice = %q, want the save failure", m.settings.notice)
	}
	if out := crop(m.View()); !strings.Contains(out, "could not save settings") {
		t.Errorf("save failure is not rendered in the open popup:\n%s", out)
	}

	// Once the filesystem target is usable again, closing retries the same edit.
	t.Setenv("KANBAN_FILE", originalBoard)
	press(m, "esc")
	if m.view == settingsView {
		t.Fatal("settings stayed open after a successful retry")
	}
	if got := store.LoadConfig().Keys["card.archive"]; got != "z" {
		t.Errorf("retried archive binding = %q, want z", got)
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
