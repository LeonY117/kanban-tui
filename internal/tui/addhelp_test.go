package tui

import (
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
