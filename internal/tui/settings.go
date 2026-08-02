package tui

import (
	"fmt"
	"sort"
	"strings"

	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"github.com/LeonY117/kanban-tui/internal/model"
)

// PROTOTYPE — for feel only. Nothing here persists to disk yet; the point is
// to settle the interaction before wiring it to config.json.

var conflictRed = lipgloss.Color("1")

// ─── The bindable surface ────────────────────────────────────────────

// bindAction is one row of the shortcuts list. One action, one key —
// MoveUp/MoveDown are separate actions rather than a single "J / K" row, which
// is what the keyMap already models.
//
// locked actions are the floor: without them there is no way to escape a mess
// you just made, so they render but can't be rebound.
type bindAction struct {
	id     string
	group  string
	label  string
	def    string
	locked bool
}

var bindActions = []bindAction{
	{id: "nav.left", group: "Navigation", label: "column on the left", def: "h", locked: true},
	{id: "nav.right", group: "Navigation", label: "column on the right", def: "l", locked: true},
	{id: "nav.up", group: "Navigation", label: "card above", def: "k", locked: true},
	{id: "nav.down", group: "Navigation", label: "card below", def: "j", locked: true},
	{id: "nav.enter", group: "Navigation", label: "open / confirm", def: "enter", locked: true},
	{id: "nav.esc", group: "Navigation", label: "back / cancel", def: "esc", locked: true},
	{id: "nav.quit", group: "Navigation", label: "quit", def: "q", locked: true},

	{id: "card.add", group: "Cards", label: "add a card", def: "a"},
	{id: "card.edit", group: "Cards", label: "edit", def: "e"},
	{id: "card.archive", group: "Cards", label: "archive", def: "x"},
	{id: "card.delete", group: "Cards", label: "delete", def: "d"},
	{id: "card.move", group: "Cards", label: "move to column / board", def: "m"},
	{id: "card.copy", group: "Cards", label: "copy id", def: "c"},
	{id: "card.status", group: "Cards", label: "set status", def: "s"},
	{id: "card.assign", group: "Cards", label: "assign", def: "A"},
	{id: "card.moveLeft", group: "Cards", label: "move a column left", def: "H"},
	{id: "card.moveRight", group: "Cards", label: "move a column right", def: "L"},
	{id: "card.reorderUp", group: "Cards", label: "reorder up", def: "K"},
	{id: "card.reorderDown", group: "Cards", label: "reorder down", def: "J"},

	{id: "board.picker", group: "Board", label: "board picker", def: "tab"},
	{id: "board.archiveView", group: "Board", label: "archive browser", def: "X"},
	{id: "board.unarchive", group: "Board", label: "unarchive", def: "u"},
	{id: "board.pin", group: "Board", label: "pin board", def: "p"},
	{id: "board.rename", group: "Board", label: "rename board", def: "r"},
	{id: "board.layout", group: "Board", label: "card size", def: "v"},
	{id: "board.rowLayout", group: "Board", label: "rows / columns", def: "V"},
	{id: "board.zoom", group: "Board", label: "zoom in", def: "+"},
	{id: "board.unzoom", group: "Board", label: "zoom out", def: "-"},
	{id: "board.panelNext", group: "Board", label: "next panel", def: "]"},
	{id: "board.panelPrev", group: "Board", label: "previous panel", def: "["},
	{id: "board.settings", group: "Board", label: "settings", def: "?"},
}

// ─── State ───────────────────────────────────────────────────────────

type settingsSection int

const (
	sectionShortcuts settingsSection = iota
	sectionColumns
	sectionAbout
)

var sectionNames = []string{"Shortcuts", "Columns", "About"}

// headerVariant selects one of the three header treatments under test. Toggled
// live with `\` so the options can be compared by feel rather than by mockup.
const headerVariantCount = 3

var headerVariantNames = []string{"tabs", "underline", "numbered"}

type settingsState struct {
	open    bool
	section settingsSection
	idx     int
	variant int

	// Working copies. Edits land here and only reach the real config on save,
	// so esc can always walk it back.
	binds  map[string]string
	labels map[model.Status]string

	// baseline is what binds looked like on open — used to undo just the
	// conflicting edits when someone escapes out of a conflicted state.
	baseline map[string]string

	capturing bool   // waiting for the next keypress to become a binding
	editing   bool   // typing a column label
	buf       string // the label being typed

	confirm string // pending destructive action, "" when none
	notice  string
	warned  bool // esc was pressed once while conflicted
}

func newSettingsState() settingsState {
	s := settingsState{
		binds:    map[string]string{},
		labels:   map[model.Status]string{},
		baseline: map[string]string{},
		variant:  0,
	}
	for _, a := range bindActions {
		s.binds[a.id] = a.def
	}
	for _, st := range model.ColumnOrder {
		s.labels[st] = statusDisplay[st]
	}
	return s
}

// conflicts maps a key to every action claiming it, for keys claimed more than
// once. Locked actions take part: rebinding onto `q` is a conflict, not a
// silent shadow.
func (s *settingsState) conflicts() map[string][]string {
	byKey := map[string][]string{}
	for _, a := range bindActions {
		k := s.binds[a.id]
		if k == "" {
			continue
		}
		byKey[k] = append(byKey[k], a.id)
	}
	out := map[string][]string{}
	for k, ids := range byKey {
		if len(ids) > 1 {
			sort.Strings(ids)
			out[k] = ids
		}
	}
	return out
}

func (s *settingsState) conflicted(id string) bool {
	c := s.conflicts()
	return len(c[s.binds[id]]) > 1
}

func (s *settingsState) conflictCount() int { return len(s.conflicts()) }

// revertConflicts puts every action caught in a collision back to what it was
// when the page opened, and leaves clean edits alone.
func (s *settingsState) revertConflicts() int {
	n := 0
	for _, ids := range s.conflicts() {
		for _, id := range ids {
			if s.binds[id] != s.baseline[id] {
				s.binds[id] = s.baseline[id]
				n++
			}
		}
	}
	return n
}

// ─── Entry / update ──────────────────────────────────────────────────

func (m *Model) enterSettings() (tea.Model, tea.Cmd) {
	if m.settings.binds == nil {
		m.settings = newSettingsState()
	}
	m.settings.baseline = map[string]string{}
	for k, v := range m.settings.binds {
		m.settings.baseline[k] = v
	}
	m.settings.idx = 0
	m.settings.notice = ""
	m.settings.warned = false
	m.settings.confirm = ""
	m.popupReturnView = m.view
	m.view = settingsView
	return m, nil
}

func (m *Model) closeSettings() {
	m.settings.capturing = false
	m.settings.editing = false
	m.settings.confirm = ""
	m.restorePopupView(settingsView)
}

func (m *Model) updateSettings(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	s := &m.settings
	k := msg.String()

	// Capture mode swallows everything: the whole point is that the next key
	// you press becomes the binding, so no other handler may see it first.
	if s.capturing {
		if k == "esc" {
			s.capturing = false
			s.notice = "cancelled"
			return m, nil
		}
		a := s.currentAction()
		if a != nil {
			s.binds[a.id] = k
			s.notice = fmt.Sprintf("%s → %s", a.label, k)
			s.warned = false
		}
		s.capturing = false
		return m, nil
	}

	if s.editing {
		switch k {
		case "esc":
			s.editing, s.buf = false, ""
			s.notice = "cancelled"
		case "enter":
			st := model.ColumnOrder[s.idx]
			if name := strings.TrimSpace(s.buf); name != "" {
				s.labels[st] = name
				s.notice = fmt.Sprintf("renamed to %s", name)
			} else {
				s.notice = "a column needs a name — left unchanged"
			}
			s.editing, s.buf = false, ""
		case "backspace":
			if r := []rune(s.buf); len(r) > 0 {
				s.buf = string(r[:len(r)-1])
			}
		default:
			if len(k) == 1 {
				s.buf += k
			}
		}
		return m, nil
	}

	if s.confirm != "" {
		switch k {
		case "y", "Y":
			switch s.confirm {
			case "resetAll":
				for _, a := range bindActions {
					s.binds[a.id] = a.def
				}
				s.notice = "all shortcuts back to defaults"
			case "resetLabels":
				for _, st := range model.ColumnOrder {
					s.labels[st] = statusDisplay[st]
				}
				s.notice = "all column names back to defaults"
			}
			s.confirm = ""
		default:
			s.confirm = ""
			s.notice = "cancelled"
		}
		return m, nil
	}

	switch {
	case k == "\\":
		s.variant = (s.variant + 1) % headerVariantCount
		s.notice = "header: " + headerVariantNames[s.variant]

	case k == "esc":
		if s.section == sectionShortcuts && s.conflictCount() > 0 {
			if !s.warned {
				s.warned = true
				s.notice = "esc again undoes the clashing changes"
				return m, nil
			}
			n := s.revertConflicts()
			s.notice = ""
			m.notice = fmt.Sprintf("undid %d conflicting change(s)", n)
		}
		m.closeSettings()

	case k == "tab", k == "]":
		s.section = (s.section + 1) % 3
		s.idx, s.notice = 0, ""
	case k == "[":
		s.section = (s.section + 2) % 3
		s.idx, s.notice = 0, ""
	case k == "1":
		s.section, s.idx = sectionShortcuts, 0
	case k == "2":
		s.section, s.idx = sectionColumns, 0
	case k == "3":
		s.section, s.idx = sectionAbout, 0

	case key.Matches(msg, keys.Up):
		if s.idx > 0 {
			s.idx--
		}
	case key.Matches(msg, keys.Down):
		if s.idx < s.rowCount()-1 {
			s.idx++
		}

	case key.Matches(msg, keys.Enter):
		switch s.section {
		case sectionShortcuts:
			a := s.currentAction()
			if a == nil {
				return m, nil
			}
			if a.locked {
				s.notice = a.label + " can't be rebound — it's how you get out"
				return m, nil
			}
			s.capturing = true
			s.notice = ""
		case sectionColumns:
			s.editing = true
			s.buf = s.labels[model.ColumnOrder[s.idx]]
		}

	case k == "d":
		switch s.section {
		case sectionShortcuts:
			if a := s.currentAction(); a != nil && !a.locked {
				s.binds[a.id] = a.def
				s.notice = fmt.Sprintf("%s back to %s", a.label, a.def)
			}
		case sectionColumns:
			st := model.ColumnOrder[s.idx]
			s.labels[st] = statusDisplay[st]
			s.notice = "back to " + statusDisplay[st]
		}

	case k == "D":
		switch s.section {
		case sectionShortcuts:
			s.confirm = "resetAll"
		case sectionColumns:
			s.confirm = "resetLabels"
		}
	}
	return m, nil
}

func (s *settingsState) rowCount() int {
	switch s.section {
	case sectionShortcuts:
		return len(bindActions)
	case sectionColumns:
		return len(model.ColumnOrder)
	}
	return 0
}

func (s *settingsState) currentAction() *bindAction {
	if s.section != sectionShortcuts || s.idx < 0 || s.idx >= len(bindActions) {
		return nil
	}
	return &bindActions[s.idx]
}

// ─── Render ──────────────────────────────────────────────────────────

const settingsWidth = 56

func (m *Model) viewSettings() string {
	s := &m.settings
	inner := settingsWidth - 4

	header := s.header(inner)
	footer := s.footer(inner)
	divider := " " + strings.Repeat("─", inner-2) + " "

	var body []string
	cursorRow := 0
	switch s.section {
	case sectionShortcuts:
		body, cursorRow = s.shortcutRows(inner)
	case sectionColumns:
		body, cursorRow = s.columnRows(inner)
	case sectionAbout:
		body = s.aboutRows(inner)
	}

	// The shortcuts list is longer than any sensible popup, so the body
	// scrolls while the header and footer stay put — the footer carries the
	// conflict warning, which must never be the thing that scrolls away.
	chrome := len(header) + len(footer) + 1
	height := len(body) + chrome + 2
	if max := m.height - 2; height > max {
		height = max
	}
	if height < 8 {
		height = 8
	}
	visible := height - 2 - chrome
	if visible < 1 {
		visible = 1
	}
	above, below := 0, 0
	if len(body) > visible {
		start := cursorRow - visible/2
		if start < 0 {
			start = 0
		}
		if start+visible > len(body) {
			start = len(body) - visible
		}
		above, below = start, len(body)-start-visible
		body = body[start : start+visible]
		dim := lipgloss.NewStyle().Foreground(dimGray)
		if above > 0 {
			body[0] = dim.Render(fmt.Sprintf("  ↑ %d more", above))
		}
		if below > 0 {
			body[len(body)-1] = dim.Render(fmt.Sprintf("  ↓ %d more", below))
		}
	}

	rows := append([]string{}, header...)
	rows = append(rows, body...)
	rows = append(rows, divider)
	rows = append(rows, footer...)

	width := settingsWidth
	if width > m.width-4 {
		width = m.width - 4
	}

	content := lipgloss.NewStyle().PaddingLeft(1).Render(strings.Join(rows, "\n"))
	popup := renderPanel("Settings", content, width, height, green, true)
	return m.centerOverPopup(popup, m.popupBackdrop(m.popupReturnView), width, height)
}

// header draws the section menu in whichever treatment is under test.
func (s *settingsState) header(w int) []string {
	active := lipgloss.NewStyle().Foreground(green).Bold(true)
	idle := lipgloss.NewStyle().Foreground(dimGray)

	switch s.variant {
	case 0: // tabs — the active section in brackets
		var parts []string
		for i, n := range sectionNames {
			if settingsSection(i) == s.section {
				parts = append(parts, active.Render("["+n+"]"))
			} else {
				parts = append(parts, idle.Render(" "+n+" "))
			}
		}
		return []string{" " + strings.Join(parts, "  "), ""}

	case 1: // underline — a rule under the active section only
		var parts []string
		var rule strings.Builder
		rule.WriteString(" ")
		for i, n := range sectionNames {
			if settingsSection(i) == s.section {
				parts = append(parts, active.Render(n))
				rule.WriteString(strings.Repeat("─", len(n)))
			} else {
				parts = append(parts, idle.Render(n))
				rule.WriteString(strings.Repeat(" ", len(n)))
			}
			rule.WriteString("   ")
		}
		return []string{" " + strings.Join(parts, "   "), rule.String()}

	default: // numbered — press the number to jump
		var parts []string
		for i, n := range sectionNames {
			tag := fmt.Sprintf("%d %s", i+1, n)
			if settingsSection(i) == s.section {
				parts = append(parts, active.Render(tag))
			} else {
				parts = append(parts, idle.Render(tag))
			}
		}
		return []string{" " + strings.Join(parts, idle.Render(" · ")), ""}
	}
}

func (s *settingsState) shortcutRows(w int) ([]string, int) {
	conf := s.conflicts()
	var rows []string
	cursorRow := 0
	group := ""
	for i, a := range bindActions {
		if a.group != group {
			group = a.group
			rows = append(rows, lipgloss.NewStyle().Foreground(dimGray).Render(" "+group))
		}
		marker := "  "
		if i == s.idx {
			marker = selectedMarker.Render("* ")
			cursorRow = len(rows)
		}
		keyText := s.binds[a.id]
		if s.capturing && i == s.idx {
			keyText = "press a key…"
		}

		labelStyle := lipgloss.NewStyle()
		keyStyle := lipgloss.NewStyle()
		switch {
		case len(conf[s.binds[a.id]]) > 1:
			labelStyle = labelStyle.Foreground(conflictRed)
			keyStyle = keyStyle.Foreground(conflictRed).Bold(true)
		case a.locked:
			labelStyle = labelStyle.Foreground(dimGray)
			keyStyle = keyStyle.Foreground(dimGray)
		default:
			keyStyle = keyStyle.Foreground(green)
		}
		rows = append(rows, padBetween(marker+labelStyle.Render("  "+a.label),
			keyStyle.Render(keyText), w))
	}
	return rows, cursorRow
}

func (s *settingsState) columnRows(w int) ([]string, int) {
	var rows []string
	cursorRow := 0
	rows = append(rows, lipgloss.NewStyle().Foreground(dimGray).
		Render(padBetween("  status", "name", w)))
	for i, st := range model.ColumnOrder {
		marker := "  "
		if i == s.idx {
			marker = selectedMarker.Render("* ")
			cursorRow = len(rows)
		}
		name := s.labels[st]
		style := lipgloss.NewStyle().Foreground(green)
		if s.editing && i == s.idx {
			name = "[" + s.buf + "▏]"
			style = lipgloss.NewStyle().Foreground(green).Bold(true)
		}
		rows = append(rows, padBetween(
			marker+lipgloss.NewStyle().Foreground(dimGray).Render(string(st)),
			style.Render(name), w))
	}
	rows = append(rows, "")
	rows = append(rows, lipgloss.NewStyle().Foreground(dimGray).
		Render("  the stored status never changes — board.json"))
	rows = append(rows, lipgloss.NewStyle().Foreground(dimGray).
		Render("  still says HOLD, and --status HOLD still works"))
	return rows, cursorRow
}

func (s *settingsState) aboutRows(w int) []string {
	dim := lipgloss.NewStyle().Foreground(dimGray)
	return []string{
		"  " + dim.Render("config") + "   ~/.kanban/config.json",
		"  " + dim.Render("board") + "    ~/.kanban/board.json",
		"",
		dim.Render("  Shortcuts and column names are local display"),
		dim.Render("  preferences. They never change what a board"),
		dim.Render("  stores, so the CLI and agents keep one"),
		dim.Render("  vocabulary whatever your screen says."),
		"",
		dim.Render("  PROTOTYPE — nothing here saves to disk yet."),
	}
}

func (s *settingsState) footer(w int) []string {
	dim := lipgloss.NewStyle().Foreground(dimGray)
	warn := lipgloss.NewStyle().Foreground(conflictRed).Bold(true)

	if s.confirm != "" {
		what := "every shortcut"
		if s.confirm == "resetLabels" {
			what = "every column name"
		}
		return []string{warn.Render("  reset " + what + " to defaults?  y / n")}
	}
	if s.capturing {
		return []string{dim.Render("  press any key to bind it · esc cancel")}
	}
	if s.editing {
		return []string{dim.Render("  type a name · enter save · esc cancel")}
	}

	var lines []string
	if n := s.conflictCount(); n > 0 && s.section == sectionShortcuts {
		lines = append(lines, warn.Render(fmt.Sprintf("  %d key used twice — resolve to leave", n)))
	}
	if s.notice != "" {
		lines = append(lines, dim.Render("  "+s.notice))
	}
	switch s.section {
	case sectionShortcuts:
		lines = append(lines, dim.Render("  enter rebind · d reset row · D reset all"))
	case sectionColumns:
		lines = append(lines, dim.Render("  enter rename · d reset row · D reset all"))
	default:
		lines = append(lines, dim.Render("  tab section · esc close"))
	}
	lines = append(lines, dim.Render("  \\ header style: "+headerVariantNames[s.variant]))
	for i, l := range lines {
		if lipgloss.Width(l) > w {
			lines[i] = ansi.Truncate(l, w, "…")
		}
	}
	return lines
}

// padBetween left-aligns a and right-aligns b inside w cells, measuring
// rendered width so ANSI styling doesn't throw the alignment off.
func padBetween(a, b string, w int) string {
	gap := w - lipgloss.Width(a) - lipgloss.Width(b)
	if gap < 1 {
		gap = 1
	}
	return a + strings.Repeat(" ", gap) + b
}
