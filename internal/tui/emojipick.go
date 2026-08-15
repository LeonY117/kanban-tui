package tui

// Prototype (KA18): a floating emoji picker over any text field. keys.Emoji —
// alt+e by default, rebindable from settings — opens it while typing, and the
// pick lands back in the field it came from: a title
// gets it as its prefix, everything else at the cursor. It navigates like the
// board: hjkl/arrows move, and `/` starts a live filter over names and
// keywords ("happy" finds 🙂) with the board search's grammar — enter keeps
// the filter, esc clears it, esc again closes. Sections mirror a real picker:
// "Most used" first, computed from every live board, then the Unicode groups.
// Offering only the safe set (internal/emoji) is the point: what the picker
// inserts can never skew the board in another terminal.

import (
	"sort"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/LeonY117/kanban-tui/internal/emoji"
	"github.com/LeonY117/kanban-tui/internal/model"
	"github.com/LeonY117/kanban-tui/internal/store"
)

const (
	emojiCellWidth = 4  // emoji (2 cols) + 2 cols of breathing room
	emojiSticky    = 10 // size of the "Most used" section
)

// emojiTarget names the text widget a pick lands in. Titles take the emoji as
// their prefix (replacing an existing one); the rest insert at the cursor.
type emojiTarget int

const (
	emojiToAddTitle emojiTarget = iota
	emojiToAddDesc
	emojiToAddTags
	emojiToAddAssign
	emojiToEditTitle
	emojiToEditDesc
	emojiToInfoDesc
)

type emojiPicker struct {
	open       bool
	filtering  bool // typing into the filter, entered with `/`
	target     emojiTarget
	returnView viewMode
	filter     textinput.Model
	sel        int // index into the filtered flat list
	scroll     int // first visible grid row (headers count as rows)
	list       []emoji.Entry
}

func (m *Model) openEmojiPicker(target emojiTarget) (tea.Model, tea.Cmd) {
	in := textinput.New()
	in.Prompt = "/ "
	in.Placeholder = "filter"
	in.CharLimit = 40

	m.emojiPick = emojiPicker{
		open:       true,
		target:     target,
		returnView: m.view,
		filter:     in,
		list:       append(m.mostUsedEmoji(emojiSticky), emoji.Safe...),
	}
	m.view = emojiView
	return m, nil
}

// mostUsedEmoji is the "Most used" section: safe emoji leading ticket titles
// across the main board and every live sprint, most frequent first. Computed
// at open so it tracks how the boards actually speak, with nothing to curate.
func (m *Model) mostUsedEmoji(limit int) []emoji.Entry {
	byEmoji := make(map[string]emoji.Entry, len(emoji.Safe))
	for _, e := range emoji.Safe {
		byEmoji[e.Emoji] = e
	}

	count := make(map[string]int)
	tally := func(s *store.Store, err error) {
		if err != nil || s == nil {
			return
		}
		board, err := s.Load()
		if err != nil {
			return
		}
		for _, t := range board.Tickets {
			// A redundant VS16 on a safe emoji is the same emoji.
			lead := strings.TrimSuffix(emoji.Lead(t.Title), "️")
			if _, ok := byEmoji[lead]; ok {
				count[lead]++
			}
		}
	}

	tally(store.New(""), nil)
	if sprints, err := store.ListSprints(); err == nil {
		for _, s := range sprints {
			if !s.Archived {
				tally(store.NewSprint(s.Name))
			}
		}
	}

	used := make([]string, 0, len(count))
	for e := range count {
		used = append(used, e)
	}
	sort.Slice(used, func(i, j int) bool {
		if count[used[i]] != count[used[j]] {
			return count[used[i]] > count[used[j]]
		}
		return used[i] < used[j] // deterministic order among ties
	})
	if len(used) > limit {
		used = used[:limit]
	}

	out := make([]emoji.Entry, 0, len(used))
	for _, e := range used {
		entry := byEmoji[e]
		entry.Group = "Most used"
		out = append(out, entry)
	}
	return out
}

func (m *Model) closeEmojiPicker() {
	m.view = m.emojiPick.returnView
	m.emojiPick = emojiPicker{}
}

// emojiFiltered narrows the list to entries matching every filter word in the
// name or keywords, deduplicated (sticky entries repeat in their home
// section). Name matches outrank keyword-only matches, and within names exact
// beats prefix beats substring — typing "locked" picks 🔒, not 🔏, and
// "happy" still finds the smileys by keyword.
func (m *Model) emojiFiltered() []emoji.Entry {
	words := strings.Fields(strings.ToLower(m.emojiPick.filter.Value()))
	if len(words) == 0 {
		return m.emojiPick.list
	}
	query := strings.Join(words, " ")
	seen := make(map[string]bool)
	var exact, prefix, inName, byKeyword []emoji.Entry
	for _, e := range m.emojiPick.list {
		if seen[e.Emoji] {
			continue
		}
		allInName, match := true, true
		for _, w := range words {
			inN := strings.Contains(e.Name, w)
			if !inN && !strings.Contains(e.Keywords, w) {
				match = false
				break
			}
			allInName = allInName && inN
		}
		if !match {
			continue
		}
		seen[e.Emoji] = true
		switch {
		case e.Name == query:
			exact = append(exact, e)
		case allInName && strings.HasPrefix(e.Name, words[0]):
			prefix = append(prefix, e)
		case allInName:
			inName = append(inName, e)
		default:
			byKeyword = append(byKeyword, e)
		}
	}
	return append(append(append(exact, prefix...), inName...), byKeyword...)
}

// gridRow is one rendered row of the picker: a section header, or up to cols
// consecutive entries of the flat filtered list starting at start.
type gridRow struct {
	header   string
	start, n int
}

// emojiRows lays the flat list into rows. Sections only exist while the list
// is unfiltered — ranking a filter reorders across groups, so headers would
// lie there; the filtered grid is flat instead.
func (m *Model) emojiRows(list []emoji.Entry, cols int) []gridRow {
	sectioned := strings.TrimSpace(m.emojiPick.filter.Value()) == ""
	var rows []gridRow
	group := ""
	rowStart, rowN := 0, 0
	flush := func() {
		if rowN > 0 {
			rows = append(rows, gridRow{start: rowStart, n: rowN})
		}
		rowN = 0
	}
	for i, e := range list {
		if sectioned && e.Group != group {
			flush()
			group = e.Group
			rows = append(rows, gridRow{header: group})
		}
		if rowN == 0 {
			rowStart = i
		}
		rowN++
		if rowN == cols {
			flush()
		}
	}
	flush()
	return rows
}

// emojiRowOf locates sel's row index and column within rows.
func emojiRowOf(rows []gridRow, sel int) (ri, col int) {
	for i, r := range rows {
		if r.header == "" && sel >= r.start && sel < r.start+r.n {
			return i, sel - r.start
		}
	}
	return 0, 0
}

// emojiPickerSize is the floating popup's outer size.
func (m *Model) emojiPickerSize() (int, int) {
	w := 50
	if w > m.width-4 {
		w = m.width - 4
	}
	if w < 24 {
		w = 24
	}
	h := 20
	if h > m.height-4 {
		h = m.height - 4
	}
	if h < 8 {
		h = 8
	}
	return w, h
}

// emojiGridCols is how many cells fit one grid row at the popup's inner width.
func emojiGridCols(innerWidth int) int {
	cols := (innerWidth - 2) / emojiCellWidth
	if cols < 1 {
		cols = 1
	}
	return cols
}

// moveEmojiRow steps the selection to the nearest cell row above or below,
// keeping the column — how every grid picker walks across section headers.
// A method rather than a closure so the wheel reaches it too, like every
// other list in the TUI.
func (m *Model) moveEmojiRow(dir int) {
	w, _ := m.emojiPickerSize()
	rows := m.emojiRows(m.emojiFiltered(), emojiGridCols(w-2))
	ri, col := emojiRowOf(rows, m.emojiPick.sel)
	for i := ri + dir; i >= 0 && i < len(rows); i += dir {
		if rows[i].header == "" {
			if col >= rows[i].n {
				col = rows[i].n - 1
			}
			m.emojiPick.sel = rows[i].start + col
			return
		}
	}
}

func (m *Model) updateEmojiPicker(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	filtered := m.emojiFiltered()

	moveRow := m.moveEmojiRow
	moveCell := func(dir int) {
		if next := m.emojiPick.sel + dir; next >= 0 && next < len(filtered) {
			m.emojiPick.sel = next
		}
	}
	pick := func() (tea.Model, tea.Cmd) {
		if m.emojiPick.sel < len(filtered) {
			m.applyPickedEmoji(filtered[m.emojiPick.sel].Emoji)
		}
		return m, nil
	}

	// Filter mode follows the board search's grammar: typing narrows live,
	// up/down keep moving the selection, enter commits the filter, esc
	// clears it. Left/right stay with the text cursor, as in any input.
	if m.emojiPick.filtering {
		switch msg.String() {
		case "esc":
			// Only a query that actually narrowed the list invalidates where
			// the cursor was; backing out of an empty `/` leaves the grid
			// exactly where it was scrolled to.
			if m.emojiPick.filter.Value() != "" {
				m.emojiPick.filter.SetValue("")
				m.emojiPick.sel, m.emojiPick.scroll = 0, 0
			}
			m.emojiPick.filter.Blur()
			m.emojiPick.filtering = false
		case "enter":
			m.emojiPick.filter.Blur()
			m.emojiPick.filtering = false
		case "up", "ctrl+p":
			moveRow(-1)
		case "down", "ctrl+n":
			moveRow(1)
		// Both spellings, like the board search: backspacing past the start
		// of the query deletes the slash too and drops back to nav mode. It
		// commits rather than cancels for the board's reason — an empty
		// filter already shows everything (see updateSearch).
		case "backspace", "ctrl+h":
			if m.emojiPick.filter.Value() == "" {
				m.emojiPick.filter.Blur()
				m.emojiPick.filtering = false
				return m, nil
			}
			var cmd tea.Cmd
			m.emojiPick.filter, cmd = m.emojiPick.filter.Update(msg)
			m.emojiPick.sel, m.emojiPick.scroll = 0, 0
			return m, cmd
		default:
			var cmd tea.Cmd
			m.emojiPick.filter, cmd = m.emojiPick.filter.Update(msg)
			m.emojiPick.sel, m.emojiPick.scroll = 0, 0
			return m, cmd
		}
		return m, nil
	}

	switch msg.String() {
	// ctrl+c alone, not keys.Quit: the sibling popups bind q as well, but they
	// are never summoned from a half-typed ticket. This one is, and quitting
	// discards that draft without a confirm — so q stays inert here, the way
	// the search input treats it (see updateSearch).
	case "ctrl+c":
		return m, tea.Quit
	case "/":
		m.emojiPick.filtering = true
		m.emojiPick.filter.Focus()
		m.emojiPick.filter.CursorEnd()
		return m, textinput.Blink
	case "esc":
		// Like the board: esc clears an active filter first, then closes.
		if m.emojiPick.filter.Value() != "" {
			m.emojiPick.filter.SetValue("")
			m.emojiPick.sel, m.emojiPick.scroll = 0, 0
			return m, nil
		}
		m.closeEmojiPicker()
	case "enter":
		return pick()
	case "h", "left":
		moveCell(-1)
	case "l", "right":
		moveCell(1)
	case "k", "up":
		moveRow(-1)
	case "j", "down":
		moveRow(1)
	}
	return m, nil
}

// applyPickedEmoji lands e in the picker's target widget and closes the
// picker; the target keeps its editing state, so typing just continues.
func (m *Model) applyPickedEmoji(e string) {
	switch m.emojiPick.target {
	case emojiToAddTitle:
		setTitlePrefix(&m.addTitle, e)
	case emojiToEditTitle:
		setTitlePrefix(&m.editTitle, e)
	case emojiToAddDesc:
		m.addDesc.InsertString(e)
	case emojiToEditDesc:
		m.editDesc.InsertString(e)
	case emojiToInfoDesc:
		m.infoDesc.InsertString(e)
	case emojiToAddTags:
		insertAtCursor(&m.addTags, e)
	case emojiToAddAssign:
		insertAtCursor(&m.addAssign, e)
	}
	m.closeEmojiPicker()
}

// setTitlePrefix makes e the title's emoji prefix, replacing whatever emoji —
// safe or fragile — led the title before.
func setTitlePrefix(in *textinput.Model, e string) {
	title := in.Value()
	if lead := emoji.Lead(title); lead != "" {
		title = strings.TrimLeft(strings.TrimPrefix(title, lead), " ")
	}
	// The gap stays on an empty title so typing continues after it.
	if !setWithinLimit(in, e+" "+title) {
		return
	}
	in.CursorEnd()
}

// insertAtCursor splices s into the input at its cursor. Position and
// SetCursor speak runes, so the splice does too.
func insertAtCursor(in *textinput.Model, s string) {
	runes := []rune(in.Value())
	pos := in.Position()
	if pos > len(runes) {
		pos = len(runes)
	}
	if !setWithinLimit(in, string(runes[:pos])+s+string(runes[pos:])) {
		return
	}
	in.SetCursor(pos + len([]rune(s)))
}

// setWithinLimit writes v only if it fits the input's CharLimit, reporting
// whether it did. textinput.SetValue truncates from the end instead of
// refusing, so a field already at its cap would silently lose its tail to make
// room for the emoji. Declining is what the widget itself does when you type
// at the cap, and it never eats text the user already entered.
// (textarea.InsertString needs no such guard — it trims the insertion.)
func setWithinLimit(in *textinput.Model, v string) bool {
	if in.CharLimit > 0 && len([]rune(v)) > in.CharLimit {
		return false
	}
	in.SetValue(v)
	return true
}

// viewEmoji floats the picker over whichever view summoned it.
func (m *Model) viewEmoji() string {
	backdrop := m.renderView(m.emojiPick.returnView)
	// The backdrop's zones belong to a view the user can't reach right now.
	m.resetZones()
	w, h := m.emojiPickerSize()
	origin := m.popupOrigin(w, h)
	popup := m.renderEmojiPicker(w, h, origin)
	return overlayAt(backdrop, popup, origin.x, origin.y)
}

// renderEmojiPicker draws the floating picker panel. origin is its top-left
// screen cell, for the click zones.
func (m *Model) renderEmojiPicker(width, height int, origin point) string {
	filtered := m.emojiFiltered()
	cols := emojiGridCols(width - 2)
	rows := m.emojiRows(filtered, cols)

	// Inner rows: filter line + grid + name footer, between the panel borders.
	gridRows := height - 5
	if gridRows < 1 {
		gridRows = 1
	}

	// Keep the selection visible; drag the section header along when it sits
	// directly above the selected row.
	selRow, _ := emojiRowOf(rows, m.emojiPick.sel)
	if selRow > 0 && rows[selRow-1].header != "" {
		selRow--
	}
	if selRow < m.emojiPick.scroll {
		m.emojiPick.scroll = selRow
	}
	selRow, _ = emojiRowOf(rows, m.emojiPick.sel)
	if selRow >= m.emojiPick.scroll+gridRows {
		m.emojiPick.scroll = selRow - gridRows + 1
	}

	m.emojiPick.filter.Width = width - 8
	lines := []string{m.emojiPick.filter.View()}

	// Reverse video, like selectedFieldStyle, so the highlight adapts to any
	// terminal theme without picking colors.
	selStyle := lipgloss.NewStyle().Reverse(true)
	headerStyle := lipgloss.NewStyle().Foreground(midGray)
	for ri := m.emojiPick.scroll; ri < m.emojiPick.scroll+gridRows && ri < len(rows); ri++ {
		row := rows[ri]
		if row.header != "" {
			lines = append(lines, headerStyle.Render("─ "+strings.ToLower(row.header)))
			continue
		}
		var cells []string
		for c := 0; c < row.n; c++ {
			i := row.start + c
			cell := " " + filtered[i].Emoji + " "
			if i == m.emojiPick.sel {
				cell = selStyle.Render(cell)
			}
			cells = append(cells, cell)
			m.addZone(hitZone{kind: zoneEmojiCell,
				x: origin.x + 1 + c*emojiCellWidth, y: origin.y + 2 + (ri - m.emojiPick.scroll),
				w: emojiCellWidth, h: 1, idx: i})
		}
		lines = append(lines, strings.Join(cells, ""))
	}

	name := ""
	if m.emojiPick.sel < len(filtered) {
		name = filtered[m.emojiPick.sel].Name
	} else if len(filtered) == 0 {
		name = "no match"
	}
	lines = append(lines, headerStyle.Render(name))

	title := "Emoji"
	if !m.emojiPick.filtering {
		title = "Emoji — / to filter"
	}
	return renderPanel(title, strings.Join(lines, "\n"), width, height, columnColor(model.ColumnOrder[m.focusedCol]), true)
}
