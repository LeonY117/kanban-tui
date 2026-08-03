package tui

import (
	"fmt"
	"os"
	"sort"
	"strings"

	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"github.com/LeonY117/kanban-tui/internal/model"
	"github.com/LeonY117/kanban-tui/internal/store"
)

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
	{id: "board.search", group: "Board", label: "search", def: "/"},
	{id: "board.tags", group: "Board", label: "tag filter (in board picker)", def: "t"},
}

var bindActionsByID = func() map[string]bindAction {
	byID := make(map[string]bindAction, len(bindActions))
	for _, action := range bindActions {
		byID[action.id] = action
	}
	return byID
}()

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

	// Baselines identify the net rows changed during this visit. Saving merges
	// only those rows into the latest config, so another process's unrelated
	// edits and settings this build doesn't know about survive.
	baseline       map[string]string
	baselineLabels map[model.Status]string
	changedBinds   map[string]bool
	changedLabels  map[model.Status]bool

	capturing bool   // waiting for the next keypress to become a binding
	editing   bool   // typing a column label
	buf       string // the label being typed

	confirm string // pending destructive action, "" when none
	notice  string
	warned  bool // esc was pressed once while conflicted
	dirty   bool // something changed and is worth writing on close
	undone  int  // clashing edits rolled back on the way out, reported after saving
}

func newSettingsState() settingsState {
	s := settingsState{
		binds:          map[string]string{},
		labels:         map[model.Status]string{},
		baseline:       map[string]string{},
		baselineLabels: map[model.Status]string{},
		changedBinds:   map[string]bool{},
		changedLabels:  map[model.Status]bool{},
	}
	s.refreshLiveValues()
	return s
}

func (s *settingsState) refreshLiveValues() {
	for _, a := range bindActions {
		s.binds[a.id] = hk(a.id)
	}
	for _, st := range model.ColumnOrder {
		s.labels[st] = statusDisplay[st]
	}
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
					s.markBindingChanged(id)
					n++
				}
			}
		}
	}
	// Belt and braces: if anything still clashes, nothing edited survives.
	for id, base := range s.baseline {
		if s.binds[id] != base {
			s.binds[id] = base
			s.markBindingChanged(id)
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

func (s *settingsState) markBindingChanged(id string) {
	if s.binds[id] == s.baseline[id] {
		delete(s.changedBinds, id)
	} else {
		s.changedBinds[id] = true
	}
	s.updateDirty()
}

func (s *settingsState) markLabelChanged(status model.Status) {
	if s.labels[status] == s.baselineLabels[status] {
		delete(s.changedLabels, status)
	} else {
		s.changedLabels[status] = true
	}
	s.updateDirty()
}

func (s *settingsState) updateDirty() {
	s.dirty = len(s.changedBinds) > 0 || len(s.changedLabels) > 0
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

// labelStatus is the column the cursor is on, for tests and callers that need
// it without reaching into ColumnOrder.
func (s *settingsState) labelStatus() model.Status {
	if s.idx < 0 || s.idx >= len(model.ColumnOrder) {
		return ""
	}
	return model.ColumnOrder[s.idx]
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
	m.settings.baselineLabels = map[model.Status]string{}
	for k, v := range m.settings.labels {
		m.settings.baselineLabels[k] = v
	}
	m.settings.changedBinds = map[string]bool{}
	m.settings.changedLabels = map[model.Status]bool{}
	m.settings.idx = 0
	m.settings.notice = ""
	m.settings.warned = false
	m.settings.undone = 0
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
		if !m.saveSettings() {
			return
		}
	}
	m.restorePopupView(settingsView)
}

// saveSettings writes the working copy to config.json and makes it live.
//
// Only what differs from the built-in default is stored, so a later change to
// a default still reaches anyone who never overrode it.
func (m *Model) saveSettings() bool {
	s := &m.settings
	// Defensive: closeSettings already refuses to leave a conflict behind, and
	// a conflicted config would be silently refused on load — so a "saved"
	// notice over one would be a lie.
	if s.conflictCount() > 0 {
		m.notice = "not saved — resolve the key conflicts first"
		s.notice = m.notice
		return false
	}

	var saved store.Config
	err := store.UpdateConfig(func(cfg *store.Config) error {
		if err := s.mergeIntoConfig(cfg); err != nil {
			return err
		}
		saved = *cfg
		return nil
	})
	if err != nil {
		// Keep the popup and its dirty working copy alive so another esc can
		// retry after the save problem has been fixed.
		m.notice = "could not save settings: " + err.Error()
		s.notice = m.notice
		return false
	}
	m.setSettingsSavedNotice(ApplyConfig(saved))
	// A concurrent writer may have changed an untouched known row. Reflect the
	// config that is now live so reopening this Model cannot show stale values.
	s.refreshLiveValues()
	s.changedBinds = map[string]bool{}
	s.changedLabels = map[model.Status]bool{}
	s.dirty = false
	return true
}

func (s *settingsState) mergeIntoConfig(cfg *store.Config) error {
	for _, a := range bindActions {
		if !s.changedBinds[a.id] {
			continue
		}
		if s.binds[a.id] == a.def {
			delete(cfg.Keys, a.id)
			continue
		}
		if cfg.Keys == nil {
			cfg.Keys = map[string]string{}
		}
		cfg.Keys[a.id] = s.binds[a.id]
	}
	for _, status := range model.ColumnOrder {
		if s.changedLabels[status] {
			mergeColumnLabel(cfg, status, s.labels[status])
		}
	}
	return s.validateMergedBindings(cfg.Keys)
}

// Another settings page may have claimed a key after this one opened. Validate
// the merged assignment while the lock is still held so two individually clean
// working copies cannot write a conflict.
func (s *settingsState) validateMergedBindings(bindings map[string]string) error {
	resolved, refused := sanitizeBindings(bindings)
	changedKeys := map[string]bool{}
	for _, a := range bindActions {
		if s.changedBinds[a.id] {
			changedKeys[s.binds[a.id]] = true
		}
		if s.changedBinds[a.id] && resolved[a.id] != s.binds[a.id] {
			return fmt.Errorf("key %q was claimed by another config change", s.binds[a.id])
		}
	}
	for _, id := range refused {
		if _, known := bindActionsByID[id]; known && changedKeys[bindings[id]] {
			return fmt.Errorf("key %q was claimed by another config change", bindings[id])
		}
	}
	return nil
}

func (m *Model) setSettingsSavedNotice(refused, unbound []string) {
	switch {
	case len(refused) > 0:
		m.notice = fmt.Sprintf("settings saved — %d binding(s) refused on load", len(refused))
	case len(unbound) > 0:
		// An action whose default key an override claimed has none left. Say
		// so here: the alternative is finding out by pressing it.
		m.notice = fmt.Sprintf("settings saved — %s left unbound", strings.Join(unbound, ", "))
	case m.settings.undone > 0:
		// Saying only "settings saved" here would let someone leave believing an
		// edit took when it had just been rolled back.
		m.notice = fmt.Sprintf("settings saved — undid %d clashing change(s)", m.settings.undone)
	default:
		m.notice = "settings saved"
	}
	m.settings.undone = 0
}

// mergeColumnLabel changes only one canonical status in the latest config.
// Resetting clears every spelling of that status so an earlier duplicate
// cannot become effective again; an entry with an independent short label is
// retained, while a label-only entry disappears entirely.
func mergeColumnLabel(cfg *store.Config, status model.Status, label string) {
	last := -1
	for i, cc := range cfg.Columns {
		if parsed, err := model.ParseStatus(cc.Status); err == nil && parsed == status {
			last = i
		}
	}
	if label != defaultStatusDisplay[status] {
		if last < 0 {
			cfg.Columns = append(cfg.Columns, store.ColumnConfig{
				Status: string(status), Label: label,
			})
			return
		}
		cfg.Columns[last].Label = label
		return
	}

	columns := cfg.Columns[:0]
	for _, cc := range cfg.Columns {
		if parsed, err := model.ParseStatus(cc.Status); err == nil && parsed == status {
			cc.Label = ""
			if cc.Short == "" {
				continue
			}
		}
		columns = append(columns, cc)
	}
	cfg.Columns = columns
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
			for {
				label = fmt.Sprintf("%s (%s)", label, status)
				if _, clash = byLabel[label]; !clash {
					break
				}
			}
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
			switch {
			case msg.Type == tea.KeyRunes && (msg.Paste || len(msg.Runes) != 1):
				// Pasted text is one KeyRunes message carrying every rune, so
				// binding it would produce a key no keypress can ever match.
				s.notice = "that looks like pasted text — press a key instead"
			case reservedKeys()[k]:
				s.notice = k + " is reserved — it's how you get around"
			default:
				s.binds[a.id] = k
				s.markBindingChanged(a.id)
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
				s.markLabelChanged(st)
				s.notice = "renamed to " + name
			}
			s.editing, s.buf = false, ""
		case "backspace":
			if r := []rune(s.buf); len(r) > 0 {
				s.buf = string(r[:len(r)-1])
			}
		default:
			// Rune-aware, so "é" and CJK type as readily as ASCII. A paste is a
			// single KeyRunes message carrying every rune, and this is a text
			// field, so take all of them. Anything bubbletea reports as a named
			// chord ("ctrl+a", "shift+tab") is not KeyRunes and is ignored.
			if msg.Type == tea.KeyRunes {
				room := maxColumnLabel - len([]rune(s.buf))
				if room <= 0 {
					s.notice = fmt.Sprintf("a column name stops at %d characters", maxColumnLabel)
					return m, nil
				}
				add := msg.Runes
				if len(add) > room {
					add = add[:room]
					s.notice = fmt.Sprintf("trimmed to %d characters", maxColumnLabel)
				}
				s.buf += string(add)
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
					s.markBindingChanged(a.id)
				}
				s.notice = "all shortcuts back to defaults"
			case "resetLabels":
				// This replaces the complete set in one pass; the built-in labels
				// are unique, so unlike a single-row reset it cannot make a clash.
				for _, st := range model.ColumnOrder {
					s.labels[st] = defaultStatusDisplay[st]
					s.markLabelChanged(st)
				}
				s.notice = "all column names back to defaults"
			}
		} else {
			s.notice = "cancelled"
		}
		s.confirm = ""
		return m, nil
	}

	switch {
	case key.Matches(msg, keys.Quit):
		return m, tea.Quit
	case k == "esc":
		// Checked from every section: a conflict made in Shortcuts and then
		// escaped from Columns used to close and write itself to disk.
		if s.conflictCount() > 0 {
			if !s.warned {
				s.warned = true
				return m, nil
			}
			s.undone = s.revertConflicts()
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
			s.markBindingChanged(a.id)
			s.notice = a.label + " back to " + a.def
		case sectionColumns:
			st := model.ColumnOrder[s.idx]
			if s.labels[st] == defaultStatusDisplay[st] {
				s.notice = "already default"
				return m, nil
			}
			if s.labelTaken(st, defaultStatusDisplay[st]) {
				s.notice = defaultStatusDisplay[st] + " is already another column — left unchanged"
				return m, nil
			}
			s.labels[st] = defaultStatusDisplay[st]
			s.markLabelChanged(st)
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

// maxColumnLabel bounds a column name. The ticket status picker lays every
// label out on one unbounded horizontal line, so a few descriptive renames
// pushed later options past the right edge of an 80-column terminal, where
// bubbletea clips them with no ellipsis — you could arrow onto an option you
// could not read. Capping at the point of naming is the cheapest place to stop
// that. A hand-edited config.json can still exceed it; that is not clamped,
// because silently rewriting what someone typed into a file is worse.
const maxColumnLabel = 16

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
	body, rowIndexes, cursorRow := m.settingsBody(inner)

	// Fixed height: the body window is whatever the chrome leaves, and a short
	// section pads rather than shrinking the popup under the cursor.
	visible := height - 2 - len(header) - len(footer) - 1
	if visible < 1 {
		visible = 1
	}
	windowRows, windowIndexes := windowSettingsRows(body, rowIndexes, cursorRow, visible)
	m.registerSettingsZones(origin, inner, len(header), tabSpans, windowIndexes)

	rows := append([]string{}, header...)
	rows = append(rows, windowRows...)
	rows = append(rows, divider)
	rows = append(rows, footer...)

	content := lipgloss.NewStyle().PaddingLeft(1).Render(strings.Join(rows, "\n"))
	popup := renderPanel("Settings", content, width, height, green, true)
	return overlayAt(backdrop, popup, origin.x, origin.y)
}

func (m *Model) settingsBody(width int) ([]string, []int, int) {
	switch m.settings.section {
	case sectionShortcuts:
		return m.settings.shortcutRows(width)
	case sectionColumns:
		return m.settings.columnRows(width)
	case sectionAbout:
		rows := m.aboutRows(width)
		indexes := make([]int, len(rows))
		for i := range indexes {
			indexes[i] = -1
		}
		return rows, indexes, 0
	default:
		return nil, nil, 0
	}
}

func windowSettingsRows(body []string, rowIndexes []int, cursorRow, visible int) ([]string, []int) {
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
	rows := append([]string{}, body[start:end]...)
	indexes := append([]int{}, rowIndexes[start:end]...)
	for len(rows) < visible {
		rows = append(rows, "")
		indexes = append(indexes, -1)
	}
	dim := lipgloss.NewStyle().Foreground(dimGray)
	if start > 0 {
		rows[0] = dim.Render(fmt.Sprintf(" ↑ %d more", start))
		indexes[0] = -1
	}
	if end < len(body) {
		rows[len(rows)-1] = dim.Render(fmt.Sprintf(" ↓ %d more", len(body)-end))
		indexes[len(rows)-1] = -1
	}
	return rows, indexes
}

func (m *Model) registerSettingsZones(origin point, width, headerHeight int, tabSpans []tabSpan, rowIndexes []int) {
	s := &m.settings
	if s.capturing || s.editing || s.confirm != "" {
		return
	}
	// Content sits one cell in from the border plus the PaddingLeft(1).
	contentX := origin.x + 2
	for i, span := range tabSpans {
		m.addZone(hitZone{kind: zoneSettingsTab, x: contentX + span.x,
			y: origin.y + 1, w: span.w, h: 1, idx: i})
	}
	for i, idx := range rowIndexes {
		if idx < 0 {
			continue
		}
		m.addZone(hitZone{kind: zoneSettingsRow, x: contentX,
			y: origin.y + 1 + headerHeight + i, w: width, h: 1, idx: idx})
	}
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

// aboutRows names the files this process is actually using. They move with
// KANBAN_FILE and with --sprint, so hard-coding ~/.kanban pointed people at the
// wrong board on every sprint — and naming the files is all this tab is for.
func (m *Model) aboutRows(w int) []string {
	dim := lipgloss.NewStyle().Foreground(dimGray)
	return []string{
		groupHeader("Files", w),
		padBetween(" "+dim.Render("  config"), fitPath(store.ConfigPath(), w-11)+" ", w),
		padBetween(" "+dim.Render("  board"), fitPath(m.store.BoardPath(), w-11)+" ", w),
	}
}

// fitPath shortens a path to width cells, from the LEFT — the filename and the
// directory holding it are what identify a board, so a path that has to lose
// something loses its root rather than its tail.
func fitPath(p string, width int) string {
	p = tildePath(p)
	if width < 4 || lipgloss.Width(p) <= width {
		return p
	}
	r := []rune(p)
	return "…" + string(r[len(r)-(width-1):])
}

// tildePath shortens a path under the user's home for display.
func tildePath(p string) string {
	home, err := os.UserHomeDir()
	if err != nil || home == "" || !strings.HasPrefix(p, home+string(os.PathSeparator)) {
		return p
	}
	return "~" + p[len(home):]
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
	case s.conflictCount() > 0:
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
