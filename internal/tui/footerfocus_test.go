package tui

import (
	"strings"
	"testing"

	"github.com/LeonY117/kanban-tui/internal/model"
)

// Prototype scaffolding (KA3) — goes when footerfocus.go goes.

// bottomOfColumn puts the cursor on the last card of the focused column. It
// sets the cursor rather than pressing j, which would trip the fall-through
// itself and leave a test measuring the state it meant to set up.
func bottomOfColumn(t *testing.T, m *Model) {
	t.Helper()
	count := len(m.visibleTickets(model.ColumnOrder[m.focusedCol]))
	if count == 0 {
		t.Fatalf("setup: column %d is empty", m.focusedCol)
	}
	m.cursors[m.focusedCol] = count - 1
	if !m.atColumnBottom() {
		t.Fatalf("setup: cursor not at the bottom of column %d", m.focusedCol)
	}
}

// The default has to be the old behaviour: j at the bottom clamps, as it always
// did, so the trial mode is opt-in rather than something you discover by
// accident.
func TestFallthroughIsOffByDefault(t *testing.T) {
	m, _ := boardWith(t, "one|TODO", "two|TODO")
	m.focusedCol = 1
	bottomOfColumn(t, m)

	m.Update(keyPress("j"))
	if m.footerFocus {
		t.Error("j reached the footer with the trial mode off")
	}
}

func TestFallthroughReachesTheBoardNameAndOpensIt(t *testing.T) {
	m, _ := boardWith(t, "one|TODO", "two|TODO")
	setDesc(t, "", "what this board is for")
	m.reload()
	m.focusedCol = 1

	m.Update(keyPress("z"))
	if !m.navFallthrough {
		t.Fatal("z did not turn the trial mode on")
	}

	bottomOfColumn(t, m)
	m.Update(keyPress("j"))
	if !m.footerFocus {
		t.Fatal("j at the bottom did not reach the footer")
	}
	// The cursor must not have moved on past the last card as well.
	if got := m.cursors[m.focusedCol]; got != 1 {
		t.Errorf("cursor = %d, want it left on the last card (1)", got)
	}

	m.Update(keyPress("enter"))
	if m.view != infoView {
		t.Fatalf("view = %v, want infoView", m.view)
	}
	if !strings.Contains(m.View(), "what this board is for") {
		t.Errorf("popup did not show the description:\n%s", m.View())
	}
}

func TestFallthroughReturnsToTheColumn(t *testing.T) {
	m, _ := boardWith(t, "one|TODO", "two|TODO")
	m.focusedCol = 1
	m.Update(keyPress("z"))
	bottomOfColumn(t, m)
	m.Update(keyPress("j"))

	m.Update(keyPress("k"))
	if m.footerFocus {
		t.Error("k did not leave the footer")
	}
	if got := m.cursors[m.focusedCol]; got != 1 {
		t.Errorf("cursor = %d, want to land back on the card it left (1)", got)
	}
}

// Sideways is a return to the cards — otherwise focus claims to be on the
// footer while a card key like x acts on a card.
func TestFallthroughClearedByLateralMove(t *testing.T) {
	m, _ := boardWith(t, "one|TODO")
	m.focusedCol = 1
	m.Update(keyPress("z"))
	bottomOfColumn(t, m)
	m.Update(keyPress("j"))
	if !m.footerFocus {
		t.Fatal("setup: never reached the footer")
	}

	m.Update(keyPress("l"))
	if m.footerFocus {
		t.Error("moving to the next column left focus on the footer")
	}
}

// Turning the mode back off must not leave focus stranded on the footer.
func TestTogglingOffReleasesTheFooter(t *testing.T) {
	m, _ := boardWith(t, "one|TODO")
	m.focusedCol = 1
	m.Update(keyPress("z"))
	bottomOfColumn(t, m)
	m.Update(keyPress("j"))

	m.Update(keyPress("z"))
	if m.navFallthrough {
		t.Error("z did not turn the mode off")
	}
	if m.footerFocus {
		t.Error("focus stayed on the footer after the mode was switched off")
	}
}

// Focus is somewhere else, so nothing in the column is selected — but the
// column stays framed as the one you'd come back to.
func TestFallthroughDeselectsTheCardButKeepsTheColumn(t *testing.T) {
	m, _ := boardWith(t, "one|TODO", "two|TODO")
	m.width, m.height = 90, 20
	m.focusedCol = 1
	m.Update(keyPress("z"))
	bottomOfColumn(t, m)

	before := m.View()
	m.Update(keyPress("j"))
	after := m.View()

	if !m.footerFocus {
		t.Fatal("setup: never reached the footer")
	}
	if before == after {
		t.Error("reaching the footer changed nothing on screen")
	}
	// A selected card closes into its own rounded box; deselected, its rows
	// rejoin the continuous table. Colour is stripped in tests, so the corner
	// glyph is what tells them apart.
	if strings.Count(after, "╭") >= strings.Count(before, "╭") {
		t.Errorf("the card still looks selected while the footer holds focus:\n%s", after)
	}
	if !strings.Contains(after, "[1] Todo") {
		t.Errorf("the column lost its frame:\n%s", after)
	}
	// The cursor itself is remembered, so k lands back on the same card.
	if got := m.cursors[m.focusedCol]; got != 1 {
		t.Errorf("cursor = %d, want it remembered at 1", got)
	}
}
