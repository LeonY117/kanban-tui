package tui

import (
	"fmt"
	"strings"
	"testing"
	"unicode/utf8"

	tea "github.com/charmbracelet/bubbletea"
)

func settingsModel(t *testing.T) *Model {
	t.Helper()
	m := testModel(t, "Wire up the auth callback")
	m.width, m.height, m.ready = 120, 34, true
	m.enterSettings()
	return m
}

func press(m *Model, keys ...string) {
	for _, k := range keys {
		var msg tea.KeyMsg
		switch k {
		case "enter", "esc", "tab", "backspace", "up", "down":
			msg = tea.KeyMsg{Type: map[string]tea.KeyType{
				"enter": tea.KeyEnter, "esc": tea.KeyEsc, "tab": tea.KeyTab,
				"backspace": tea.KeyBackspace, "up": tea.KeyUp, "down": tea.KeyDown,
			}[k]}
		default:
			msg = tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(k)}
		}
		m.updateSettings(msg)
	}
}

// crop cuts the popup's columns out of the full-screen render.
func crop(full string) string {
	lines := strings.Split(full, "\n")
	x0, x1 := -1, -1
	for _, l := range lines {
		i := strings.Index(l, "\u256d\u2500Settings")
		if i < 0 {
			continue
		}
		x0 = utf8.RuneCountInString(l[:i])
		r := []rune(l)
		for j := x0 + 1; j < len(r); j++ {
			if r[j] == '\u256e' {
				x1 = j
				break
			}
		}
	}
	if x0 < 0 || x1 < 0 {
		return "(popup not found)"
	}
	var out []string
	started := false
	for _, l := range lines {
		r := []rune(l)
		if x1 >= len(r) {
			continue
		}
		seg := string(r[x0 : x1+1])
		if strings.HasPrefix(seg, "\u256d") {
			started = true
		}
		if started {
			out = append(out, seg)
		}
		if started && strings.HasPrefix(seg, "\u2570") {
			break
		}
	}
	return strings.Join(out, "\n")
}

func TestZZSettingsVariants(t *testing.T) {
	for v := 0; v < headerVariantCount; v++ {
		m := settingsModel(t)
		m.settings.variant = v
		m.settings.section = sectionColumns
		fmt.Printf("\n===== %d: %s =====\n", v, headerVariantNames[v])
		fmt.Println(crop(m.View()))
	}
}

func TestZZSettingsStates(t *testing.T) {
	// Conflict: bind "archive" (idx 9) onto m, which "move" already has.
	m := settingsModel(t)
	press(m, "j", "j", "j", "j", "j", "j", "j", "j", "j", "enter", "m")
	fmt.Println("\n===== CONFLICT: archive stole m from move =====")
	fmt.Println(crop(m.View()))
	fmt.Println("conflicts:", m.settings.conflictCount())

	press(m, "esc")
	fmt.Println("\n===== ESC ONCE: refused =====")
	fmt.Println(crop(m.View()))
	fmt.Println("still in settings:", m.view == settingsView)

	press(m, "esc")
	fmt.Println("\n===== ESC TWICE: conflicting edits undone, closed =====")
	fmt.Println("view is board:", m.view != settingsView, "| notice:", m.notice)
	fmt.Println("archive is back to:", m.settings.binds["card.archive"])

	// Columns
	m2 := settingsModel(t)
	press(m2, "2", "j", "j", "j", "j", "enter")
	press(m2, "W", "a", "i", "t", "i", "n", "g")
	fmt.Println("\n===== COLUMNS: renaming HOLD =====")
	fmt.Println(crop(m2.View()))

	press(m2, "enter")
	fmt.Println("\n===== COLUMNS: after rename =====")
	fmt.Println(crop(m2.View()))

	// Reset-all confirm
	press(m2, "D")
	fmt.Println("\n===== RESET ALL: confirm gate =====")
	fmt.Println(crop(m2.View()))

	// About
	m3 := settingsModel(t)
	press(m3, "3")
	fmt.Println("\n===== ABOUT =====")
	fmt.Println(crop(m3.View()))
}
