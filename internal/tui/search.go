package tui

import (
	"fmt"
	"strings"
	"unicode"

	"github.com/LeonY117/kanban-tui/internal/model"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

// ─── State ───────────────────────────────────────────────────────────

// searchState is a surface filter: `/` opens an input over the footer hint
// line and the board or archive narrows live as you type.
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
	prevTicket string // id of the card the cursor was on, restored by esc

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

// active reports whether this filter narrows or widens what its surface shows.
func (s *searchState) active() bool { return !s.parsed.Empty() || s.global }

// searchActive is the board's filter specifically. visibleTickets and the zoom
// guard mean the board even while the archive browser is on screen, so they ask
// this rather than activeSearch.
func (m *Model) searchActive() bool { return m.search.active() }

// ─── Which surface is being filtered ─────────────────────────────────

// activeSearch is the filter belonging to the surface on screen.
//
// The board and the archive browser keep separate ones (Leon, 2026-08-04).
// They are separate lists — one holds what is open, the other what is finished
// — and a query typed over one has no meaning over the other. Sharing the value
// would also mean stepping into the archive silently re-scoped the board.
// Sharing the *type* is the point: one implementation, two instances.
func (m *Model) activeSearch() *searchState {
	if m.view == archiveView {
		return &m.archiveSearch
	}
	return &m.search
}

// activePool is every ticket the active surface's scope can reach — what the
// completion counts and the "of N" are measured against.
func (m *Model) activePool() []model.Ticket {
	if m.view == archiveView {
		return m.archivePool()
	}
	return m.searchPool()
}

// activeCounts is the "12 of 58" for whichever surface is on screen.
func (m *Model) activeCounts() (shown, total int) {
	if m.view == archiveView {
		return m.archiveCounts()
	}
	return m.searchCounts()
}

// refreshActiveSelection re-clamps whichever cursor the filter just moved under.
func (m *Model) refreshActiveSelection() {
	if m.view == archiveView {
		m.clampArchiveCursor()
		return
	}
	m.refreshSearchSelection()
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
	name, ok := m.activeSearch().owners[id]
	return name, ok
}

// boardBadge is the `demo/` prefix a borrowed card wears, empty for local
// ones. Every surface that prints a short id prints this in front of it —
// a borrowed card that reads as local is one keystroke from a refusal
// naming a board the view never mentioned.
func (m *Model) boardBadge(id string) string {
	if owner, ok := m.ticketOwner(id); ok {
		return boardDisplayName(owner) + "/"
	}
	return ""
}

func (m *Model) renderTicketID(t model.Ticket, style lipgloss.Style) string {
	return foreignBoardStyle.Render(m.boardBadge(t.ID)) + style.Render(t.ShortID)
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
	st := m.activeSearch()
	st.prevQuery = st.query
	st.prevGlobal = st.global
	// Remember the card, not the index: typing narrows the column and clamps
	// the cursor, so an index saved here means something else by the time esc
	// puts it back.
	st.prevTicket = ""
	if t := m.activeSelectedTicket(); t != nil {
		st.prevTicket = t.ID
	}
	st.input.SetValue(st.query)
	st.input.CursorEnd()
	st.input.Focus()
	st.open = true
	st.tagIdx = 0
}

// activeSelectedTicket is whatever the surface on screen has under its cursor.
func (m *Model) activeSelectedTicket() *model.Ticket {
	if m.view == archiveView {
		return m.archiveSelected()
	}
	return m.selectedTicket()
}

// commitSearch closes the input and leaves the filter standing. Filtering in
// place is only worth doing if you can then move around what's left, which
// means the filter has to outlive the input that made it.
func (m *Model) commitSearch() {
	st := m.activeSearch()
	st.open = false
	st.input.Blur()
	if !st.active() {
		m.clearSearch()
	}
}

// cancelSearch puts the board back as it was before the input opened — query,
// scope and the card under the cursor. Restoring the query alone is not enough:
// live filtering clamps the cursor while you type, so the board would come back
// with a different card selected despite nothing having been committed.
func (m *Model) cancelSearch() {
	st := m.activeSearch()
	st.open = false
	st.input.Blur()
	m.setQuery(st.prevQuery)
	if st.global != st.prevGlobal {
		st.global = st.prevGlobal
		m.loadActiveForeign()
	}
	if st.prevTicket != "" {
		m.focusActiveTicket(st.prevTicket)
	}
	m.refreshActiveSelection()
}

// clearSearch drops the filter and the scope together. Scope is part of the
// filter, not a separate mode: leaving it on after a clear would keep other
// boards' cards on screen with nothing left to explain why.
func (m *Model) clearSearch() {
	m.resetSearch(m.activeSearch())
	m.loadActiveForeign()
	m.refreshActiveSelection()
}

// resetSearch puts one filter back to nothing — query, scope, completion and
// the borrowed rows it had pulled in. Separate from clearSearch because a
// board switch has to reset both filters, not just the one whose surface
// happens to be on screen.
func (m *Model) resetSearch(st *searchState) {
	st.open = false
	st.input.Blur()
	st.input.SetValue("")
	st.query = ""
	st.parsed = model.ParseQuery("")
	st.tagIdx = 0
	st.global = false
	st.foreign = nil
	st.owners = nil
}

// refreshDetailIfOpen re-seeds the detail editors from whatever the cursor is
// on now. Every cursor mover in the split and detail views does this; the
// filter is one too, because narrowing a column slides a different card under
// a stationary cursor. Skipping it leaves editTicketID on the card that
// scrolled away, and the next save renames a ticket that is no longer on
// screen — the pane itself renders from selectedTicket, so nothing looks wrong
// until the write lands.
func (m *Model) refreshDetailIfOpen() {
	if m.view == splitView || m.view == detailView {
		m.refreshDetailEditors()
	}
}

func (m *Model) refreshSearchSelection() {
	m.clampCursors()
	m.refreshDetailIfOpen()
}

func (m *Model) setQuery(q string) {
	st := m.activeSearch()
	st.query = q
	st.parsed = model.ParseQuery(q)
}

func (m *Model) toggleSearchScope() {
	st := m.activeSearch()
	st.global = !st.global
	m.loadActiveForeign()
	st.tagIdx = 0
	m.refreshActiveSelection()
}

// loadActiveForeign reloads borrowed rows for whichever surface is on screen.
func (m *Model) loadActiveForeign() {
	if m.view == archiveView {
		m.loadForeignArchive()
		return
	}
	m.loadForeign()
}

// focusActiveTicket puts the surface's cursor back on a ticket by id.
func (m *Model) focusActiveTicket(id string) {
	if m.view == archiveView {
		m.focusArchiveTicket(id)
		return
	}
	m.focusTicket(id)
}

// scopeToggleLabel names where ctrl+g would take the search, not where it is —
// a hint is only useful if it says what the key does next.
func (m *Model) scopeToggleLabel() string {
	if m.activeSearch().global {
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
	m.refreshDetailIfOpen()
	m.notice = "on " + boardDisplayName(owner)
	return true
}

// focusTicket puts the cursor on a card by id — silently doing nothing if the
// filter leaves it off screen, which is why callers that care check first.
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
	st := m.activeSearch()
	value := st.input.Value()
	if st.input.Position() != len([]rune(value)) {
		return "", "", false
	}
	tokens, openQuote := model.Tokenize(value)
	if len(tokens) == 0 {
		return "", "", false
	}
	// A trailing space ended the last term, so it is no longer being typed —
	// unless a quote is still open, where the space is part of the tag.
	// Compare trimmed against the whole string rather than a byte index against
	// len-1: multibyte whitespace (U+00A0 among others) survives the textinput
	// sanitiser, and its start byte is never its last one, so an index test
	// read a finished term as still being typed.
	if !openQuote && strings.TrimRightFunc(value, unicode.IsSpace) != value {
		return "", "", false
	}
	last := tokens[len(tokens)-1]
	if !last.Tagged {
		return "", "", false
	}
	// A negated term is not completed. The count beside a candidate is the
	// number of cards accepting it leaves you with, and under negation that
	// number describes the complement — so either it stops meaning what it
	// means everywhere else, or the list has to explain itself. `-#cli` is
	// still typed out in full; it is the completion that is declined, not the
	// query (Leon, 2026-08-03).
	if last.Negated {
		return "", "", false
	}
	return last.Text, value[:last.Start], true
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
	return model.TagCandidates(context.MatchAll(m.activePool()), prefix)
}

func (m *Model) acceptTagCompletion() bool {
	_, before, ok := m.pendingTag()
	if !ok {
		return false
	}
	cands := m.tagCandidates()
	if len(cands) == 0 {
		return false
	}
	st := m.activeSearch()
	st.input.SetValue(before + model.QuoteTag(cands[clampIndex(st.tagIdx, len(cands))].Tag) + " ")
	st.input.CursorEnd()
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
	st := m.activeSearch()
	st.tagIdx = ((clampIndex(st.tagIdx, n)+dir)%n + n) % n
}

// ─── Keys ────────────────────────────────────────────────────────────

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
	st := m.activeSearch()
	st.input, cmd = st.input.Update(msg)
	m.syncQuery()
	return m, cmd
}

// syncQuery re-reads the input after an edit. The cursor is clamped here
// rather than at the render, because a narrowing column can strand it past the
// end of a list that still has to answer selectedTicket correctly.
func (m *Model) syncQuery() {
	st := m.activeSearch()
	m.setQuery(st.input.Value())
	st.tagIdx = 0
	m.refreshActiveSelection()
}

// ─── Footer ──────────────────────────────────────────────────────────

// searchInputWidth is how much of the footer the query gets. A third of the
// terminal, bounded: too narrow and a two-term query scrolls out of sight,
// too wide and the match count it exists to explain gets pushed off the end.
func searchInputWidth(total int) int {
	return min(max(total/3, 12), 40)
}

// searchFooter replaces the hint line while the input is open: the query, then
// tag completions, the match count, and whatever hints still fit. There is one
// line to share, so the pieces compete for it and the least useful drop first.
func (m *Model) searchFooter(badge string) string {
	st := m.activeSearch()
	st.input.Width = searchInputWidth(m.width)
	input := helpStyle.Render(st.input.View())

	budget := m.width - lipgloss.Width(badge) - lipgloss.Width(input) - 2
	shown, total := m.activeCounts()
	count := fmt.Sprintf("%d/%d", shown, total)
	hints := fmt.Sprintf("%s | ^g %s | tab tag | esc cancel", count, m.scopeToggleLabel())

	// Completions get whatever is left once the count and the way out are
	// safe — those two are the line's floor, and fitHints protects the last
	// hint by construction. Giving the strip a fixed share instead silently
	// dropped candidates while hint text it outranks sat next to it.
	floor := count + " | esc cancel"
	right := fitHints(hints, budget)
	if strip := m.completionStrip(budget - lipgloss.Width(floor) - 2); strip != "" {
		right = strip + "  " + fitHints(hints, budget-lipgloss.Width(strip)-2)
	}
	return m.renderFooter(badge, input, helpStyle.Render(right))
}

// completionStrip lists the tag completions that fit, marking the one tab
// would take.
func (m *Model) completionStrip(budget int) string {
	cands := m.tagCandidates()
	if len(cands) == 0 || budget < 6 {
		return ""
	}
	idx := clampIndex(m.activeSearch().tagIdx, len(cands))

	render := func(start int) (string, int) {
		var parts []string
		used, last := 0, start-1
		for i := start; i < len(cands); i++ {
			// Render the term tab would write, quotes and all, so the strip and
			// the input can't disagree about what a candidate is called.
			text := fmt.Sprintf("%s %d", model.QuoteTag(cands[i].Tag), cands[i].Count)
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

// filterBadge is the active filter as it reads beside the board's name: the
// tag for a single-tag query, the query itself otherwise, and nothing at all
// when the board is showing everything.
//
// It is capped rather than wrapped or scrolled — the badge shares one line
// with the board name and the id prefix, and a long query is still recognisable
// from its first few terms.
// maxFilterBadge is the widest the filter chip ever gets, before the terminal
// itself lowers it further.
const maxFilterBadge = 24

// filterBadgeVisible reports whether the chip is on the footer right now. It
// is off while the input is open, where the input itself shows the query.
func (m *Model) filterBadgeVisible() bool {
	return !m.activeSearch().open && m.filterBadge() != ""
}

func (m *Model) filterBadge() string {
	st := m.activeSearch()
	if !st.active() {
		return ""
	}
	label := st.query
	switch {
	case label == model.Untagged:
		label = "no tags"
	case label == "":
		// Scope alone, with no query — the board is wider, not narrower.
		label = "all boards"
	case st.global:
		label += " · all boards"
	}

	// Bound against the terminal, and truncate only once the scope suffix is
	// on. Capping the query first and appending " · all boards" afterwards
	// made the one piece that was supposed to be bounded the piece that
	// overflowed — 38 columns from a stated cap of 24, on a terminal whose
	// supported minimum is 50.
	cap := min(maxFilterBadge, max(m.width/3, 8))
	if lipgloss.Width(label) > cap {
		label = ansi.Truncate(label, cap, "…")
	}
	// The board name is already padded on both sides; one trailing space here
	// separates the filter from the id prefix that follows it.
	return label + " "
}

// searchCountLabel is the footer's "how much of the board is this" — the
// query itself lives beside the board name, so this carries only the count.
func (m *Model) searchCountLabel() string {
	shown, total := m.activeCounts()
	return fmt.Sprintf("%d of %d", shown, total)
}

// ─── The archive browser's filter ────────────────────────────────────

// The archive is the second surface, and the one dfd36a pencilled first. It
// reuses the query language, the input, the completion strip and the footer
// chip; only the list underneath differs — a flat run of date headers and
// tickets rather than five columns.

// visibleArchiveEntries is the archive's single answer to "what is in this
// list right now", and every cursor, render and action path reads through it.
// archiveCursor indexes what is on screen while the store indexes what exists,
// which is the same trap the board's visibleTickets exists to close.
//
// A date header survives only if something under it did. Filtering headers
// alongside tickets would leave a day heading a group with nothing in it, and
// dropping them entirely would lose the one thing the archive is sorted by.
func (m *Model) visibleArchiveEntries() []archiveEntry {
	if !m.archiveSearch.active() {
		return m.archiveEntries
	}
	out := make([]archiveEntry, 0, len(m.archiveEntries))
	var pendingHeader archiveEntry
	for _, e := range m.archiveEntries {
		if e.isHeader {
			pendingHeader = e
			continue
		}
		if m.archiveSearch.parsed.Match(e.ticket) {
			if pendingHeader.isHeader {
				out = append(out, pendingHeader)
				pendingHeader = archiveEntry{}
			}
			out = append(out, e)
		}
	}
	return out
}

// archivePool is every archived ticket the current scope can reach.
func (m *Model) archivePool() []model.Ticket {
	out := make([]model.Ticket, 0, len(m.archiveEntries))
	for _, e := range m.archiveEntries {
		if !e.isHeader {
			out = append(out, e.ticket)
		}
	}
	return out
}

func (m *Model) archiveCounts() (shown, total int) {
	return countArchiveTickets(m.visibleArchiveEntries()), countArchiveTickets(m.archiveEntries)
}

// clampArchiveCursor puts the cursor back on a ticket that is actually on
// screen, after a filter narrowed the list under it.
func (m *Model) clampArchiveCursor() {
	entries := m.visibleArchiveEntries()
	if len(entries) == 0 {
		m.archiveCursor = 0
		return
	}
	if m.archiveCursor >= len(entries) {
		m.archiveCursor = len(entries) - 1
	}
	if m.archiveCursor < 0 {
		m.archiveCursor = 0
	}
	// Never rest on a date header: it has no ticket, so every action would
	// have to special-case it.
	for m.archiveCursor < len(entries) && entries[m.archiveCursor].isHeader {
		m.archiveCursor++
	}
	if m.archiveCursor >= len(entries) {
		m.archiveCursor = firstTicketIdx(entries)
	}
}

// focusArchiveTicket puts the cursor back on a ticket by id, for the esc that
// restores what the search was standing on.
func (m *Model) focusArchiveTicket(id string) {
	for i, e := range m.visibleArchiveEntries() {
		if !e.isHeader && e.ticket.ID == id {
			m.archiveCursor = i
			return
		}
	}
}

// loadForeignArchive reads every other active board's archive for global
// scope, the way loadForeign reads their boards. Archived sprints stay out for
// the same reason they do there.
//
// The borrowed rows are merged into archiveEntries rather than kept beside
// them: the archive is one list sorted by date, so a card from another board
// belongs at its own date rather than in a clump at the end.
func (m *Model) loadForeignArchive() bool {
	// Read before discarding anything. Clearing owners first and then failing
	// left the previous rows on screen with no ownership: a borrowed one lost
	// its badge, enter stopped following it home, and the unarchive guard that
	// keeps this board from writing another board's card was bypassed.
	local, err := m.store.LoadArchive()
	if err != nil {
		m.err = err
		return false
	}
	m.archiveSearch.foreign = nil
	m.archiveSearch.owners = nil
	tickets := local.Tickets

	if m.archiveSearch.global {
		entries, err := loadPickerEntries(false)
		if err != nil {
			m.notice = "could not read the other archives: " + err.Error()
			m.archiveSearch.global = false
		} else {
			owners := map[string]string{}
			for _, e := range entries {
				if e.name == m.sprintName {
					continue
				}
				s, err := boardStore(e.name)
				if err != nil {
					continue
				}
				arch, err := s.LoadArchive()
				if err != nil {
					continue
				}
				for _, t := range arch.Tickets {
					owners[t.ID] = e.name
					m.archiveSearch.foreign = append(m.archiveSearch.foreign, t)
					tickets = append(tickets, t)
				}
			}
			m.archiveSearch.owners = owners
		}
	}

	m.archiveEntries = buildArchiveEntries(tickets)
	m.clampArchiveCursor()
	return true
}

// jumpToForeignArchive follows a borrowed archive row home, landing in that
// board's archive with the query still applied — the board search's enter,
// pointed at the other list.
func (m *Model) jumpToForeignArchive() bool {
	t := m.archiveSelected()
	if t == nil {
		return false
	}
	owner, ok := m.ticketOwner(t.ID)
	if !ok {
		return false
	}

	id, query := t.ID, m.archiveSearch.query
	if err := m.switchBoard(owner); err != nil {
		m.notice = err.Error()
		return true
	}
	// switchBoard lands on the board; the row was in an archive, so follow it
	// into that board's archive rather than leaving the user somewhere else.
	m.archiveSearch.query = query
	m.archiveSearch.parsed = model.ParseQuery(query)
	m.archiveSearch.input.SetValue(query)
	m.enterArchive()
	m.focusArchiveTicket(id)
	return true
}
