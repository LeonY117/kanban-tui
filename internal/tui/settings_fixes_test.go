package tui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/LeonY117/kanban-tui/internal/model"
	"github.com/LeonY117/kanban-tui/internal/store"
)

// A conflict made in Shortcuts and then escaped from another section used to
// close the page and write itself to disk, because the guard only ran while
// the Shortcuts tab was showing.
func TestEscapingFromAnotherSectionStillCatchesAConflict(t *testing.T) {
	restoreBindings(t)
	m := settingsModel(t)

	m.settings.idx = findAction(t, "card.archive")
	press(m, "enter", "m") // "m" is card.move
	if m.settings.conflictCount() != 1 {
		t.Fatalf("expected a conflict, got %d", m.settings.conflictCount())
	}
	press(m, "2") // hop to Columns
	if out := crop(m.View()); !strings.Contains(out, "used twice") {
		t.Errorf("Columns footer hid the outstanding conflict:\n%s", out)
	}
	press(m, "esc")
	if m.view != settingsView {
		t.Fatal("esc closed the page from Columns while a key conflict stood")
	}
	press(m, "esc")

	root := filepath.Dir(os.Getenv("KANBAN_FILE"))
	if data, err := os.ReadFile(filepath.Join(root, "config.json")); err == nil {
		if strings.Contains(string(data), `"card.archive": "m"`) {
			t.Errorf("the conflict reached disk:\n%s", data)
		}
	}
	ApplyConfig(store.LoadConfig())
	if keys.Archive.Keys()[0] != "x" || keys.Move.Keys()[0] != "m" {
		t.Errorf("archive=%v move=%v, want the defaults intact",
			keys.Archive.Keys(), keys.Move.Keys())
	}
}

// A hand-edited config can start with a custom label occupying another
// column's default. Resetting just that row must not create a duplicate.
func TestResettingAColumnRefusesADuplicateDefault(t *testing.T) {
	restoreBindings(t)
	ApplyConfig(store.Config{Columns: []store.ColumnConfig{
		{Status: "TODO", Label: "Tasks"},
		{Status: "DONE", Label: "Todo"},
	}})
	m := settingsModel(t)
	m.settings.section = sectionColumns
	m.settings.idx = 1 // TODO

	press(m, "r")

	if got := m.settings.labels[model.StatusTodo]; got != "Tasks" {
		t.Errorf("TODO label = %q, want Tasks left in place", got)
	}
	if !strings.Contains(m.settings.notice, "already another column") {
		t.Errorf("notice = %q, want the duplicate refusal", m.settings.notice)
	}
}

// Reverting one clash can create another: add a->z, edit e->a, archive x->z.
// Undoing the z clash puts add back on "a", which edit now holds.
func TestRevertingAConflictDoesNotLeaveANewOne(t *testing.T) {
	s := newSettingsState()
	s.baseline = map[string]string{}
	for k, v := range s.binds {
		s.baseline[k] = v
	}
	s.binds["card.add"] = "z"
	s.binds["card.edit"] = "a"
	s.binds["card.archive"] = "z"

	s.revertConflicts()
	if n := s.conflictCount(); n != 0 {
		t.Errorf("%d conflict(s) survived the revert: add=%q edit=%q archive=%q",
			n, s.binds["card.add"], s.binds["card.edit"], s.binds["card.archive"])
	}
}

// Two columns reading the same word shadow one another in the meta-bar status
// picker, so picking one can store the other's status.
func TestRenamingToAnotherColumnsNameIsRefused(t *testing.T) {
	restoreBindings(t)
	m := settingsModel(t)
	m.settings.section = sectionColumns
	m.settings.idx = 1 // TODO

	press(m, "enter")
	for range "Todo" {
		press(m, "backspace")
	}
	press(m, "D", "o", "n", "e", "enter")

	if got := m.settings.labels[model.StatusTodo]; got != "Todo" {
		t.Errorf("TODO label = %q, want it left alone", got)
	}
	if !strings.Contains(m.settings.notice, "already another column") {
		t.Errorf("notice = %q, want it to say why", m.settings.notice)
	}
}

// Even if two columns did share a label, the picker must not silently map one
// onto the other's status.
func TestStatusChoicesDisambiguatesADuplicateLabel(t *testing.T) {
	restoreBindings(t)
	ApplyConfig(store.Config{Columns: []store.ColumnConfig{
		{Status: "TODO", Label: "Done"},
	}})

	labels, byLabel := statusChoices()
	if len(byLabel) != len(labels) {
		t.Fatalf("%d labels collapsed to %d choices", len(labels), len(byLabel))
	}
	seen := map[model.Status]bool{}
	for _, st := range byLabel {
		if seen[st] {
			t.Errorf("two labels map to %q", st)
		}
		seen[st] = true
	}
	if !seen[model.StatusTodo] || !seen[model.StatusDone] {
		t.Errorf("byLabel = %v, want both TODO and DONE reachable", byLabel)
	}
}

// A generated suffix can itself be a label from the file. Keep suffixing
// until every picker entry is actually addressable.
func TestStatusChoicesDisambiguatesAGeneratedSuffixCollision(t *testing.T) {
	restoreBindings(t)
	ApplyConfig(store.Config{Columns: []store.ColumnConfig{
		{Status: "TODO", Label: "Foo"},
		{Status: "DOING", Label: "Foo (DONE)"},
		{Status: "DONE", Label: "Foo"},
	}})

	labels, byLabel := statusChoices()
	if len(byLabel) != len(labels) {
		t.Fatalf("%d labels collapsed to %d choices: %v", len(labels), len(byLabel), labels)
	}
	seen := map[model.Status]bool{}
	for _, status := range byLabel {
		seen[status] = true
	}
	for _, status := range []model.Status{model.StatusTodo, model.StatusDoing, model.StatusDone} {
		if !seen[status] {
			t.Errorf("%s disappeared from choices: %v", status, byLabel)
		}
	}
}

// The page doesn't expose short labels, so saving must not throw away one that
// was set by hand.
func TestSavingPreservesHandSetShortLabels(t *testing.T) {
	restoreBindings(t)
	m := settingsModel(t)
	if err := store.SaveConfig(store.Config{Columns: []store.ColumnConfig{
		{Status: "HOLD", Short: "Wt"},
	}}); err != nil {
		t.Fatal(err)
	}
	ApplyConfig(store.LoadConfig())

	m.settings = newSettingsState()
	m.settings.baseline = map[string]string{}
	for k, v := range m.settings.binds {
		m.settings.baseline[k] = v
	}
	m.settings.idx = findAction(t, "card.archive")
	press(m, "enter", "z", "esc")

	if got := store.LoadConfig().ShortLabels()[model.StatusHold]; got != "Wt" {
		t.Errorf("HOLD short = %q after an unrelated rebind, want Wt", got)
	}
}

func TestSaveRefusesAConflictedWorkingCopy(t *testing.T) {
	restoreBindings(t)
	m := settingsModel(t)
	m.settings.binds["card.archive"] = "m"
	m.settings.dirty = true

	m.saveSettings()
	if !strings.Contains(m.notice, "not saved") {
		t.Errorf("notice = %q, want a refusal", m.notice)
	}
	root := filepath.Dir(os.Getenv("KANBAN_FILE"))
	if _, err := os.Stat(filepath.Join(root, "config.json")); !os.IsNotExist(err) {
		t.Error("a conflicted working copy was written")
	}
}

// The popup's zones used to be registered before the backdrop rendered, so the
// board's column zones sat on top and swallowed every click.
func TestSettingsClicksLandOnTheSettingsPopup(t *testing.T) {
	restoreBindings(t)
	m := settingsModel(t)
	m.View()

	origin := m.popupOrigin(settingsWidth, m.settingsHeight())
	height := m.settingsHeight()

	// Walk the popup's rows. The first body row is a group header and owns no
	// zone by design, so find the first row that does.
	var row *hitZone
	for y := origin.y + 1; y < origin.y+height-1; y++ {
		z := m.zoneAt(origin.x+3, y)
		if z == nil {
			continue
		}
		if z.kind != zoneSettingsRow && z.kind != zoneSettingsTab {
			t.Fatalf("zone at y=%d over the popup is kind %v — the backdrop is winning",
				y-origin.y, z.kind)
		}
		if z.kind == zoneSettingsRow && row == nil {
			row = z
		}
	}
	if row == nil {
		t.Fatal("no clickable settings row anywhere in the popup")
	}
	if row.idx != 0 {
		t.Errorf("first clickable row is action %d, want 0", row.idx)
	}
	if tab := m.zoneAt(origin.x+3, origin.y+1); tab == nil || tab.kind != zoneSettingsTab {
		t.Errorf("tab zone = %+v, want a settings tab", tab)
	}
}

func TestClickingASettingsTabSwitchesSection(t *testing.T) {
	restoreBindings(t)
	m := settingsModel(t)
	m.View()

	origin := m.popupOrigin(settingsWidth, m.settingsHeight())
	var columnsTab *hitZone
	for x := origin.x + 2; x < origin.x+settingsWidth-2; x++ {
		if z := m.zoneAt(x, origin.y+1); z != nil && z.kind == zoneSettingsTab && z.idx == int(sectionColumns) {
			columnsTab = z
			break
		}
	}
	if columnsTab == nil {
		t.Fatal("no zone for the Columns tab")
	}
	m.Update(mouseAt(columnsTab.x, columnsTab.y))
	if m.settings.section != sectionColumns {
		t.Errorf("section = %v, want Columns", m.settings.section)
	}
}
