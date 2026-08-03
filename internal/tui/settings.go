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
	"github.com/LeonY117/kanban-tui/internal/store"
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

type settingsState struct {
	section settingsSection
	idx     int

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
	dirty   bool // something changed and is worth writing on close
}

func newSettingsState() settingsState {
	s := settingsState{
		binds:    map[string]string{},
		labels:   map[model.Status]string{},
		baseline: map[string]string{},
	}
	for _, a := range bindActions {
		s.binds[a.id] = hk(a.id)
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
		if k := s.binds[a.id]; k != "" {
			byKey[k] = append(byKey[k], a.id)
		}
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

func (s *settingsState) conflictCount() int { return len(s.conflicts()) }

// revertConflicts puts every action caught in a collision back to what it was
// when the page opened, and leaves clean edits alone.
func (s *settingsState) revertConflicts() int {
	n := 0
	// One pass can trade one clash for another: with add a->z, edit e->a and
	// archive x->z, reverting the z clash puts add back on "a", which edit now
	// holds. Keep going until the working copy is clean. The baseline itself is
	// conflict-free, so full reversion is the floor and this terminates; the
	// bound is belt and braces.
	for i := 0; i <= len(bindActions); i++ {
		if s.conflictCount() == 0 {
			return n
		}
		for _, ids := range s.conflicts() {
			for _, id := range ids {
				if s.binds[id] != s.baseline[id] {
					s.binds[id] = s.baseline[id]
					n++
				}
			}
		}
	}
	// Belt and braces: if anything still clashes, nothing edited survives.
	for id, base := range s.baseline {
		if s.binds[id] != base {
			s.binds[id] = base
			n++
		}
	}
	return n
}

// labelTaken reports whether another column already reads this way.
func (s *settingsState) labelTaken(self model.Status, name string) bool {
	want := strings.ToLower(strings.TrimSpace(name))
	for _, st := range model.ColumnOrder {
		if st == self {
			continue
		}
		if strings.ToLower(strings.TrimSpace(s.labels[st])) == want {
			return true
		}
	}
	return false
}

// duplicateLabels reports column labels used by more than one status. Two
// columns reading the same word is not merely ugly: the meta-bar status picker
// keys its choices by label, so the duplicate shadows one status and picking it
// writes the other one to the ticket.
func (s *settingsState) duplicateLabels() map[string]int {
	byLabel := map[string]int{}
	for _, st := range model.ColumnOrder {
		byLabel[strings.ToLower(strings.TrimSpace(s.labels[st]))]++
	}
	out := map[string]int{}
	for label, n := range byLabel {
		if n > 1 {
			out[label] = n
		}
	}
	return out
}

// atDefault reports whether the focused row still holds its built-in value, so
// the footer can say "already default" rather than offering a reset that would
// do nothing.
func (s *settingsState) atDefault() bool {
	switch s.section {
	case sectionShortcuts:
		a := s.currentAction()
		return a != nil && s.binds[a.id] == a.def
	case sectionColumns:
		if s.idx < 0 || s.idx >= len(model.ColumnOrder) {
			return false
		}
		st := model.ColumnOrder[s.idx]
		return s.labels[st] == defaultStatusDisplay[st]
	}
	return false
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
	m.settings.dirty = false
	m.popupReturnView = m.view
	m.view = settingsView
	return m, nil
}

func (m *Model) closeSettings() {
	m.settings.capturing = false
	m.settings.editing = false
	m.settings.confirm = ""
	if m.settings.dirty {
		m.saveSettings()
	}
	m.restorePopupView(settingsView)
}

// saveSettings writes the working copy to config.json and makes it live.
//
// Only what differs from the built-in default is stored, so a later change to
// a default still reaches anyone who never overrode it.
func (m *Model) saveSettings() {
	s := &m.settings
	// Defensive: closeSettings already refuses to leave a conflict behind, and
	// a conflicted config would be silently refused on load — so a "saved"
	// notice over one would be a lie.
	if s.conflictCount() > 0 {
		m.notice = "not saved — resolve the key conflicts first"
		return
	}

	// Start from what is on disk so fields this page doesn't expose survive.
	// The short labels in the count strip are hand-edited only; rebuilding the
	// config from the working copy alone silently dropped them.
	cfg := store.LoadConfig()
	shorts := map[string]string{}
	for _, cc := range cfg.Columns {
		if st, err := model.ParseStatus(cc.Status); err == nil && cc.Short != "" {
			shorts[string(st)] = cc.Short
		}
	}

	cfg.Columns = nil
	for _, st := range model.ColumnOrder {
		label := ""
		if s.labels[st] != defaultStatusDisplay[st] {
			label = s.labels[st]
		}
		short := shorts[string(st)]
		if label == "" && short == "" {
			continue
		}
		cfg.Columns = append(cfg.Columns,
			store.ColumnConfig{Status: string(st), Label: label, Short: short})
	}

	cfg.Keys = nil
	for _, a := range bindActions {
		if a.locked || s.binds[a.id] == a.def {
			continue
		}
		if cfg.Keys == nil {
			cfg.Keys = map[string]string{}
		}
		cfg.Keys[a.id] = s.binds[a.id]
	}

	if err := store.SaveConfig(cfg); err != nil {
		// m.err is set all over this file but rendered nowhere, so a failed
		// save would otherwise report itself as a success.
		m.notice = "could not save settings: " + err.Error()
		return
	}
	if refused := ApplyConfig(cfg); len(refused) > 0 {
		m.notice = fmt.Sprintf("settings saved — %d binding(s) refused on load", len(refused))
	} else {
		m.notice = "settings saved"
	}
	s.dirty = false
}

// statusChoices lists the statuses the meta-bar picker offers, labelled as they
// appear on the board, with a way back from the label the user picked. Backlog
// is absent, matching the fixed list this replaced.
//
// The map back matters: the picker used to round-trip its label through
// model.ParseStatus, which only works while every label is also a status name.
// Rename Done to Shipped and picking it would silently do nothing.
func statusChoices() ([]string, map[string]model.Status) {
	labels := make([]string, 0, len(model.ColumnOrder))
	byLabel := make(map[string]model.Status, len(model.ColumnOrder))
	for _, status := range model.ColumnOrder {
		if status == model.StatusBacklog {
			continue
		}
		label := statusDisplay[status]
		// A label two columns share would otherwise overwrite the earlier
		// status here, so picking the first "Done" could store DONE when the
		// user meant TODO. Disambiguate rather than lose one.
		if _, clash := byLabel[label]; clash {
			label = fmt.Sprintf("%s (%s)", label, status)
		}
		labels = append(labels, label)
		byLabel[label] = status
	}
	return labels, byLabel
}

func (m *Model) setSettingsSection(sec settingsSection) {
	m.settings.section = (sec + 3) % 3
	m.settings.idx = 0
	m.settings.notice = ""
}

// moveSettingsCursor is the shared entry point for j/k and the wheel.
func (m *Model) moveSettingsCursor(dir int) {
	s := &m.settings
	if s.capturing || s.editing || s.confirm != "" {
		return
	}
	n := s.rowCount()
	if n == 0 {
		return
	}
	s.idx += dir
	if s.idx < 0 {
		s.idx = 0
	}
	if s.idx > n-1 {
		s.idx = n - 1
	}
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
		if a := s.currentAction(); a != nil {
			// Reserved keys are refused here rather than shown as a conflict:
			// the clash is with a binding this list doesn't show (an arrow that
			// is also `h`, ctrl+c, a column-jump digit), so a red row would
			// point at nothing the user could resolve.
			if reservedKeys()[k] {
				s.notice = k + " is reserved — it's how you get around"
			} else {
				s.binds[a.id] = k
				s.dirty = true
				s.notice = fmt.Sprintf("%s → %s", a.label, k)
				s.warned = false
			}
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
			name := strings.TrimSpace(s.buf)
			switch {
			case name == "":
				s.notice = "a column needs a name — left unchanged"
			case s.labelTaken(st, name):
				s.notice = name + " is already another column — left unchanged"
			default:
				s.labels[st] = name
				s.dirty = true
				s.notice = "renamed to " + name
			}
			s.editing, s.buf = false, ""
		case "backspace":
			if r := []rune(s.buf); len(r) > 0 {
				s.buf = string(r[:len(r)-1])
			}
		default:
			// Rune-aware, so "é" and CJK type as readily as ASCII. Anything
			// bubbletea reports as a named chord ("ctrl+a", "shift+tab") is
			// more than one rune and is ignored.
			if len([]rune(k)) == 1 {
				s.buf += k
			}
		}
		return m, nil
	}

	if s.confirm != "" {
		if k == "y" || k == "Y" {
			switch s.confirm {
			case "resetAll":
				for _, a := range bindActions {
					s.binds[a.id] = a.def
				}
				s.dirty = true
				s.notice = "all shortcuts back to defaults"
			case "resetLabels":
				for _, st := range model.ColumnOrder {
					s.labels[st] = defaultStatusDisplay[st]
				}
				s.dirty = true
				s.notice = "all column names back to defaults"
			}
		} else {
			s.notice = "cancelled"
		}
		s.confirm = ""
		return m, nil
	}

	switch {
	case k == "esc":
		// Checked from every section: a conflict made in Shortcuts and then
		// escaped from Columns used to close and write itself to disk.
		if s.conflictCount() > 0 {
			if !s.warned {
				s.warned = true
				return m, nil
			}
			n := s.revertConflicts()
			m.notice = fmt.Sprintf("undid %d conflicting change(s)", n)
		}
		m.closeSettings()

	// Sections move on h/l as well as tab — the same left/right idiom the board
	// uses for columns, so the top menu reads as a row of columns too.
	case k == "tab", key.Matches(msg, keys.Right):
		m.setSettingsSection(s.section + 1)
	case k == "shift+tab", key.Matches(msg, keys.Left):
		m.setSettingsSection(s.section - 1)
	case k == "1":
		m.setSettingsSection(sectionShortcuts)
	case k == "2":
		m.setSettingsSection(sectionColumns)
	case k == "3":
		m.setSettingsSection(sectionAbout)

	case key.Matches(msg, keys.Up):
		m.moveSettingsCursor(-1)
	case key.Matches(msg, keys.Down):
		m.moveSettingsCursor(1)

	case key.Matches(msg, keys.Enter):
		return m.settingsActivate()

	case k == "r":
		switch s.section {
		case sectionShortcuts:
			a := s.currentAction()
			if a == nil || a.locked {
				return m, nil
			}
			if s.binds[a.id] == a.def {
				s.notice = "already default"
				return m, nil
			}
			s.binds[a.id] = a.def
			s.dirty = true
			s.notice = a.label + " back to " + a.def
		case sectionColumns:
			st := model.ColumnOrder[s.idx]
			if s.labels[st] == defaultStatusDisplay[st] {
				s.notice = "already default"
				return m, nil
			}
			s.labels[st] = defaultStatusDisplay[st]
			s.dirty = true
			s.notice = "back to " + defaultStatusDisplay[st]
		}

	case k == "R":
		switch s.section {
		case sectionShortcuts:
			s.confirm = "resetAll"
		case sectionColumns:
			s.confirm = "resetLabels"
		}
	}
	return m, nil
}

// settingsActivate is what enter does, and where a click lands.
func (m *Model) settingsActivate() (tea.Model, tea.Cmd) {
	s := &m.settings
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
	return m, nil
}

// ─── Render ──────────────────────────────────────────────────────────

const settingsWidth = 56

// settingsHeight keeps the popup a constant share of the viewport, so
// switching sections doesn't make it grow and shrink under the cursor.
func (m *Model) settingsHeight() int {
	h := m.height * 4 / 5
	if h > m.height-2 {
		h = m.height - 2
	}
	if h < 10 {
		h = 10
	}
	return h
}

func (m *Model) viewSettings() string {
	s := &m.settings
	width := settingsWidth
	if width > m.width-4 {
		width = m.width - 4
	}
	inner := width - 4
	height := m.settingsHeight()
	origin := m.popupOrigin(width, height)

	backdrop := m.popupBackdrop(m.popupReturnView)
	m.resetZones() // drop the backdrop's zones; the popup covers them

	header, tabSpans := s.header()
	footer := s.footer(inner)
	divider := " " + strings.Repeat("─", inner-2) + " "

	var body []string
	var rowIdx []int
	cursorRow := 0
	switch s.section {
	case sectionShortcuts:
		body, rowIdx, cursorRow = s.shortcutRows(inner)
	case sectionColumns:
		body, rowIdx, cursorRow = s.columnRows(inner)
	case sectionAbout:
		body = s.aboutRows(inner)
		rowIdx = make([]int, len(body))
		for i := range rowIdx {
			rowIdx[i] = -1
		}
	}

	// Fixed height: the body window is whatever the chrome leaves, and a short
	// section pads rather than shrinking the popup under the cursor.
	visible := height - 2 - len(header) - len(footer) - 1
	if visible < 1 {
		visible = 1
	}
	start := 0
	if len(body) > visible {
		start = cursorRow - visible/2
		if start < 0 {
			start = 0
		}
		if start+visible > len(body) {
			start = len(body) - visible
		}
	}
	end := start + visible
	if end > len(body) {
		end = len(body)
	}
	winRows := append([]string{}, body[start:end]...)
	winIdx := append([]int{}, rowIdx[start:end]...)
	for len(winRows) < visible {
		winRows = append(winRows, "")
		winIdx = append(winIdx, -1)
	}
	dim := lipgloss.NewStyle().Foreground(dimGray)
	if start > 0 {
		winRows[0] = dim.Render(fmt.Sprintf(" ↑ %d more", start))
		winIdx[0] = -1
	}
	if end < len(body) {
		winRows[len(winRows)-1] = dim.Render(fmt.Sprintf(" ↓ %d more", len(body)-end))
		winIdx[len(winRows)-1] = -1
	}

	// Zones. Content sits one cell in from the border plus the PaddingLeft(1).
	contentX := origin.x + 2
	if !s.capturing && !s.editing && s.confirm == "" {
		for i, span := range tabSpans {
			m.addZone(hitZone{kind: zoneSettingsTab, x: contentX + span.x,
				y: origin.y + 1, w: span.w, h: 1, idx: i})
		}
		for i, idx := range winIdx {
			if idx < 0 {
				continue
			}
			m.addZone(hitZone{kind: zoneSettingsRow, x: contentX,
				y: origin.y + 1 + len(header) + i, w: inner, h: 1, idx: idx})
		}
	}

	rows := append([]string{}, header...)
	rows = append(rows, winRows...)
	rows = append(rows, divider)
	rows = append(rows, footer...)

	content := lipgloss.NewStyle().PaddingLeft(1).Render(strings.Join(rows, "\n"))
	popup := renderPanel("Settings", content, width, height, green, true)
	return overlayAt(backdrop, popup, origin.x, origin.y)
}

type tabSpan struct{ x, w int }

// header draws the section tabs and reports where each one landed, so the same
// layout can be registered as click targets without measuring styled text.
func (s *settingsState) header() ([]string, []tabSpan) {
	active := lipgloss.NewStyle().Foreground(green).Bold(true)
	idle := lipgloss.NewStyle().Foreground(dimGray)

	var b strings.Builder
	var spans []tabSpan
	b.WriteString(" ")
	x := 1
	for i, n := range sectionNames {
		text := " " + n + " "
		if settingsSection(i) == s.section {
			text = "[" + n + "]"
			b.WriteString(active.Render(text))
		} else {
			b.WriteString(idle.Render(text))
		}
		spans = append(spans, tabSpan{x: x, w: len(text)})
		x += len(text) + 1
		b.WriteString(" ")
	}
	return []string{b.String(), ""}, spans
}

// groupHeader separates a group from its rows with weight and a rule rather
// than with indentation, so every row keeps the same left edge.
func groupHeader(name string, w int) string {
	rule := w - len(name) - 3
	if rule < 1 {
		rule = 1
	}
	return " " + lipgloss.NewStyle().Bold(true).Render(name) + " " +
		lipgloss.NewStyle().Foreground(dimGray).Render(strings.Repeat("─", rule))
}

func (s *settingsState) shortcutRows(w int) ([]string, []int, int) {
	conf := s.conflicts()
	var rows []string
	var idxs []int
	cursorRow := 0
	group := ""
	for i, a := range bindActions {
		if a.group != group {
			if group != "" {
				rows, idxs = append(rows, ""), append(idxs, -1)
			}
			group = a.group
			rows, idxs = append(rows, groupHeader(group, w)), append(idxs, -1)
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

		labelStyle, keyStyle := lipgloss.NewStyle(), lipgloss.NewStyle()
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
		rows = append(rows, padBetween(" "+marker+labelStyle.Render(a.label),
			keyStyle.Render(keyText)+" ", w))
		idxs = append(idxs, i)
	}
	return rows, idxs, cursorRow
}

func (s *settingsState) columnRows(w int) ([]string, []int, int) {
	var rows []string
	var idxs []int
	cursorRow := 0
	rows, idxs = append(rows, groupHeader("Columns", w)), append(idxs, -1)
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
			style = style.Bold(true)
		}
		rows = append(rows, padBetween(
			" "+marker+lipgloss.NewStyle().Foreground(dimGray).Render(string(st)),
			style.Render(name)+" ", w))
		idxs = append(idxs, i)
	}
	return rows, idxs, cursorRow
}

func (s *settingsState) aboutRows(w int) []string {
	dim := lipgloss.NewStyle().Foreground(dimGray)
	return []string{
		groupHeader("Files", w),
		padBetween(" "+dim.Render("  config"), "~/.kanban/config.json ", w),
		padBetween(" "+dim.Render("  board"), "~/.kanban/board.json ", w),
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
		return []string{warn.Render(" reset " + what + " to defaults?  y / n"), ""}
	}
	if s.capturing {
		return []string{dim.Render(" press any key to bind it · esc cancel"), ""}
	}
	if s.editing {
		return []string{dim.Render(" type a name · enter save · esc cancel"), ""}
	}

	var lines []string
	switch {
	case s.conflictCount() > 0 && s.section == sectionShortcuts:
		n := s.conflictCount()
		noun := "key is"
		if n > 1 {
			noun = "keys are"
		}
		// Once esc has been refused, the line has to say what esc will do
		// next — otherwise the only way out is a message the user can't see.
		tail := "resolve to leave"
		if s.warned {
			tail = "esc again undoes them"
		}
		lines = append(lines, warn.Render(fmt.Sprintf(" %d %s used twice — %s", n, noun, tail)))
	case s.notice != "":
		lines = append(lines, dim.Render(" "+s.notice))
	case s.atDefault():
		lines = append(lines, dim.Render(" already default"))
	default:
		lines = append(lines, "")
	}
	switch s.section {
	case sectionShortcuts:
		lines = append(lines, dim.Render(" enter rebind · r reset · R reset all · esc close"))
	case sectionColumns:
		lines = append(lines, dim.Render(" enter rename · r reset · R reset all · esc close"))
	default:
		lines = append(lines, dim.Render(" h/l section · esc close"))
	}
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
