package tui

import (
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

// The popup must not resize as sections change — a page that grows and shrinks
// under the cursor was the first thing that felt wrong.
func TestSettingsHeightIsConstantAcrossSections(t *testing.T) {
	m := settingsModel(t)
	want := 0
	for _, sec := range []settingsSection{sectionShortcuts, sectionColumns, sectionAbout} {
		m.settings.section = sec
		got := len(strings.Split(crop(m.View()), "\n"))
		if want == 0 {
			want = got
		}
		if got != want {
			t.Errorf("%s renders %d rows, want %d — the popup is resizing",
				sectionNames[sec], got, want)
		}
	}
}

// A list longer than the popup has to scroll, and the footer must not be what
// scrolls away — it carries the conflict warning.
func TestLongSectionScrollsButKeepsItsFooter(t *testing.T) {
	m := settingsModel(t)
	m.settings.idx = len(bindActions) - 1
	out := crop(m.View())

	if !strings.Contains(out, "more") {
		t.Error("no scroll indicator on a list longer than the popup")
	}
	if !strings.Contains(out, "esc close") {
		t.Error("the footer scrolled out of view")
	}
	last := bindActions[len(bindActions)-1]
	if !strings.Contains(out, last.label) {
		t.Errorf("the focused row %q is not on screen", last.label)
	}
}

func TestLockedRowsRefuseToRebind(t *testing.T) {
	m := settingsModel(t)
	m.settings.idx = findAction(t, "nav.quit")
	press(m, "enter")

	if m.settings.capturing {
		t.Error("a locked row entered capture mode")
	}
	if !strings.Contains(m.settings.notice, "can't be rebound") {
		t.Errorf("notice = %q, want a refusal", m.settings.notice)
	}
}

func TestReservedKeyIsRefusedDuringCapture(t *testing.T) {
	m := settingsModel(t)
	m.settings.idx = findAction(t, "card.archive")
	press(m, "enter", "left")

	if got := m.settings.binds["card.archive"]; got != "x" {
		t.Errorf("card.archive = %q, want the arrow refused", got)
	}
	if !strings.Contains(m.settings.notice, "reserved") {
		t.Errorf("notice = %q, want it to say the key is reserved", m.settings.notice)
	}
}

func TestAlreadyDefaultIsReportedInsteadOfAPointlessReset(t *testing.T) {
	m := settingsModel(t)
	m.settings.idx = findAction(t, "card.add")
	press(m, "r")
	if m.settings.notice != "already default" {
		t.Errorf("notice = %q, want \"already default\"", m.settings.notice)
	}
	if m.settings.dirty {
		t.Error("a no-op reset marked the page dirty")
	}
}
