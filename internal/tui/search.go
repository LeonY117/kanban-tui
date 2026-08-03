package tui

import (
	"fmt"
	"strings"
	"unicode"

	"github.com/LeonY117/kanban-tui/internal/model"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// ─── State ───────────────────────────────────────────────────────────

// searchState is the board filter: `/` opens an input over the footer hint
// line and the board narrows live as you type.
//
// It is session-only and never persisted. Reopening a board to find half of it
// missing, with no memory of why, is a worse trade than retyping four
// characters.
type searchState struct {
	input  textinput.Model
	open   bool        // the input holds the keyboard
	query  string      // live text — the board filters while typing, not on submit
	parsed model.Query // query, reparsed on every change
	global bool        // scope: every board rather than this one

	// What to put back when the input is cancelled rather than committed.
	prevQuery  string
	prevGlobal bool

	tagIdx int // highlighted tag completion

	// Cards borrowed from other boards under global scope, and the board each
	// came from. Membership in owners is what makes a card foreign: the main
	// board's name is "", so comparing names would read it as local.
	foreign []model.Ticket
	owners  map[string]string
}

func newSearchState() searchState {
	ti := textinput.New()
	ti.Prompt = "/"
	ti.CharLimit = 120
	ti.Placeholder = "title, #tag, assignee"
	return searchState{input: ti}
}

// searchActive reports whether anything is narrowing or widening the board.
func (m *Model) searchActive() bool {
	return !m.search.parsed.Empty() || m.search.global
}

// ─── The filtered read ───────────────────────────────────────────────

// visibleTickets is the single answer to "what is in this column right now",
// and every cursor, render and move path reads through it.
//
// Reads that mean "what is on the board" — picker counts, anything that writes
// to the store — deliberately stay on board.ByStatus. A cursor indexes what is
// on screen while the store indexes what exists, and under a filter those are
// different lists: mixing them up selects a card you can't see, or lands a
// move on the wrong neighbour.
func (m *Model) visibleTickets(status model.Status) []model.Ticket {
	local := m.board.ByStatus(status)
	if !m.searchActive() {
		return local
	}
	out := m.search.parsed.MatchAll(local)
	if !m.search.global {
		return out
	}

	// Foreign cards trail the local ones, in board order, so the board you are
	// standing on stays at the top of every column.
	merged := make([]model.Ticket, len(out), len(out)+len(m.search.foreign))
	copy(merged, out)
	for _, t := range m.search.foreign {
		if t.Status == status && m.search.parsed.Match(t) {
			merged = append(merged, t)
		}
	}
	return merged
}

// ticketOwner names the board a card came from, and reports false for cards
// belonging to the board on screen.
func (m *Model) ticketOwner(id string) (string, bool) {
	name, ok := m.search.owners[id]
	return name, ok
}

// searchPool is every card the current scope can return, across all columns.
func (m *Model) searchPool() []model.Ticket {
	pool := make([]model.Ticket, 0, len(m.board.Tickets)+len(m.search.foreign))
	pool = append(pool, m.board.Tickets...)
	if m.search.global {
		pool = append(pool, m.search.foreign...)
	}
	return pool
}

// searchCounts is the "12 of 58" in the footer: how many cards survive the
// filter, out of how many the scope can see.
func (m *Model) searchCounts() (shown, total int) {
	for _, status := range model.ColumnOrder {
		shown += len(m.visibleTickets(status))
	}
	return shown, len(m.searchPool())
}

// ─── Opening, closing, scope ─────────────────────────────────────────

// enterSearch opens the input pre-filled with the live query and the cursor at
// its end — the sprint rename's shape, so refining a filter never means
// retyping it.
func (m *Model) enterSearch() {
	m.search.prevQuery = m.search.query
	m.search.prevGlobal = m.search.global
	m.search.input.SetValue(m.search.query)
	m.search.input.CursorEnd()
	m.search.input.Focus()
	m.search.open = true
	m.search.tagIdx = 0
}

// commitSearch closes the input and leaves the filter standing. Filtering in
// place is only worth doing if you can then move around what's left, which
// means the filter has to outlive the input that made it.
func (m *Model) commitSearch() {
	m.search.open = false
	m.search.input.Blur()
	if !m.searchActive() {
		m.clearSearch()
	}
}

// cancelSearch puts the board back exactly as it was before the input opened.
func (m *Model) cancelSearch() {
	m.search.open = false
	m.search.input.Blur()
	m.setQuery(m.search.prevQuery)
	if m.search.global != m.search.prevGlobal {
		m.search.global = m.search.prevGlobal
		m.loadForeign()
	}
	m.clampCursors()
}

// clearSearch drops the filter and the scope together. Scope is part of the
// filter, not a separate mode: leaving it on after a clear would keep other
// boards' cards on screen with nothing left to explain why.
func (m *Model) clearSearch() {
	m.search.open = false
	m.search.input.Blur()
	m.search.input.SetValue("")
	m.setQuery("")
	m.search.tagIdx = 0
	m.search.global = false
	m.loadForeign()
	m.clampCursors()
}

func (m *Model) setQuery(q string) {
	m.search.query = q
	m.search.parsed = model.ParseQuery(q)
}

// toggleSearchScope flips between this board and every board.
func (m *Model) toggleSearchScope() {
	m.search.global = !m.search.global
	m.loadForeign()
	m.search.tagIdx = 0
	m.clampCursors()
}

// scopeToggleLabel names where ctrl+g would take the search, not where it is —
// a hint is only useful if it says what the key does next.
func (m *Model) scopeToggleLabel() string {
	if m.search.global {
		return "this board"
	}
	return "all boards"
}

// loadForeign reads every other active board into memory for global scope.
//
// Archived sprints stay out. They are hidden from the picker until asked for,
// and a search is the wrong place for them to reappear uninvited.
//
// This is a snapshot taken when the scope changes or the board reloads, not a
// live read: a card edited on another board mid-search shows its old text
// until the next reload. Boards are small and the alternative is re-reading
// every sprint on every keystroke.
func (m *Model) loadForeign() {
	m.search.foreign = nil
	m.search.owners = nil
	if !m.search.global {
		return
	}

	entries, err := loadPickerEntries(false)
	if err != nil {
		m.notice = "could not read the other boards: " + err.Error()
		m.search.global = false
		return
	}

	owners := map[string]string{}
	for _, e := range entries {
		if e.name == m.sprintName {
			continue
		}
		s, err := boardStore(e.name)
		if err != nil {
			continue
		}
		b, err := s.Load()
		if err != nil {
			continue
		}
		for _, t := range b.Tickets {
			owners[t.ID] = e.name
			m.search.foreign = append(m.search.foreign, t)
		}
	}
	m.search.owners = owners
}

// jumpToForeign follows a card borrowed by a global search home to the board
// that owns it, keeping the query so the same cards stay on screen. Scope
// drops back to this board, because you are now standing on the one that card
// lives in. Reports false when the selection is local and the caller should
// carry on as normal.
func (m *Model) jumpToForeign() bool {
	t := m.selectedTicket()
	if t == nil {
		return false
	}
	owner, ok := m.ticketOwner(t.ID)
	if !ok {
		return false
	}

	id := t.ID
	query := m.search.query
	if err := m.switchBoard(owner); err != nil {
		m.notice = err.Error()
		return true
	}
	m.setQuery(query)
	m.search.input.SetValue(query)
	m.focusTicket(id)
	m.notice = "on " + boardDisplayName(owner)
	return true
}

// focusTicket puts the cursor on a card by id, if the filter leaves it on
// screen at all.
func (m *Model) focusTicket(id string) {
	for col, status := range model.ColumnOrder {
		for i, t := range m.visibleTickets(status) {
			if t.ID == id {
				m.focusedCol = col
				m.cursors[col] = i
				m.clampCursors()
				return
			}
		}
	}
	m.clampCursors()
}

// ─── Tag completion ──────────────────────────────────────────────────

// pendingTag is the `#term` being typed, if the cursor is on one.
//
// Completion only offers itself at the end of the input. Mid-line the last
// field is not the one being edited, and completing it would rewrite text the
// user isn't looking at.
func (m *Model) pendingTag() (prefix string, before string, ok bool) {
	value := m.search.input.Value()
	if m.search.input.Position() != len([]rune(value)) {
		return "", "", false
	}
	cut := strings.LastIndexFunc(value, unicode.IsSpace) + 1
	last := value[cut:]
	if !strings.HasPrefix(last, "#") {
		return "", "", false
	}
	return strings.TrimPrefix(last, "#"), value[:cut], true
}

// tagCandidates are the completions for the tag being typed, counted against
// the cards the rest of the query already selects — so the number beside a tag
// is the number of cards picking it leaves you with.
//
// A bare `#` has an empty prefix and offers everything, which is how the tag
// list PR #5 built a modal picker for gets discovered from inside the input.
func (m *Model) tagCandidates() []model.TagCount {
	prefix, before, ok := m.pendingTag()
	if !ok {
		return nil
	}
	context := model.ParseQuery(before)
	return model.TagCandidates(context.MatchAll(m.searchPool()), prefix)
}

// acceptTagCompletion writes the highlighted candidate into the query.
func (m *Model) acceptTagCompletion() bool {
	_, before, ok := m.pendingTag()
	if !ok {
		return false
	}
	cands := m.tagCandidates()
	if len(cands) == 0 {
		return false
	}
	m.search.input.SetValue(before + "#" + cands[clampIndex(m.search.tagIdx, len(cands))].Tag + " ")
	m.search.input.CursorEnd()
	m.syncQuery()
	return true
}

// moveTagCursor walks the completion list without editing the query, so the
// candidate tab takes can be chosen when the top one isn't the wanted one.
func (m *Model) moveTagCursor(dir int) {
	n := len(m.tagCandidates())
	if n == 0 {
		return
	}
	m.search.tagIdx = ((clampIndex(m.search.tagIdx, n)+dir)%n + n) % n
}

// ─── Keys ────────────────────────────────────────────────────────────

// updateSearch owns the keyboard while the input is open.
func (m *Model) updateSearch(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.Type {
	case tea.KeyEsc:
		m.cancelSearch()
		return m, nil
	case tea.KeyEnter:
		m.commitSearch()
		return m, nil
	case tea.KeyCtrlC:
		return m, tea.Quit
	case tea.KeyCtrlG:
		m.toggleSearchScope()
		return m, nil
	case tea.KeyTab:
		m.acceptTagCompletion()
		return m, nil
	case tea.KeyCtrlN, tea.KeyDown:
		m.moveTagCursor(1)
		return m, nil
	case tea.KeyCtrlP, tea.KeyUp:
		m.moveTagCursor(-1)
		return m, nil
	}

	var cmd tea.Cmd
	m.search.input, cmd = m.search.input.Update(msg)
	m.syncQuery()
	return m, cmd
}

// syncQuery re-reads the input after an edit. The cursor is clamped here
// rather than at the render, because a narrowing column can strand it past the
// end of a list that still has to answer selectedTicket correctly.
func (m *Model) syncQuery() {
	m.setQuery(m.search.input.Value())
	m.search.tagIdx = 0
	m.clampCursors()
}

// ─── Footer ──────────────────────────────────────────────────────────

// searchInputWidth is how much of the footer the query gets. A third of the
// terminal, bounded: too narrow and a two-term query scrolls out of sight,
// too wide and the match count it exists to explain gets pushed off the end.
func searchInputWidth(total int) int {
	w := total / 3
	if w < 12 {
		w = 12
	}
	if w > 40 {
		w = 40
	}
	return w
}

// searchFooter replaces the hint line while the input is open: the query, then
// tag completions, the match count, and whatever hints still fit. There is one
// line to share, so the pieces compete for it and the least useful drop first.
func (m *Model) searchFooter(badge string) string {
	m.search.input.Width = searchInputWidth(m.width)
	input := helpStyle.Render(m.search.input.View())

	budget := m.width - lipgloss.Width(badge) - lipgloss.Width(input) - 2
	shown, total := m.searchCounts()
	hints := fmt.Sprintf("%d/%d | ^g %s | tab tag | esc cancel", shown, total, m.scopeToggleLabel())

	// Completions get whatever is left once the count and the way out are
	// safe — those two are the line's floor, and fitHints protects the last
	// hint by construction. Giving the strip a fixed share instead silently
	// dropped candidates while hint text it outranks sat next to it.
	floor := fmt.Sprintf("%d/%d | esc cancel", shown, total)
	right := fitHints(hints, budget)
	if strip := m.completionStrip(budget - lipgloss.Width(floor) - 2); strip != "" {
		right = strip + "  " + fitHints(hints, budget-lipgloss.Width(strip)-2)
	}
	return lipgloss.JoinHorizontal(lipgloss.Center, badge, input, helpStyle.Render(right))
}

// completionStrip lists the tag completions that fit, marking the one tab
// would take.
func (m *Model) completionStrip(budget int) string {
	cands := m.tagCandidates()
	if len(cands) == 0 || budget < 6 {
		return ""
	}
	idx := clampIndex(m.search.tagIdx, len(cands))

	render := func(start int) (string, int) {
		var parts []string
		used, last := 0, start-1
		for i := start; i < len(cands); i++ {
			text := fmt.Sprintf("#%s %d", cands[i].Tag, cands[i].Count)
			cost := lipgloss.Width(text)
			if len(parts) > 0 {
				cost += 2
			}
			if used+cost > budget {
				break
			}
			used += cost
			last = i
			if i == idx {
				parts = append(parts, searchTagStyle.Render(text))
			} else {
				parts = append(parts, tagStyle.Render(text))
			}
		}
		return strings.Join(parts, "  "), last
	}

	// Walking the list with ctrl+n can carry the highlight past what fits from
	// the front; anchoring the strip on it then keeps the choice visible.
	strip, last := render(0)
	if idx > last {
		strip, _ = render(idx)
	}
	return strip
}

// searchChip is the footer's reminder that the board is filtered once the
// input has closed. Without it a narrowed board is indistinguishable from a
// board that lost its cards.
func (m *Model) searchChip() string {
	var parts []string
	if m.search.query != "" {
		parts = append(parts, "/"+m.search.query)
	}
	if m.search.global {
		parts = append(parts, "all boards")
	}
	shown, total := m.searchCounts()
	return fmt.Sprintf("%s  %d of %d", strings.Join(parts, " · "), shown, total)
}
