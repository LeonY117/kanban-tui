package tui

// Prototype: a Slack-style `:shortcode` typeahead, the other half of how people
// type emoji. alt+e is for browsing — you don't know what you want; `:` is for
// when you do, and never leaves the keyboard or the text you're writing.
//
// It is keystroke-driven rather than derived from the field's text: `:` arms
// it, word characters extend the query, backspace shortens it, and anything
// else — a space, an arrow, a click — drops it. That means no cursor
// arithmetic to keep in sync with six different widgets, and it fails safe:
// the state can only ever be wrong in the direction of not showing.
//
// Nothing is intercepted until the popup is actually on screen (two characters
// in, at least one match), so `enter` still submits a ticket titled ":wip" and
// a lone `:` in prose costs nothing.

import (
	"sort"
	"strings"

	"github.com/charmbracelet/bubbles/textarea"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/LeonY117/kanban-tui/internal/emoji"
)

const (
	typeaheadMinChars = 2 // as in Slack: `:a` is still just text
	typeaheadRows     = 6
)

type emojiTypeahead struct {
	active  bool // `:` has been typed and the query is still being built
	target  emojiTarget
	query   string
	sel     int
	matches []emoji.Entry
}

// showing reports whether the popup is on screen — and therefore whether the
// typeahead may take keys off the field underneath it.
func (m *Model) typeaheadShowing() bool {
	t := m.emojiType
	return t.active && len(t.query) >= typeaheadMinChars && len(t.matches) > 0
}

// shortcode is the `:memo:` form of an entry's name.
func shortcode(name string) string {
	return strings.ReplaceAll(name, " ", "_")
}

func isShortcodeRune(r rune) bool {
	return r >= 'a' && r <= 'z' || r >= '0' && r <= '9' || r == '_' || r == '+' || r == '-'
}

// focusedTextTarget names the widget the user is typing into right now, or
// reports false when the keystroke isn't going into text at all.
func (m *Model) focusedTextTarget() (emojiTarget, bool) {
	switch m.view {
	case addView:
		if m.addDescEditing {
			return emojiToAddDesc, true
		}
		switch m.addFocusIdx {
		case addFocusTitle:
			return emojiToAddTitle, true
		case addFocusTags:
			return emojiToAddTags, true
		case addFocusAssign:
			return emojiToAddAssign, true
		}
	case splitView, detailView:
		if m.editTitle.Focused() {
			return emojiToEditTitle, true
		}
		if m.editDesc.Focused() {
			return emojiToEditDesc, true
		}
	case infoView:
		if m.infoEditing {
			return emojiToInfoDesc, true
		}
	}
	return 0, false
}

// trackTypeahead mirrors the keystroke that just went into the field. It runs
// after the widget has seen the key, so the text and the query stay in step.
func (m *Model) trackTypeahead(msg tea.KeyMsg) {
	target, ok := m.focusedTextTarget()
	if !ok || (m.emojiType.active && m.emojiType.target != target) {
		m.emojiType = emojiTypeahead{}
		if !ok {
			return
		}
	}

	switch msg.Type {
	case tea.KeyRunes:
		if msg.Alt || len(msg.Runes) != 1 {
			m.emojiType = emojiTypeahead{}
			return
		}
		r := msg.Runes[0]
		switch {
		case r == ':':
			m.emojiType = emojiTypeahead{active: true, target: target}
		case m.emojiType.active && isShortcodeRune(r):
			m.setTypeaheadQuery(m.emojiType.query + string(r))
		default:
			m.emojiType = emojiTypeahead{}
		}
	case tea.KeyBackspace, tea.KeyCtrlH:
		if !m.emojiType.active {
			return
		}
		// Backspacing over the `:` itself ends the shortcode, exactly as it
		// ends the board's search.
		if m.emojiType.query == "" {
			m.emojiType = emojiTypeahead{}
			return
		}
		m.setTypeaheadQuery(m.emojiType.query[:len(m.emojiType.query)-1])
	default:
		m.emojiType = emojiTypeahead{}
	}
}

func (m *Model) setTypeaheadQuery(q string) {
	m.emojiType.query = q
	m.emojiType.sel = 0
	m.emojiType.matches = typeaheadMatches(q)
}

// typeaheadMatches ranks the safe set against a partial shortcode: exact, then
// prefix, then substring.
//
// Shortcodes only — deliberately not the picker's keyword search. A keyword
// bucket makes `:wip` pop a list (something has "wipe" among its keywords) and
// turns the enter that should have submitted the ticket into a toilet roll.
// The two surfaces divide honestly: `:` is for when you know the name, alt+e
// is for when you don't.
func typeaheadMatches(query string) []emoji.Entry {
	if query == "" {
		return nil
	}
	var exact, prefix, inName []emoji.Entry
	for _, e := range emoji.Safe {
		code := shortcode(e.Name)
		switch {
		case code == query:
			exact = append(exact, e)
		case strings.HasPrefix(code, query):
			prefix = append(prefix, e)
		case strings.Contains(code, query):
			inName = append(inName, e)
		}
	}
	out := make([]emoji.Entry, 0, typeaheadRows)
	for _, bucket := range [][]emoji.Entry{exact, prefix, inName} {
		// Shortest first inside a bucket: the less shortcode there is around
		// the query, the closer the match. `:fir` means 🔥, not the first
		// quarter moon, which codepoint order would otherwise put first.
		sort.SliceStable(bucket, func(i, j int) bool {
			return len(bucket[i].Name) < len(bucket[j].Name)
		})
		for _, e := range bucket {
			if len(out) == typeaheadRows {
				return out
			}
			out = append(out, e)
		}
	}
	return out
}

// typeaheadKey handles the keys the popup owns while it is on screen, and
// reports whether it consumed one. Everything else falls through to the field.
func (m *Model) typeaheadKey(msg tea.KeyMsg) (bool, tea.Cmd) {
	if !m.typeaheadShowing() {
		return false, nil
	}
	switch msg.String() {
	case "up", "ctrl+p":
		if m.emojiType.sel > 0 {
			m.emojiType.sel--
		}
		return true, nil
	case "down", "ctrl+n":
		if m.emojiType.sel < len(m.emojiType.matches)-1 {
			m.emojiType.sel++
		}
		return true, nil
	case "enter", "tab":
		m.applyTypeahead()
		return true, nil
	case "esc":
		// Dismiss without touching what was typed — `:wip` stays `:wip`.
		m.emojiType = emojiTypeahead{}
		return true, nil
	}
	return false, nil
}

// applyTypeahead swaps the `:query` the user typed for the selected emoji.
func (m *Model) applyTypeahead() {
	if m.emojiType.sel >= len(m.emojiType.matches) {
		return
	}
	e := m.emojiType.matches[m.emojiType.sel].Emoji
	n := len([]rune(m.emojiType.query)) + 1 // the query, plus its leading ':'
	target := m.emojiType.target
	m.emojiType = emojiTypeahead{}

	if ta := m.textAreaFor(target); ta != nil {
		// A textarea has no public splice, so the deletion is spelled the way
		// the user would spell it. That also keeps its undo and wrapping state
		// consistent, which poking at the value directly would not.
		for i := 0; i < n; i++ {
			*ta, _ = ta.Update(tea.KeyMsg{Type: tea.KeyBackspace})
		}
		ta.InsertString(e)
		return
	}
	if in := m.textInputFor(target); in != nil {
		replaceBeforeCursor(in, n, e)
	}
}

func (m *Model) textAreaFor(t emojiTarget) *textarea.Model {
	switch t {
	case emojiToAddDesc:
		return &m.addDesc
	case emojiToEditDesc:
		return &m.editDesc
	case emojiToInfoDesc:
		return &m.infoDesc
	}
	return nil
}

func (m *Model) textInputFor(t emojiTarget) *textinput.Model {
	switch t {
	case emojiToAddTitle:
		return &m.addTitle
	case emojiToAddTags:
		return &m.addTags
	case emojiToAddAssign:
		return &m.addAssign
	case emojiToEditTitle:
		return &m.editTitle
	}
	return nil
}

// replaceBeforeCursor swaps the n runes before the cursor for s.
func replaceBeforeCursor(in *textinput.Model, n int, s string) {
	runes := []rune(in.Value())
	pos := in.Position()
	if pos > len(runes) {
		pos = len(runes)
	}
	if n > pos {
		n = pos
	}
	if !setWithinLimit(in, string(runes[:pos-n])+s+string(runes[pos:])) {
		return
	}
	in.SetCursor(pos - n + len([]rune(s)))
}

// ─── Render ──────────────────────────────────────────────────────────

// overlayTypeahead floats the suggestion list over the frame, anchored to the
// field being typed into so it reads as belonging to that text.
func (m *Model) overlayTypeahead(frame string) string {
	if !m.typeaheadShowing() {
		return frame
	}
	list, w, h := m.renderTypeaheadList()
	origin := m.typeaheadOrigin(w, h)
	return overlayAt(frame, list, origin.x, origin.y)
}

// typeaheadOrigin hangs the list off the focused field, covering what comes
// after it. Slack puts its list above the composer because the composer is at
// the bottom of the window; the equivalent here — next to the text, over the
// content it interrupts — is below, since these fields sit near the top of
// their popup. Above is the fallback when the field is too low for that.
func (m *Model) typeaheadOrigin(w, h int) point {
	anchor := m.typeaheadAnchor()
	if anchor == nil {
		// No zone to hang it off (the board-description popup registers none):
		// centre it, which is where every other floating panel lands.
		return m.popupOrigin(w, h)
	}
	x := min(anchor.x, max(0, m.width-w))
	if below := anchor.y + anchor.h; below+h <= m.height {
		return point{x: x, y: below}
	}
	return point{x: x, y: max(0, anchor.y-h)}
}

// typeaheadAnchor is the on-screen rectangle of the field being typed into,
// from the zones the last render registered.
func (m *Model) typeaheadAnchor() *hitZone {
	kind, idx := zoneAddField, m.addFocusIdx
	if m.view == splitView || m.view == detailView {
		kind = zoneField
		idx = 1 // the title panel
		if m.editDesc.Focused() {
			idx = 2
		}
	} else if m.view != addView {
		return nil
	}
	for i := len(m.zones) - 1; i >= 0; i-- {
		z := &m.zones[i]
		if z.kind != kind || z.idx != idx {
			continue
		}
		// A description panel is thirty rows tall, so hanging the list off the
		// panel puts it nowhere near the line being typed. Narrow the anchor
		// to the cursor's own row; a one-line input needs no such care.
		if ta := m.textAreaFor(m.emojiType.target); ta != nil {
			row := min(z.y+1+ta.Line(), z.y+z.h-1)
			return &hitZone{x: z.x, y: row, w: z.w, h: 1}
		}
		return z
	}
	return nil
}

// renderTypeaheadList draws the suggestions, each as the emoji and its
// `:shortcode:` with the typed part picked out — the match is the reason the
// row is in the list, so it is what the eye should land on.
func (m *Model) renderTypeaheadList() (string, int, int) {
	matched := lipgloss.NewStyle().Bold(true)
	dim := lipgloss.NewStyle().Foreground(midGray)
	selected := lipgloss.NewStyle().Reverse(true)

	widest := 0
	rows := make([]string, 0, len(m.emojiType.matches))
	for i, e := range m.emojiType.matches {
		code := shortcode(e.Name)
		label := ":" + code + ":"
		// Pick out the typed span wherever it landed in the shortcode.
		if at := strings.Index(code, m.emojiType.query); at >= 0 {
			end := at + len(m.emojiType.query)
			label = dim.Render(":"+code[:at]) + matched.Render(code[at:end]) + dim.Render(code[end:]+":")
		} else {
			label = dim.Render(label)
		}
		// The selection marks the emoji itself, not the row: a full-width
		// reverse bar swallows the shortcode and the colons that make the row
		// readable, and the glyph is what you are actually choosing.
		glyph := e.Emoji
		if i == m.emojiType.sel {
			glyph = selected.Render(glyph)
		}
		row := " " + glyph + " " + label
		if w := lipgloss.Width(row); w > widest {
			widest = w
		}
		rows = append(rows, row)
	}

	// widest + two borders + a right gutter, so the longest shortcode doesn't
	// sit flush against the frame.
	width := min(widest+3, max(20, m.width-2))
	height := len(rows) + 2
	body := strings.Join(rows, "\n")
	return renderPanel("", body, width, height, midGray, false), width, height
}
