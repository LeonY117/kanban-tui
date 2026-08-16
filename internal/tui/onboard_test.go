package tui

import (
	"strings"
	"testing"

	"github.com/LeonY117/kanban-tui/internal/store"
	"github.com/LeonY117/kanban-tui/internal/termwidth"
)

// resetWidthProfile puts the package-level profile back, since these tests
// answer the question for real.
func resetWidthProfile(t *testing.T) {
	t.Helper()
	t.Cleanup(func() { ApplyConfig(store.Config{}) })
}

func askingModel(t *testing.T) *Model {
	t.Helper()
	resetWidthProfile(t)
	m := testModel(t, "🐛 fix the picker")
	m.termWidth = 160
	m.AskTerminalWidth()
	return m
}

// The question owns the screen: the options, and the sample to judge them by,
// with nothing else to read.
func TestFirstRunAsksOnAnEmptyScreen(t *testing.T) {
	m := askingModel(t)
	view := m.View()

	for _, want := range []string{"Emoji width", "grapheme", "narrow", "plain text"} {
		if !strings.Contains(view, want) {
			t.Errorf("the first-run question should show %q", want)
		}
	}
	// testModel seeds a board; none of it may show through.
	if strings.Contains(view, "fix the picker") || strings.Contains(view, "Todo") {
		t.Error("the board should not be drawn behind the question")
	}
}

// Moving the cursor re-renders everything under that profile. Same mechanism
// as the Display section: you pick the one that lines up.
func TestFirstRunPreviewsAsTheCursorMoves(t *testing.T) {
	m := askingModel(t)

	if got := m.widthProfile(); got != termwidth.Grapheme {
		t.Fatalf("first row should preview grapheme, got %v", got)
	}
	m.Update(keyPress("j"))
	m.View()
	if got := m.widthProfile(); got != termwidth.Narrow {
		t.Errorf("second row should preview narrow, got %v", got)
	}
	if m.width != 160-termwidth.Reserve {
		t.Errorf("layout width = %d, want the reserve held back while previewing narrow", m.width)
	}
	m.Update(keyPress("k"))
	m.View()
	if m.width != 160 {
		t.Errorf("layout width = %d, want the full window back on grapheme", m.width)
	}
}

func TestFirstRunEnterAnswersAndOpensTheBoard(t *testing.T) {
	m := askingModel(t)
	m.Update(keyPress("j"))
	m.Update(keyPress("enter"))

	if m.view != boardView {
		t.Fatalf("answering should drop onto the board, got view %v", m.view)
	}
	if widthProfile != termwidth.Narrow {
		t.Errorf("the answer should be live, got %v", widthProfile)
	}
	if cfg := store.LoadConfig(); cfg.TerminalWidth != "narrow" {
		t.Errorf("config holds %q, want narrow", cfg.TerminalWidth)
	}
}

// The bug this guards: storing only the non-default, the way keys and labels
// are stored, makes "grapheme" indistinguishable from never having been asked
// — so the question comes back every launch for everyone it didn't apply to.
func TestAnsweringWithTheDefaultStillCountsAsAnswered(t *testing.T) {
	m := askingModel(t)
	m.Update(keyPress("enter")) // the cursor starts on grapheme

	cfg := store.LoadConfig()
	if cfg.TerminalWidth != "grapheme" {
		t.Fatalf("config holds %q, want grapheme written out explicitly", cfg.TerminalWidth)
	}
}

// Esc is not an answer. A profile nobody chose reads exactly like one they did
// once it is in config.json, so the question stays until it is answered.
func TestFirstRunEscDoesNotDismissTheQuestion(t *testing.T) {
	m := askingModel(t)
	m.Update(keyPress("j")) // previewing narrow, then trying to back out
	m.Update(keyPress("esc"))

	if m.view != onboardView {
		t.Errorf("esc should leave the question up, got view %v", m.view)
	}
	if cfg := store.LoadConfig(); cfg.TerminalWidth != "" {
		t.Errorf("config holds %q, want nothing written by an esc", cfg.TerminalWidth)
	}
}

// Quitting isn't an answer, so nothing is recorded and the question returns.
func TestFirstRunQuitRecordsNothing(t *testing.T) {
	m := askingModel(t)
	m.Update(keyPress("q"))

	if cfg := store.LoadConfig(); cfg.TerminalWidth != "" {
		t.Errorf("config holds %q, want nothing written on quit", cfg.TerminalWidth)
	}
}

// Choosing the default from the settings page has to count as an answer for
// the same reason enter does here.
func TestDisplaySectionWritesTheDefaultOutToo(t *testing.T) {
	resetWidthProfile(t)
	m := testModel(t, "🐛 fix the picker")
	m.termWidth = 160
	openDisplaySection(t, m)

	m.Update(keyPress("j")) // narrow
	m.Update(keyPress("enter"))
	m.Update(keyPress("k")) // back to grapheme
	m.Update(keyPress("enter"))
	m.Update(keyPress("esc")) // close, saving

	if cfg := store.LoadConfig(); cfg.TerminalWidth != "grapheme" {
		t.Errorf("config holds %q, want grapheme written out", cfg.TerminalWidth)
	}
}
