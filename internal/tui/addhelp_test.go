package tui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
)

// The popup's interior truncates rather than wraps, so the help line has a
// hard budget — adding a hint silently ate "esc: cancel" once already.
func TestAddHelpLineFitsPopup(t *testing.T) {
	m := testModel(t)
	m.Update(keyPress("a"))
	if got, limit := ansi.StringWidth(m.addHelpLine()), m.addInnerWidth(); got > limit {
		t.Errorf("add help line is %d cells wide, popup interior is %d — the tail is clipped", got, limit)
	}
}

// And at the narrowest supported terminal, where the interior is 54 cells
// rather than 62 — the emoji hint fit the wide case and took "esc: cancel"
// with it at the narrow one.
func TestAddHelpLineFitsANarrowPopup(t *testing.T) {
	m := testModel(t)
	m.width, m.height = minTerminalWidth, minTerminalHeight
	m.Update(keyPress("a"))

	line := m.addHelpLine()
	if got, limit := ansi.StringWidth(line), m.addInnerWidth(); got > limit {
		t.Errorf("add help line is %d cells wide, popup interior is %d", got, limit)
	}
	if !strings.Contains(line, "esc: cancel") {
		t.Errorf("the way out of the popup was dropped instead of a hint: %q", line)
	}
}
