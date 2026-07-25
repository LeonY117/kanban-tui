package tui

import (
	"strings"
	"testing"

	"github.com/LeonY117/kanban-tui/internal/model"
	"github.com/LeonY117/kanban-tui/internal/store"
	"github.com/charmbracelet/x/ansi"
)

// Entering edit mode must not re-flow the description: the editor has to wrap
// at the same column the read-only render does.
func TestDescriptionWrapsIdenticallyInEditMode(t *testing.T) {
	sandboxRoot(t)
	s := store.New("")
	desc := "The quick brown fox jumps over the lazy dog and keeps running until it reaches the end of the line, then wraps.\n\nSecond paragraph here to be sure."
	if _, err := s.Add("ticket", desc, model.StatusTodo, nil, "", "test"); err != nil {
		t.Fatal(err)
	}

	descPanel := func(m *Model) []string {
		var body []string
		lines := strings.Split(ansi.Strip(m.View()), "\n")
		start := -1
		for i, l := range lines {
			if strings.Contains(l, "Description") {
				start = i
			}
		}
		if start < 0 {
			t.Fatal("no description panel rendered")
		}
		for _, l := range lines[start+1:] {
			trimmed := strings.TrimRight(l, " │")
			if strings.Contains(l, "╰") {
				break
			}
			body = append(body, trimmed)
		}
		return body
	}

	for _, width := range []int{80, 100, 132} {
		m, err := NewModel(s, "")
		if err != nil {
			t.Fatal(err)
		}
		m.width, m.height, m.ready = width, 20, true
		m.enterSplit()
		m.splitFocus = 1
		m.editField = 2

		read := descPanel(m)
		m.editDesc.Focus()
		edit := descPanel(m)

		for i := range read {
			if i >= len(edit) {
				t.Fatalf("width %d: edit mode rendered fewer lines than read mode", width)
			}
			if strings.TrimRight(read[i], " ") != strings.TrimRight(edit[i], " ") {
				t.Errorf("width %d line %d re-flowed on entering edit mode:\n read: %q\n edit: %q",
					width, i, read[i], edit[i])
			}
		}
	}
}
