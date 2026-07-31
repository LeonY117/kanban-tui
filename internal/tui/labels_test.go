package tui

import (
	"slices"
	"strings"
	"testing"

	"github.com/LeonY117/kanban-tui/internal/model"
	"github.com/LeonY117/kanban-tui/internal/store"
)

// applyConfigForTest sets labels and puts the defaults back afterwards, so one
// test's renames can't leak into the next.
func applyConfigForTest(t *testing.T, cfg store.Config) {
	t.Helper()
	t.Cleanup(func() { ApplyConfig(store.Config{}) })
	ApplyConfig(cfg)
}

func TestApplyConfigRenamesAColumn(t *testing.T) {
	applyConfigForTest(t, store.Config{StatusLabels: map[string]string{"HOLD": "Waiting"}})

	if got := statusDisplay[model.StatusHold]; got != "Waiting" {
		t.Errorf("HOLD display = %q, want %q", got, "Waiting")
	}
	// Untouched columns keep their built-in names.
	if got := statusDisplay[model.StatusDone]; got != "Done" {
		t.Errorf("DONE display = %q, want %q", got, "Done")
	}
}

// The count strip would otherwise keep showing "H" next to a column called
// Waiting.
func TestApplyConfigDerivesTheShortLabelFromTheNewName(t *testing.T) {
	applyConfigForTest(t, store.Config{StatusLabels: map[string]string{"HOLD": "Waiting"}})

	if got := statusShort[model.StatusHold]; got != "W" {
		t.Errorf("HOLD short = %q, want %q", got, "W")
	}
	if got := statusShort[model.StatusDoing]; got != "Do" {
		t.Errorf("DOING short = %q, want the built-in %q", got, "Do")
	}
}

func TestApplyConfigShortLabelOverrideBeatsTheDerivedOne(t *testing.T) {
	applyConfigForTest(t, store.Config{
		StatusLabels:      map[string]string{"HOLD": "Waiting"},
		StatusLabelsShort: map[string]string{"HOLD": "Wt"},
	})

	if got := statusShort[model.StatusHold]; got != "Wt" {
		t.Errorf("HOLD short = %q, want %q", got, "Wt")
	}
}

func TestApplyConfigResetsPreviousLabels(t *testing.T) {
	applyConfigForTest(t, store.Config{StatusLabels: map[string]string{"HOLD": "Waiting"}})
	ApplyConfig(store.Config{})

	if got := statusDisplay[model.StatusHold]; got != "Hold" {
		t.Errorf("HOLD display = %q, want the default %q back", got, "Hold")
	}
}

// The meta-bar picker shows labels and has to map the chosen one back to a
// status. It used to round-trip through model.ParseStatus, which only works
// while the label happens to be a status name — pick "Shipped" and the edit
// would silently do nothing.
func TestStatusChoicesMapRenamedLabelsBackToTheirStatus(t *testing.T) {
	applyConfigForTest(t, store.Config{StatusLabels: map[string]string{
		"HOLD": "Waiting",
		"DONE": "Shipped",
	}})

	labels, byLabel := statusChoices()
	if !slices.Equal(labels, []string{"Todo", "Doing", "Shipped", "Waiting"}) {
		t.Errorf("labels = %v, want them in column order with the renames applied", labels)
	}
	if got := byLabel["Shipped"]; got != model.StatusDone {
		t.Errorf("byLabel[Shipped] = %q, want %q", got, model.StatusDone)
	}
	if got := byLabel["Waiting"]; got != model.StatusHold {
		t.Errorf("byLabel[Waiting] = %q, want %q", got, model.StatusHold)
	}
	if _, err := model.ParseStatus("Shipped"); err == nil {
		t.Error("ParseStatus(\"Shipped\") succeeded, so this test no longer guards the round-trip")
	}
}

// Backlog is reachable by moving a card, not from this picker. Keeping it out
// matches the fixed list statusChoices replaced.
func TestStatusChoicesLeaveOutBacklog(t *testing.T) {
	applyConfigForTest(t, store.Config{})

	labels, byLabel := statusChoices()
	if slices.Contains(labels, "Backlog") {
		t.Errorf("labels = %v, want no Backlog", labels)
	}
	if len(byLabel) != len(model.ColumnOrder)-1 {
		t.Errorf("byLabel has %d entries, want %d", len(byLabel), len(model.ColumnOrder)-1)
	}
}

// The point of the whole feature: the configured name is what you read on the
// board, and the stored status is untouched.
func TestRenamedColumnTitleIsWhatTheBoardDraws(t *testing.T) {
	applyConfigForTest(t, store.Config{StatusLabels: map[string]string{"HOLD": "Waiting"}})

	m := testModel(t, "a card")
	out := m.View()

	if !strings.Contains(out, "Waiting") {
		t.Error("board does not draw the renamed column title")
	}
	if strings.Contains(out, "Hold") {
		t.Error("board still draws the built-in title Hold")
	}
	if got := m.board.Tickets[0].Status; got != model.StatusTodo {
		t.Errorf("ticket status = %q, want %q — labels must not touch stored data", got, model.StatusTodo)
	}
}

// The board picker's count strip reads the short labels, so a renamed column
// has to change there too: "2H" would still say Hold in shorthand.
func TestCountStripUsesTheRenamedShortLabel(t *testing.T) {
	applyConfigForTest(t, store.Config{StatusLabels: map[string]string{"HOLD": "Waiting"}})

	got := formatCounts(map[model.Status]int{model.StatusHold: 2})
	if !strings.Contains(got, "2W") {
		t.Errorf("count strip = %q, want it to contain %q", got, "2W")
	}
	if strings.Contains(got, "2H") {
		t.Errorf("count strip = %q, still using the built-in short label", got)
	}
}
