package tui

import (
	"reflect"
	"testing"

	"github.com/charmbracelet/bubbles/key"

	"github.com/LeonY117/kanban-tui/internal/store"
)

// liveKeyHolders maps every key the live keymap answers to, aliases included,
// to the field holding it. This is the check that matters: the settings list
// only knows one key per action, but a binding can answer to several.
func liveKeyHolders(t *testing.T) map[string]string {
	t.Helper()
	holders := map[string]string{}
	km := reflect.ValueOf(keys)
	kmType := km.Type()
	for i := 0; i < km.NumField(); i++ {
		id := kmType.Field(i).Name
		b := km.Field(i).Interface().(key.Binding)
		for _, k := range b.Keys() {
			if prev, dup := holders[k]; dup {
				t.Errorf("%s and %s both answer to %q", prev, id, k)
			}
			holders[k] = id
		}
	}
	return holders
}

func TestLiveKeyHoldersIncludesFixedBindingsOutsideSettings(t *testing.T) {
	restoreBindings(t)
	holders := liveKeyHolders(t)
	for keyName, field := range map[string]string{
		"0":           "Zero",
		"4":           "Four",
		"alt+enter":   "NewLine",
		"shift+enter": "NewLine",
		"ctrl+j":      "NewLine",
	} {
		if got := holders[keyName]; got != field {
			t.Errorf("holder of %q = %q, want %s", keyName, got, field)
		}
	}
}

// The property the old single-pass sanitiser claimed and didn't have. No two
// actions may share a key, whatever the config asks for.
//
// Totality is now conditional rather than absolute: an action may end up with
// no key, but only when an override claimed its default by name. Anything else
// losing its key is a bug, and the caller is told about the ones that do —
// both cmd/tui.go and the settings notice name them.
func TestSanitizeIsInjectiveAndOnlyDropsDisplacedDefaults(t *testing.T) {
	cases := []map[string]string{
		{},
		{"card.add": "e", "card.edit": "e"},    // both want one key
		{"card.add": "x", "card.archive": "q"}, // refused override falls back onto a taken default
		{"card.add": "x", "card.archive": "a"}, // a legal swap
		{"card.add": "z", "card.edit": "a", "card.archive": "z"},
		{"card.archive": "left"},   // an arrow alias of the floor
		{"card.archive": "ctrl+c"}, // an alias of quit
		{"card.archive": "3"},      // a column jump
		{"nav.quit": "Q", "card.teleport": "w", "card.copy": ""},
		{"card.add": "b", "card.edit": "c", "card.copy": "e", "card.archive": "a"},
	}
	for i, overrides := range cases {
		resolved, _ := sanitizeBindings(overrides)

		claimed := map[string]bool{}
		for _, k := range resolved {
			claimed[k] = true
		}

		seen := map[string]string{}
		for _, a := range bindActions {
			k, ok := resolved[a.id]
			if !ok || k == "" {
				// Acceptable only because something else was given this
				// action's default key outright.
				if !claimed[a.def] {
					t.Errorf("case %d: %s ended up unbound with its default %q still free",
						i, a.id, a.def)
				}
				continue
			}
			if prev, dup := seen[k]; dup {
				t.Errorf("case %d: %s and %s both hold %q", i, prev, a.id, k)
			}
			seen[k] = a.id
		}
	}
}

// Rebinding onto a key the floor answers to via an alias used to be accepted,
// because the settings list only knows nav.left as "h". The handler for the
// alias runs first, so the new binding was dead — and doing it to the settings
// key meant settings could never be reopened.
func TestAliasesOfTheFloorAreReserved(t *testing.T) {
	restoreBindings(t)
	for _, k := range []string{"left", "right", "up", "down", "ctrl+c", "0", "4"} {
		if !reservedKeys()[k] {
			t.Errorf("%q is not reserved but the TUI already answers to it", k)
		}
		resolved, refused := sanitizeBindings(map[string]string{"board.settings": k})
		if len(refused) == 0 {
			t.Errorf("binding settings to %q was accepted", k)
		}
		if resolved["board.settings"] != "?" {
			t.Errorf("board.settings = %q after refusing %q, want ?",
				resolved["board.settings"], k)
		}
	}
}

// No rebindable default may sit on a reserved key, or the action could never
// hold its own default.
func TestNoRebindableDefaultIsReserved(t *testing.T) {
	reserved := reservedKeys()
	for _, a := range bindActions {
		if a.locked {
			continue
		}
		if reserved[a.def] {
			t.Errorf("%s defaults to %q, which is reserved", a.id, a.def)
		}
	}
}

// Whatever a config asks for, the keymap the TUI actually dispatches on must
// never have two fields answering to one key.
func TestLiveKeymapStaysUniqueAfterApply(t *testing.T) {
	restoreBindings(t)
	for _, overrides := range []map[string]string{
		{"card.add": "e", "card.edit": "e"},
		{"card.add": "x", "card.archive": "q"},
		{"board.settings": "left", "card.copy": "ctrl+c"},
		{"card.add": "x", "card.archive": "a"},
	} {
		ApplyConfig(store.Config{Keys: overrides})
		liveKeyHolders(t)
	}
}

// Every action the settings page offers must actually drive something.
func TestEveryOfferedActionHasATarget(t *testing.T) {
	km := defaultKeyMap()
	targets := bindingTargets(&km)
	for _, a := range bindActions {
		if _, ok := targets[a.id]; !ok {
			t.Errorf("%s is offered in settings but drives no binding", a.id)
		}
	}
}
