package tui

import (
	"sort"

	"github.com/charmbracelet/bubbles/key"
)

// bindingTargets maps an action id to the keyMap field it drives. Only actions
// listed here can be rebound at all — an id in config.json that isn't here is
// from a different build and is ignored.
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
		"card.status":      &km.Status,
		"card.assign":      &km.Assign,
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
	}
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

// sanitizeBindings decides which of a config's overrides may be applied.
//
// Three kinds are refused: ids this build doesn't know, the locked navigation
// floor (a config that can rebind `q` can lock you out of your own board), and
// anything that would leave two actions sharing a key. It returns the surviving
// per-action keys and the ids whose override was refused.
//
// Collisions resolve by giving un-overridden actions their default first —
// defaults are mutually exclusive, so that pass can't fail — and then handing
// out overrides in bindActions order, dropping any that arrive to find their
// key taken. A dropped action falls back to its own default, which is still
// free precisely because it was reserved for it.
func sanitizeBindings(overrides map[string]string) (map[string]string, []string) {
	locked := map[string]bool{}
	for _, a := range bindActions {
		locked[a.id] = a.locked
	}

	// Ignore anything we must not honour before doing any assignment, so the
	// collision pass only ever sees candidates that are otherwise legal.
	valid := map[string]string{}
	var refused []string
	for id, k := range overrides {
		isLocked, known := locked[id]
		switch {
		case !known: // an id from some other build
			refused = append(refused, id)
		case isLocked: // the floor is not the config's to move
			refused = append(refused, id)
		case k == "":
			refused = append(refused, id)
		default:
			valid[id] = k
		}
	}

	resolved := map[string]string{}
	taken := map[string]string{} // key -> action id holding it

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
			refused = append(refused, a.id)
			resolved[a.id] = a.def
			taken[a.def] = a.id
			continue
		}
		resolved[a.id] = want
		taken[want] = a.id
	}

	sort.Strings(refused)
	return resolved, refused
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
