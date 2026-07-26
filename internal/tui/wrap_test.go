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
	desc := strings.Join([]string{
		"The quick brown fox jumps over the lazy dog and keeps running until it reaches the end of the line, then wraps.",
		"",
		"Notion transcript READ and folded in — it added the daily-management-report opportunity (~8h/wk), the group-company ODBC feeds and P-class semantics.",
		"",
		"  - an indented bullet whose text is long enough to need wrapping at every width under test",
		"  - https://example.com/a/very/long/url/that/cannot/be/broken/on/spaces/at/all/whatsoever",
		"",
		"⏰ Wed 10:00 call · Fri 10:30 in person — emoji and middots included.",
	}, "\n")
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

// The wrap port is only worth anything if it matches the real textarea, so
// check it against one directly rather than against a screenshot of the first
// viewport: a divergence past the first panelful, or one shared by both
// renders, is invisible to the view-level test above.
func TestWrapDescMatchesTheTextarea(t *testing.T) {
	inputs := map[string]string{
		"prose":         "The quick brown fox jumps over the lazy dog and keeps running until it reaches the end of the line, then wraps.",
		"hyphenated":    "the daily-management-report opportunity and the group-company ODBC feeds and P-class semantics",
		"long url":      "see https://example.com/a/very/long/url/that/cannot/be/broken/on/spaces/at/all/whatsoever for details",
		"indented":      "  - an indented bullet whose text is long enough to need wrapping\n  - a second one, also long enough to wrap at every width under test",
		"double space":  "sentence one.  sentence two.  sentence three, with enough words after it to wrap somewhere",
		"trailing runs": "a word followed by many spaces          and then more words to push past the width",
		"emoji":         "⏰ Wed 10:00 call · Fri 10:30 in person — emoji and middots included, plus enough text to wrap",
		"tabs":          "a\tb\tc and then a long enough tail that the line has to wrap at the narrow widths",
		"control chars": "a\x00b\x07c and then a long enough tail that the line has to wrap at the narrow widths",
		"blank lines":   "first paragraph that is long enough to wrap somewhere\n\n\nlast paragraph, also long enough to wrap somewhere",
	}

	for name, text := range inputs {
		for _, width := range []int{12, 23, 40, 71} {
			ta := newDescArea(text)
			ta.SetWidth(width)
			ta.SetHeight(200)

			var editor []string
			for _, l := range strings.Split(ansi.Strip(ta.View()), "\n") {
				editor = append(editor, strings.TrimRight(l, " "))
			}
			// The textarea pads its box to full height; drop the empty tail.
			for len(editor) > 1 && editor[len(editor)-1] == "" {
				editor = editor[:len(editor)-1]
			}

			read := strings.Split(wrapDesc(text, width), "\n")
			for i := range read {
				read[i] = strings.TrimRight(read[i], " ")
			}
			for len(read) > 1 && read[len(read)-1] == "" {
				read = read[:len(read)-1]
			}

			if len(read) != len(editor) {
				t.Errorf("%s at width %d: read mode wrapped to %d lines, editor to %d\nread:   %q\neditor: %q",
					name, width, len(read), len(editor), read, editor)
				continue
			}
			for i := range read {
				if read[i] != editor[i] {
					t.Errorf("%s at width %d, line %d:\n read   %q\n editor %q", name, width, i, read[i], editor[i])
				}
			}
		}
	}
}
