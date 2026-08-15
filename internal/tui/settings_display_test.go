package tui

import (
	"strings"
	"testing"

	"github.com/LeonY117/kanban-tui/internal/store"
	"github.com/LeonY117/kanban-tui/internal/termwidth"
)

func openDisplaySection(t *testing.T, m *Model) {
	t.Helper()
	m.Update(keyPress("?"))
	if m.view != settingsView {
		t.Fatal("? should open settings")
	}
	m.Update(keyPress("3"))
	if m.settings.section != sectionDisplay {
		t.Fatal("3 should reach the Display section")
	}
}

// Moving the cursor renders the board under that profile, which is the whole
// mechanism: you pick the one whose sample box lines up.
func TestDisplaySectionPreviewsAsTheCursorMoves(t *testing.T) {
	m := testModel(t, "🐛 a ticket")
	m.termWidth = 160
	openDisplaySection(t, m)

	if got := m.widthProfile(); got != termwidth.Grapheme {
		t.Fatalf("first row should preview grapheme, got %v", got)
	}
	m.Update(keyPress("j"))
	if got := m.widthProfile(); got != termwidth.Narrow {
		t.Errorf("second row should preview narrow, got %v", got)
	}
	// The preview is a real render: the reserve moves with it.
	m.View()
	if m.width != 160-termwidth.Reserve {
		t.Errorf("layout width = %d, want the reserve held back while previewing narrow", m.width)
	}
	m.Update(keyPress("k"))
	m.View()
	if m.width != 160 {
		t.Errorf("layout width = %d, want the full window back on grapheme", m.width)
	}
}

// Previewing alone must not change anything — the choice is the enter.
func TestDisplayPreviewIsNotAChoice(t *testing.T) {
	m := testModel(t, "🐛 a ticket")
	m.termWidth = 160
	openDisplaySection(t, m)
	m.Update(keyPress("j")) // preview narrow, don't choose it

	if m.settings.width != termwidth.Grapheme {
		t.Error("moving the cursor should not change the chosen profile")
	}
	if m.settings.changedWidth || m.settings.dirty {
		t.Error("a preview should not mark the page dirty")
	}
}

func TestDisplayChoosingPersistsAndApplies(t *testing.T) {
	m := testModel(t, "🐛 a ticket")
	m.termWidth = 160
	openDisplaySection(t, m)
	m.Update(keyPress("j"))
	m.Update(keyPress("enter"))

	if m.settings.width != termwidth.Narrow || !m.settings.changedWidth {
		t.Fatal("enter should choose the previewed profile")
	}
	m.Update(keyPress("esc")) // closes, saving
	if m.view == settingsView {
		t.Fatal("esc should close the settings popup")
	}

	if widthProfile != termwidth.Narrow {
		t.Errorf("the chosen profile should be live, got %v", widthProfile)
	}
	if cfg := store.LoadConfig(); cfg.TerminalWidth != "narrow" {
		t.Errorf("config holds %q, want narrow — the setting must outlive the session", cfg.TerminalWidth)
	}
	t.Cleanup(func() { ApplyConfig(store.Config{}) })
}

// The section has to show what it claims: the options, and a sample to judge
// them by.
func TestDisplaySectionRendersOptionsAndSample(t *testing.T) {
	m := testModel(t, "🐛 a ticket")
	m.termWidth = 160
	openDisplaySection(t, m)

	view := m.View()
	for _, want := range []string{"grapheme", "narrow", "Codex", "plain text", "🐛"} {
		if !strings.Contains(view, want) {
			t.Errorf("the Display section should show %q", want)
		}
	}
}

func TestSettingsNavigationIncludesAbout(t *testing.T) {
	m := testModel(t, "a ticket")
	openDisplaySection(t, m)

	m.Update(keyPress("4"))
	if m.settings.section != sectionAbout {
		t.Fatalf("4 reached %s, want About", sectionNames[m.settings.section])
	}
	m.Update(keyPress("tab"))
	if m.settings.section != sectionShortcuts {
		t.Errorf("tab from About reached %s, want Shortcuts", sectionNames[m.settings.section])
	}
	m.Update(keyPress("shift+tab"))
	if m.settings.section != sectionAbout {
		t.Errorf("shift+tab from Shortcuts reached %s, want About", sectionNames[m.settings.section])
	}
}

func TestDisplayChoicePersistsWhenQuittingSettings(t *testing.T) {
	m := testModel(t, "🐛 a ticket")
	openDisplaySection(t, m)
	m.Update(keyPress("j"))
	m.Update(keyPress("enter"))

	if _, cmd := m.updateSettings(keyPress("ctrl+c")); cmd == nil {
		t.Fatal("ctrl+c should still quit from settings")
	}
	if cfg := store.LoadConfig(); cfg.TerminalWidth != "narrow" {
		t.Errorf("config holds %q, want the choice saved before quitting", cfg.TerminalWidth)
	}
	t.Cleanup(func() { ApplyConfig(store.Config{}) })
}

// updateDirty assigns rather than accumulates, so a width change it doesn't
// count gets cleared by the next edit anywhere else on the page — and closing
// the popup then threw the chosen profile away without a word.
func TestDisplayChoiceSurvivesAnEditElsewhere(t *testing.T) {
	m := testModel(t, "🐛 a ticket")
	m.termWidth = 160
	openDisplaySection(t, m)
	m.Update(keyPress("j"))
	m.Update(keyPress("enter")) // choose narrow

	// Now touch a shortcut and put it straight back.
	m.Update(keyPress("1")) // Shortcuts
	before := m.settings.binds["card.add"]
	m.settings.binds["card.add"] = "z"
	m.settings.markBindingChanged("card.add")
	m.settings.binds["card.add"] = before
	m.settings.markBindingChanged("card.add")

	if !m.settings.dirty {
		t.Fatal("the page should still be dirty — a profile was chosen")
	}
	m.Update(keyPress("esc"))
	if cfg := store.LoadConfig(); cfg.TerminalWidth != "narrow" {
		t.Errorf("config holds %q, want narrow — the choice was discarded", cfg.TerminalWidth)
	}
	t.Cleanup(func() { ApplyConfig(store.Config{}) })
}

// The reserve can push a small window under the minimum. If the size guard
// runs before the profile is applied, m.width stays shrunk and moving back to
// grapheme can't undo it — the only way out was to resize the terminal.
func TestNarrowProfileDoesNotTrapASmallWindow(t *testing.T) {
	m := testModel(t, "🐛 a ticket")
	m.termWidth, m.height = minTerminalWidth+4, 40 // 54: fine wide, under the floor once reserved
	openDisplaySection(t, m)

	// The shrink lands on this render; with the guard ahead of the profile the
	// lockout only bites on the next one, when m.width is already 46.
	m.Update(keyPress("j")) // preview narrow
	m.View()
	m.View()

	m.Update(keyPress("k")) // back to grapheme
	if got := m.View(); strings.Contains(got, "Terminal too small") {
		t.Error("moving back to grapheme should restore the window, not stay stuck below the floor")
	}
}
