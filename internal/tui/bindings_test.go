package tui

import (
	"slices"
	"strings"
	"testing"

	"github.com/LeonY117/kanban-tui/internal/model"
	"github.com/LeonY117/kanban-tui/internal/store"
)

// restoreBindings puts the package-level keymap back after a test moves it.
func restoreBindings(t *testing.T) {
	t.Helper()
	t.Cleanup(func() { ApplyConfig(store.Config{}) })
}

func TestSanitizeAcceptsAPlainOverride(t *testing.T) {
	resolved, refused := sanitizeBindings(map[string]string{"card.archive": "z"})
	if len(refused) != 0 {
		t.Fatalf("refused = %v, want none", refused)
	}
	if resolved["card.archive"] != "z" {
		t.Errorf("card.archive = %q, want z", resolved["card.archive"])
	}
	if resolved["card.add"] != "a" {
		t.Errorf("untouched action moved: card.add = %q", resolved["card.add"])
	}
}

// A config that can rebind the navigation floor can lock someone out of their
// own board, so the floor is not the config's to move.
func TestSanitizeRefusesTheLockedFloor(t *testing.T) {
	resolved, refused := sanitizeBindings(map[string]string{"nav.quit": "Q"})
	if !slices.Contains(refused, "nav.quit") {
		t.Errorf("refused = %v, want it to contain nav.quit", refused)
	}
	if resolved["nav.quit"] != "q" {
		t.Errorf("nav.quit = %q, want the default q", resolved["nav.quit"])
	}
}

func TestSanitizeRefusesUnknownIdsAndBlanks(t *testing.T) {
	_, refused := sanitizeBindings(map[string]string{
		"card.teleport": "z", // from some other build
		"card.archive":  "",
	})
	if !slices.Contains(refused, "card.teleport") || !slices.Contains(refused, "card.archive") {
		t.Errorf("refused = %v, want both entries", refused)
	}
}

// A hand-edited file can ask for a key another action holds by default. The
// override wins and the displaced action is left unbound: a key named in the
// config is an explicit choice, and a default is not. The alternative order
// lets any default this tool gains later take a key back off a config that
// already claimed it (Leon, 2026-08-03).
func TestAnOverrideBeatsAnotherActionsDefault(t *testing.T) {
	resolved, refused := sanitizeBindings(map[string]string{"card.archive": "a"}) // "a" is card.add
	if slices.Contains(refused, "card.archive") {
		t.Errorf("refused = %v, want the override honoured", refused)
	}
	if resolved["card.archive"] != "a" {
		t.Errorf("card.archive = %q, want the key it asked for", resolved["card.archive"])
	}
	if got, bound := resolved["card.add"]; bound {
		t.Errorf("card.add = %q, want it unbound rather than taking the key back", got)
	}
}

// Two actions trading keys is legal — neither default is reserved, because
// both are overridden, so each is free for the other to take.
func TestSanitizeAllowsASwap(t *testing.T) {
	resolved, refused := sanitizeBindings(map[string]string{
		"card.add":     "x",
		"card.archive": "a",
	})
	if len(refused) != 0 {
		t.Fatalf("refused = %v, want none — a swap is legal", refused)
	}
	if resolved["card.add"] != "x" || resolved["card.archive"] != "a" {
		t.Errorf("add = %q, archive = %q, want x and a",
			resolved["card.add"], resolved["card.archive"])
	}
}

func TestSanitizeDropsTheSecondOfTwoOverridesWantingOneKey(t *testing.T) {
	resolved, refused := sanitizeBindings(map[string]string{
		"card.archive": "z",
		"card.copy":    "z",
	})
	if len(refused) != 1 {
		t.Fatalf("refused = %v, want exactly one dropped", refused)
	}
	// Whichever lost keeps its own default, and the winner holds z alone.
	holders := 0
	for _, a := range bindActions {
		if resolved[a.id] == "z" {
			holders++
		}
	}
	if holders != 1 {
		t.Errorf("%d actions hold z, want exactly 1", holders)
	}
	loser := refused[0]
	for _, a := range bindActions {
		if a.id == loser && resolved[a.id] != a.def {
			t.Errorf("%s = %q, want its default %q", loser, resolved[loser], a.def)
		}
	}
}

// No config may leave two actions sharing a key, whatever it asks for.
func TestSanitizeNeverProducesADuplicate(t *testing.T) {
	resolved, _ := sanitizeBindings(map[string]string{
		"card.archive":   "a",
		"card.copy":      "a",
		"card.move":      "q",
		"board.pin":      "tab",
		"card.moveLeft":  "x",
		"nav.quit":       "z",
		"card.teleport":  "w",
		"board.settings": "",
	})
	seen := map[string]string{}
	for id, k := range resolved {
		if prev, dup := seen[k]; dup {
			t.Errorf("%s and %s both hold %q", prev, id, k)
		}
		seen[k] = id
	}
}

func TestApplyConfigRebindsTheLiveKeymap(t *testing.T) {
	restoreBindings(t)
	ApplyConfig(store.Config{Keys: map[string]string{"card.add": "z"}})

	if !keys.Add.Enabled() || keys.Add.Keys()[0] != "z" {
		t.Errorf("keys.Add = %v, want z", keys.Add.Keys())
	}
	if hk("card.add") != "z" {
		t.Errorf("hk(card.add) = %q, want z", hk("card.add"))
	}

	ApplyConfig(store.Config{})
	if keys.Add.Keys()[0] != "a" {
		t.Errorf("keys.Add = %v, want the default a back", keys.Add.Keys())
	}
}

func TestHelpTextUsesReboundKeysOnEveryView(t *testing.T) {
	restoreBindings(t)
	refused, _ := ApplyConfig(store.Config{Keys: map[string]string{
		"board.rename":      "n",
		"board.pin":         "b",
		"card.reorderUp":    "U",
		"card.reorderDown":  "D",
		"card.archive":      "y",
		"board.unarchive":   "i",
		"board.archiveView": "Z",
		"board.picker":      "w",
		"card.moveLeft":     "<",
		"card.moveRight":    ">",
		"card.move":         "g",
		"card.delete":       "!",
		"board.unzoom":      "_",
		"card.edit":         "f",
	}})
	if len(refused) != 0 {
		t.Fatalf("test bindings were refused: %v", refused)
	}

	m := &Model{view: pickerView, pickerShowArchived: true}
	for _, want := range []string{
		"n rename", "b pin", "U/D reorder", "y archive", "i unarchive", "Z hide archived", "esc/w close",
	} {
		if got := m.helpText(); !strings.Contains(got, want) {
			t.Errorf("picker help %q does not contain %q", got, want)
		}
	}

	m.view, m.splitFocus, m.editField = splitView, 1, 0
	for _, want := range []string{"</> move", "g move to", "y archive"} {
		if got := m.helpText(); !strings.Contains(got, want) {
			t.Errorf("split help %q does not contain %q", got, want)
		}
	}

	m.view, m.editField = detailView, 0
	for _, want := range []string{"</> move", "g move to", "! delete", "_ back"} {
		if got := m.helpText(); !strings.Contains(got, want) {
			t.Errorf("detail help %q does not contain %q", got, want)
		}
	}
	m.editField = 1
	if got := m.helpText(); !strings.Contains(got, "enter/f edit") {
		t.Errorf("detail edit help %q does not contain rebound edit key", got)
	}
}

func TestApplyConfigRenamesAColumnWithoutTouchingTheStatus(t *testing.T) {
	restoreBindings(t)
	ApplyConfig(store.Config{Columns: []store.ColumnConfig{
		{Status: "HOLD", Label: "Waiting"},
	}})

	if statusDisplay[model.StatusHold] != "Waiting" {
		t.Errorf("HOLD label = %q, want Waiting", statusDisplay[model.StatusHold])
	}
	if statusShort[model.StatusHold] != "W" {
		t.Errorf("HOLD short = %q, want W derived from the new name",
			statusShort[model.StatusHold])
	}
	if statusDisplay[model.StatusDone] != "Done" {
		t.Errorf("untouched column moved: DONE = %q", statusDisplay[model.StatusDone])
	}
}

// The meta-bar status picker used to round-trip its label through ParseStatus,
// which only works while every label is also a status name.
func TestStatusChoicesMapARenamedLabelBackToItsStatus(t *testing.T) {
	restoreBindings(t)
	ApplyConfig(store.Config{Columns: []store.ColumnConfig{
		{Status: "DONE", Label: "Shipped"},
	}})

	labels, byLabel := statusChoices()
	if slices.Contains(labels, "Backlog") {
		t.Errorf("labels = %v, want no Backlog", labels)
	}
	if byLabel["Shipped"] != model.StatusDone {
		t.Errorf("byLabel[Shipped] = %q, want DONE", byLabel["Shipped"])
	}
	if _, err := model.ParseStatus("Shipped"); err == nil {
		t.Error("ParseStatus(\"Shipped\") succeeded — this test no longer guards the round-trip")
	}
}

func TestAnOverrideBeatsANewDefault(t *testing.T) {
	// A default added in a later release must not take a key the config has
	// already claimed. board.tags gaining `t` reverted an existing t binding
	// and told the user about it at startup, which is a config silently
	// changing meaning on upgrade.
	resolved, refused := sanitizeBindings(map[string]string{"board.picker": "t"})

	if got := resolved["board.picker"]; got != "t" {
		t.Errorf("board.picker = %q, want the override honoured", got)
	}
	if got, bound := resolved["board.tags"]; bound {
		t.Errorf("board.tags = %q, want it left unbound rather than displacing the override", got)
	}
	for _, r := range refused {
		if r == "board.picker" {
			t.Error("the override was refused")
		}
	}
}

func TestUnboundActionDoesNotKeepItsDefaultKey(t *testing.T) {
	// The keymap is rebuilt from the defaults, so an action left unbound has
	// to be actively cleared — otherwise it keeps the very key the override
	// was just given, and both fire.
	t.Cleanup(func() { applyKeyBindings(nil) })
	applyKeyBindings(map[string]string{"board.picker": "t"})

	if keys.TagPicker.Enabled() && len(keys.TagPicker.Keys()) > 0 {
		t.Errorf("board.tags still bound to %v after its default was taken", keys.TagPicker.Keys())
	}
	if got := keys.BoardPicker.Keys(); len(got) != 1 || got[0] != "t" {
		t.Errorf("board.picker = %v, want t", got)
	}
}
