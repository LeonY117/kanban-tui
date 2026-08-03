package tui

import (
	"sort"

	"github.com/charmbracelet/bubbles/key"
)

// bindingTargets maps an action id to the keyMap field it drives. Only actions
// listed here can be rebound at all — an id in config.json that isn't here is
// from a different build and is ignored.
//
// keys.Status and keys.Assign are deliberately absent: nothing in the TUI
// handles them, so offering them as rebindable would be a lie. They are still
// declared in keys.go; removing them is a separate cleanup.
func bindingTargets(km *keyMap) map[string]*key.Binding {
	return map[string]*key.Binding{
		"nav.left":  &km.Left,
		"nav.right": &km.Right,
		"nav.up":    &km.Up,
		"nav.down":  &km.Down,
		"nav.enter": &km.Enter,
		"nav.esc":   &km.Esc,
		"nav.quit":  &km.Quit,

		"card.add":         &km.Add,
		"card.edit":        &km.Edit,
		"card.archive":     &km.Archive,
		"card.delete":      &km.Delete,
		"card.move":        &km.Move,
		"card.copy":        &km.Copy,
		"card.moveLeft":    &km.MoveLeft,
		"card.moveRight":   &km.MoveRight,
		"card.reorderUp":   &km.MoveUp,
		"card.reorderDown": &km.MoveDown,

		"board.picker":      &km.BoardPicker,
		"board.archiveView": &km.ArchiveView,
		"board.unarchive":   &km.Unarchive,
		"board.pin":         &km.Pin,
		"board.rename":      &km.Rename,
		"board.layout":      &km.Layout,
		"board.rowLayout":   &km.RowLayout,
		"board.zoom":        &km.Zoom,
		"board.unzoom":      &km.Unzoom,
		"board.panelNext":   &km.PanelNext,
		"board.panelPrev":   &km.PanelPrev,
		"board.settings":    &km.Help,
		"board.search":      &km.Search,
	}
}

// reservedKeys is every key the TUI already answers to that a rebind may not
// claim.
//
// It is not enough to reserve the locked floor's primary key: keys.Left answers
// to both "h" and "left", and keys.Quit to both "q" and "ctrl+c". Binding an
// action to an alias looks accepted — nothing else in the settings list holds
// it — but the handler for the aliased binding runs first and shadows it. Bind
// the settings key to the left arrow and the arrow keeps navigating while `?`
// stops working, so settings can never be reopened.
//
// It also covers bindings that are handled but deliberately not offered for
// rebinding (the 0-4 column jumps, and the newline chord in the description
// editor), for the same reason.
func reservedKeys() map[string]bool {
	km := defaultKeyMap()
	targets := bindingTargets(&km)

	var bindings []key.Binding
	for _, a := range bindActions {
		if !a.locked {
			continue
		}
		if t, ok := targets[a.id]; ok {
			bindings = append(bindings, *t)
		}
	}
	bindings = append(bindings, km.Zero, km.One, km.Two, km.Three, km.Four, km.NewLine)

	out := map[string]bool{}
	for _, b := range bindings {
		for _, k := range b.Keys() {
			out[k] = true
		}
	}
	return out
}

// activeBindings is the key each action currently answers to, defaults merged
// with whatever survived sanitising. Read by the help lines so the footer can't
// advertise a key that was rebound out from under it.
var activeBindings = defaultBindings()

func defaultBindings() map[string]string {
	out := make(map[string]string, len(bindActions))
	for _, a := range bindActions {
		out[a.id] = a.def
	}
	return out
}

// sanitizeBindings decides which of a config's overrides may be applied, and
// guarantees the result binds every action to a distinct, usable key.
//
// Refused: ids this build doesn't know, the locked navigation floor (a config
// that can move `q` can lock you out of your own board), blanks, keys already
// reserved by something the settings page doesn't list, and anything that would
// leave two actions sharing a key.
//
// Collisions resolve by attempting the whole assignment and dropping the first
// override that can't be honoured, then starting over. Each round has strictly
// fewer overrides and the zero-override case is just the defaults, which are
// distinct by construction — so this terminates, and it can't produce a
// duplicate the way a single pass with a per-action fallback could. An earlier
// version let a refused override "fall back to its own default", which is only
// safe if that default was reserved for it; with {card.add: "e",
// card.edit: "e"} both ended up on "e".
//
// This fixed-point rule deliberately over-drops in some hand-edited configs.
// For example, {card.add: "z", card.edit: "z", card.archive: "e"} keeps only
// card.add, although a maximal assignment could keep card.add and archive.
// That outcome is safe, deterministic, and unreachable from settings because
// the UI blocks conflicts before saving; matching/backtracking here is not
// worth the extra complexity.
func sanitizeBindings(overrides map[string]string) (map[string]string, []string) {
	valid, refused := filterOverrides(overrides)
	for {
		resolved, clash := assignBindings(valid)
		if clash == "" {
			sort.Strings(refused)
			return resolved, refused
		}
		delete(valid, clash)
		refused = append(refused, clash)
	}
}

// filterOverrides drops what may never be applied, whatever else is asked for.
// Iteration order over the map isn't observable: it only fills another map and
// appends to refused, which the caller sorts.
func filterOverrides(overrides map[string]string) (map[string]string, []string) {
	reserved := reservedKeys()

	valid := map[string]string{}
	var refused []string
	for id, k := range overrides {
		action, known := bindActionsByID[id]
		switch {
		case !known, action.locked, k == "", reserved[k]:
			refused = append(refused, id)
		default:
			valid[id] = k
		}
	}
	return valid, refused
}

// assignBindings hands out keys, or names the first override it cannot honour.
func assignBindings(valid map[string]string) (map[string]string, string) {
	resolved := map[string]string{}
	taken := map[string]string{}
	for k := range reservedKeys() {
		taken[k] = "(reserved)"
	}

	// Actions with no surviving override take their default first. Defaults are
	// distinct, so this pass can't fail, and it reserves each such default
	// against the overrides handed out below.
	for _, a := range bindActions {
		if _, overridden := valid[a.id]; overridden && !a.locked {
			continue
		}
		resolved[a.id] = a.def
		taken[a.def] = a.id
	}
	for _, a := range bindActions {
		want, overridden := valid[a.id]
		if !overridden || a.locked {
			continue
		}
		if _, clash := taken[want]; clash {
			return nil, a.id
		}
		resolved[a.id] = want
		taken[want] = a.id
	}
	return resolved, ""
}

// applyKeyBindings rebuilds the live keymap from defaults plus the sanitised
// overrides, and reports the ids whose override was refused.
func applyKeyBindings(overrides map[string]string) []string {
	resolved, refused := sanitizeBindings(overrides)

	keys = defaultKeyMap()
	activeBindings = resolved
	targets := bindingTargets(&keys)

	for _, a := range bindActions {
		if a.locked {
			continue
		}
		k := resolved[a.id]
		if k == "" || k == a.def {
			continue
		}
		if t, ok := targets[a.id]; ok {
			*t = key.NewBinding(key.WithKeys(k), key.WithHelp(k, a.label))
		}
	}
	return refused
}

// hk is the key an action currently answers to, for help lines.
func hk(id string) string {
	if k, ok := activeBindings[id]; ok && k != "" {
		return k
	}
	return "?"
}
