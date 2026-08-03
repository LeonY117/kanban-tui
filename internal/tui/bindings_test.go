package tui

import (
	"slices"
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

// A hand-edited file can ask for a key another action already holds. The
// override loses; the action keeps its own default, which is still free
// precisely because it was reserved for it.
func TestSanitizeDropsAnOverrideThatCollidesWithADefault(t *testing.T) {
	resolved, refused := sanitizeBindings(map[string]string{"card.archive": "a"}) // "a" is card.add
	if !slices.Contains(refused, "card.archive") {
		t.Errorf("refused = %v, want card.archive", refused)
	}
	if resolved["card.archive"] != "x" {
		t.Errorf("card.archive = %q, want its default x", resolved["card.archive"])
	}
	if resolved["card.add"] != "a" {
		t.Errorf("card.add = %q, want to keep a", resolved["card.add"])
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
