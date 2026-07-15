package tui

import (
	"testing"

	"github.com/LeonY117/kanban-tui/internal/model"
	"github.com/LeonY117/kanban-tui/internal/store"
)

// Regression test: cursors must always span len(model.ColumnOrder). A
// fixed-size [5]int array panicked with "index out of range [5]" on any
// ticket move after WAITING was added as a sixth column.
func TestCursorsSpanAllColumns(t *testing.T) {
	st := store.New(t.TempDir())
	for _, s := range model.AllStatuses {
		if _, err := st.Add("ticket "+string(s), "", s, nil, "", ""); err != nil {
			t.Fatalf("seeding %s: %v", s, err)
		}
	}

	m, err := NewModel(st, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(m.cursors) != len(model.ColumnOrder) {
		t.Fatalf("cursors len = %d, want %d", len(m.cursors), len(model.ColumnOrder))
	}

	// The panic path: move a ticket out of every column, clamping cursors
	// each time — moveTicket calls clampCursors, which walks ColumnOrder.
	for col := range model.ColumnOrder {
		m.focusedCol = col
		m.clampCursors()
		m.moveTicket(1)
	}
}
