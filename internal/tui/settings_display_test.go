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
