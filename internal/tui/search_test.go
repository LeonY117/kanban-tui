package tui

import (
	"strings"
	"testing"

	"github.com/LeonY117/kanban-tui/internal/model"
	"github.com/LeonY117/kanban-tui/internal/store"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// searchKeys feeds keys through the top-level Update, so the search input has
// to actually be reachable from the board rather than merely callable.
func searchKeys(m *Model, keys ...string) {
	named := map[string]tea.KeyType{
		"/": tea.KeyRunes, "enter": tea.KeyEnter, "esc": tea.KeyEsc,
		"tab": tea.KeyTab, "ctrl+g": tea.KeyCtrlG, "ctrl+n": tea.KeyCtrlN,
		"backspace": tea.KeyBackspace, "down": tea.KeyDown, "up": tea.KeyUp,
	}
	for _, k := range keys {
		msg := tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(k)}
		if t, ok := named[k]; ok && t != tea.KeyRunes {
			msg = tea.KeyMsg{Type: t}
		}
		m.Update(msg)
	}
}

// typeSearch opens the input and types a query one rune at a time.
func typeSearch(m *Model, query string) {
	searchKeys(m, "/")
	for _, r := range query {
		searchKeys(m, string(r))
	}
}

// boardWith adds tickets to a store, each as "title|status|tags" with tags
// comma-separated, and returns a Model over it.
func boardWith(t *testing.T, specs ...string) (*Model, *store.Store) {
	t.Helper()
	sandboxRoot(t)
	s := store.New("")
	for _, spec := range specs {
		parts := strings.Split(spec, "|")
		status := model.StatusTodo
		if len(parts) > 1 && parts[1] != "" {
			parsed, err := model.ParseStatus(parts[1])
			if err != nil {
				t.Fatalf("status %q: %v", parts[1], err)
			}
			status = parsed
		}
		var tags []string
		if len(parts) > 2 && parts[2] != "" {
			tags = strings.Split(parts[2], ",")
		}
		if _, err := s.Add(parts[0], "", status, tags, "", "test"); err != nil {
			t.Fatalf("add %q: %v", spec, err)
		}
	}
	m, err := NewModel(s, "")
	if err != nil {
		t.Fatalf("new model: %v", err)
	}
	m.width, m.height, m.ready = 160, 40, true
	return m, s
}

func titlesOf(tickets []model.Ticket) []string {
	out := make([]string, len(tickets))
	for i, t := range tickets {
		out[i] = t.Title
	}
	return out
}

func TestSearchNarrowsTheBoardInPlace(t *testing.T) {
	m, _ := boardWith(t,
		"auth refresh|TODO", "billing page|TODO", "auth logout|DOING")

	typeSearch(m, "auth")

	if got := titlesOf(m.visibleTickets(model.StatusTodo)); len(got) != 1 || got[0] != "auth refresh" {
		t.Errorf("Todo = %v, want just the matching card", got)
	}
	if got := titlesOf(m.visibleTickets(model.StatusDoing)); len(got) != 1 {
		t.Errorf("Doing = %v, want the match to survive in its own column", got)
	}
	// Filtering in place means the columns stay: the board is narrowed, not
	// replaced by a result list.
	if m.view != boardView {
		t.Errorf("view = %v, want the board", m.view)
	}
	if shown, total := m.searchCounts(); shown != 2 || total != 3 {
		t.Errorf("counts = %d of %d, want 2 of 3", shown, total)
	}
}

func TestFilterOutlivesTheInput(t *testing.T) {
	m, _ := boardWith(t, "auth refresh|TODO", "billing page|TODO")

	typeSearch(m, "auth")
	searchKeys(m, "enter")

	if m.search.open {
		t.Error("enter left the input open")
	}
	if !m.searchActive() {
		t.Fatal("enter cleared the filter — narrowing in place is pointless if you can't then move around what's left")
	}
	if got := titlesOf(m.visibleTickets(model.StatusTodo)); len(got) != 1 {
		t.Errorf("Todo = %v, want the filter still applied", got)
	}
	// And the footer says why the board looks half empty.
	if chip := m.searchChip(); !strings.Contains(chip, "auth") || !strings.Contains(chip, "1 of 2") {
		t.Errorf("chip = %q, want the query and the count", chip)
	}
}

func TestEscOnTheBoardClearsTheFilter(t *testing.T) {
	m, _ := boardWith(t, "auth refresh|TODO", "billing page|TODO")

	typeSearch(m, "auth")
	searchKeys(m, "enter", "esc")

	if m.searchActive() {
		t.Error("esc left the board filtered")
	}
	if got := m.visibleTickets(model.StatusTodo); len(got) != 2 {
		t.Errorf("Todo = %v, want the whole column back", titlesOf(got))
	}
}

func TestCancellingRestoresThePreviousFilter(t *testing.T) {
	m, _ := boardWith(t, "auth refresh|TODO", "billing page|TODO")

	typeSearch(m, "auth")
	searchKeys(m, "enter")

	// Reopen, type something else, then think better of it.
	typeSearch(m, "zzz")
	if got := m.visibleTickets(model.StatusTodo); len(got) != 0 {
		t.Fatalf("Todo = %v, want the live edit to already be filtering", titlesOf(got))
	}
	searchKeys(m, "esc")

	if m.search.query != "auth" {
		t.Errorf("query = %q, want the committed filter back", m.search.query)
	}
	if got := titlesOf(m.visibleTickets(model.StatusTodo)); len(got) != 1 || got[0] != "auth refresh" {
		t.Errorf("Todo = %v, want the pre-edit filter restored", got)
	}
}

func TestReopeningPrefillsTheQuery(t *testing.T) {
	m, _ := boardWith(t, "auth refresh|TODO")

	typeSearch(m, "auth")
	searchKeys(m, "enter", "/")

	if got := m.search.input.Value(); got != "auth" {
		t.Errorf("input = %q, want it pre-filled so refining isn't retyping", got)
	}
	if pos, want := m.search.input.Position(), len("auth"); pos != want {
		t.Errorf("cursor at %d, want %d — the end, like the sprint rename", pos, want)
	}
}

// ─── The cursor drift a filtered board invites ───────────────────────

func TestCursorCannotSelectAHiddenCard(t *testing.T) {
	// The match sits at index 1 of the unfiltered column, so reading the
	// board directly would select "billing" — clamping the cursor to 0 is not
	// enough on its own.
	m, _ := boardWith(t, "billing|TODO", "auth one|TODO", "billing two|TODO")

	m.cursors[1] = 2 // on "billing two"
	typeSearch(m, "auth")

	sel := m.selectedTicket()
	if sel == nil {
		t.Fatal("nothing selected after the column narrowed")
	}
	if sel.Title != "auth one" {
		t.Errorf("selected %q, want the only visible card", sel.Title)
	}
}

func TestReorderSwapsTheCardsYouCanSee(t *testing.T) {
	// Visible A and C, with hidden B between them. J on A must swap A with C
	// — the neighbour on screen — not with the B nobody can see.
	m, s := boardWith(t, "auth A|TODO", "hidden B|TODO", "auth C|TODO")

	typeSearch(m, "auth")
	searchKeys(m, "enter")
	m.cursors[1] = 0

	m.moveTicketInColumn(1)

	board, err := s.Load()
	if err != nil {
		t.Fatal(err)
	}
	got := titlesOf(board.ByStatus(model.StatusTodo))
	want := []string{"auth C", "hidden B", "auth A"}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("board order = %v, want %v — reading the unfiltered column would have swapped auth A with hidden B", got, want)
		}
	}
	if sel := m.selectedTicket(); sel == nil || sel.Title != "auth A" {
		t.Errorf("cursor left the card it moved: %v", sel)
	}
}

func TestMoveFollowsTheCardPastHiddenNeighbours(t *testing.T) {
	// Doing holds a hidden card ahead of where the moved card lands, so the
	// unfiltered index is a valid — but wrong — visible index. That is the
	// case clampCursors cannot mask.
	m, _ := boardWith(t, "hidden X|DOING", "auth A|TODO", "auth Y|DOING")

	typeSearch(m, "auth")
	searchKeys(m, "enter")
	m.focusedCol, m.cursors[1] = 1, 0

	m.moveTicket(1) // Todo → Doing

	if m.focusedCol != 2 {
		t.Fatalf("focus = %d, want the Doing column", m.focusedCol)
	}
	sel := m.selectedTicket()
	if sel == nil || sel.Title != "auth A" {
		t.Errorf("selected %v, want the card that just moved", sel)
	}
}

func TestMovePopupFollowsTheCardPastHiddenNeighbours(t *testing.T) {
	// Same trap as the H/L move, down the other code path: the move popup
	// commits through commitMove, which re-finds the card to put the cursor
	// on it.
	m, _ := boardWith(t, "hidden X|DOING", "auth A|TODO", "auth Y|DOING")

	typeSearch(m, "auth")
	searchKeys(m, "enter")
	m.focusedCol, m.cursors[1] = 1, 0

	m.enterMovePopup()
	m.moveIdx = 2 // Doing
	m.moveActivate()

	if m.focusedCol != 2 {
		t.Fatalf("focus = %d, want the Doing column", m.focusedCol)
	}
	sel := m.selectedTicket()
	if sel == nil || sel.Title != "auth A" {
		t.Errorf("selected %v, want the card the popup just moved", sel)
	}
}

func TestCursorStopsAtTheEndOfTheVisibleColumn(t *testing.T) {
	m, _ := boardWith(t, "auth one|TODO", "billing|TODO", "billing two|TODO")

	typeSearch(m, "auth")
	searchKeys(m, "enter")

	// Two presses of j against a one-card column.
	m.moveCursor(1)
	m.moveCursor(1)

	if got := m.cursors[1]; got != 0 {
		t.Errorf("cursor at %d, want 0 — j walked off the end of what's on screen", got)
	}
	if sel := m.selectedTicket(); sel == nil || sel.Title != "auth one" {
		t.Errorf("selected %v, want the only visible card", sel)
	}
}

// surfaces are the views that draw a column of cards. Each one renders and
// scrolls through its own code path, so the filter has to reach all of them.
var surfaces = []struct {
	name  string
	enter func(m *Model)
}{
	{"board", func(m *Model) {}},
	{"rows", func(m *Model) { m.rowLayout = true }},
	{"split", func(m *Model) { m.enterSplit() }},
	{"column", func(m *Model) { m.enterSplit(); m.view = columnView }},
}

func TestHiddenCardsAreNotRenderedOnAnySurface(t *testing.T) {
	for _, s := range surfaces {
		t.Run(s.name, func(t *testing.T) {
			m, _ := boardWith(t, "billing page|TODO", "auth refresh|TODO")
			typeSearch(m, "auth")
			searchKeys(m, "enter")
			s.enter(m)

			view := m.View()
			if !strings.Contains(view, "auth refresh") {
				t.Error("the matching card is missing from the render")
			}
			if strings.Contains(view, "billing page") {
				t.Error("a filtered-out card is still drawn")
			}
		})
	}
}

func TestCursorStopsAtTheVisibleEndOnAnySurface(t *testing.T) {
	for _, s := range surfaces {
		t.Run(s.name, func(t *testing.T) {
			m, _ := boardWith(t, "auth one|TODO", "billing|TODO", "billing two|TODO")
			typeSearch(m, "auth")
			searchKeys(m, "enter")
			s.enter(m)

			searchKeys(m, "down", "down")

			if got := m.cursors[1]; got != 0 {
				t.Errorf("cursor at %d, want 0 — j walked off the end of what's on screen", got)
			}
			if sel := m.selectedTicket(); sel == nil || sel.Title != "auth one" {
				t.Errorf("selected %v, want the only visible card", sel)
			}
		})
	}
}

func TestNewCardIsFollowedPastHiddenNeighbours(t *testing.T) {
	// "billing" sits ahead of the match, so the new card's index in the
	// column and its index on screen differ.
	m, _ := boardWith(t, "billing page|TODO", "auth one|TODO")

	typeSearch(m, "auth")
	searchKeys(m, "enter")

	m.enterAddPopup()
	m.addTitle.SetValue("auth two")
	m.submitAdd()

	if sel := m.selectedTicket(); sel == nil || sel.Title != "auth two" {
		t.Errorf("selected %v, want the card just added", sel)
	}
}

func TestAddingAHiddenCardLeavesTheCursorAlone(t *testing.T) {
	m, _ := boardWith(t, "auth one|TODO", "auth two|TODO")

	typeSearch(m, "auth")
	searchKeys(m, "enter")
	m.cursors[1] = 1

	m.enterAddPopup()
	m.addTitle.SetValue("billing work")
	m.submitAdd()

	if got := m.cursors[1]; got != 1 {
		t.Errorf("cursor moved to %d — the new card isn't on screen to move to", got)
	}
}

// ─── Adding under a filter ───────────────────────────────────────────

func TestAddingACardTheFilterHidesSaysSo(t *testing.T) {
	m, s := boardWith(t, "auth one|TODO")

	typeSearch(m, "auth")
	searchKeys(m, "enter")

	m.enterAddPopup()
	m.addTitle.SetValue("billing work")
	m.submitAdd()

	board, err := s.Load()
	if err != nil {
		t.Fatal(err)
	}
	if len(board.Tickets) != 2 {
		t.Fatalf("board has %d cards, want the add to have saved anyway", len(board.Tickets))
	}
	if !strings.Contains(m.notice, "hiding it") {
		t.Errorf("notice = %q, want it to explain why the new card isn't on screen", m.notice)
	}
}

// ─── Tag completion ──────────────────────────────────────────────────

func TestBareHashOffersTheTagList(t *testing.T) {
	m, _ := boardWith(t, "a|TODO|cli", "b|TODO|cli", "c|TODO|ui")

	typeSearch(m, "#")

	cands := m.tagCandidates()
	if len(cands) != 2 {
		t.Fatalf("candidates = %+v, want the whole tag list", cands)
	}
	if cands[0].Tag != "cli" || cands[0].Count != 2 {
		t.Errorf("first = %+v, want cli with its count", cands[0])
	}
	if strip := m.completionStrip(40); !strings.Contains(strip, "cli") {
		t.Errorf("strip = %q, want the candidates rendered into the footer", strip)
	}
}

func TestTabAcceptsTheHighlightedTag(t *testing.T) {
	m, _ := boardWith(t, "a|TODO|cli", "b|TODO|ui")

	typeSearch(m, "#c")
	searchKeys(m, "tab")

	if got := m.search.input.Value(); got != "#cli " {
		t.Errorf("input = %q, want the completed tag", got)
	}
	if got := titlesOf(m.visibleTickets(model.StatusTodo)); len(got) != 1 || got[0] != "a" {
		t.Errorf("Todo = %v, want the board already filtered by the accepted tag", got)
	}
}

func TestCompletionCountsMatchWhatSelectingYields(t *testing.T) {
	m, _ := boardWith(t, "a|TODO|cli", "b|DONE|cli", "c|TODO|ui")

	typeSearch(m, "#cl")
	cands := m.tagCandidates()
	if len(cands) != 1 {
		t.Fatalf("candidates = %+v, want cli", cands)
	}
	offered := cands[0].Count

	searchKeys(m, "tab")
	shown, _ := m.searchCounts()
	if shown != offered {
		t.Errorf("completion offered %d cards, selecting it showed %d — including the done one", offered, shown)
	}
	if offered != 2 {
		t.Errorf("offered %d, want 2: a tag on a done card is still a tag", offered)
	}
}

func TestCompletionOnlyOffersAtTheEndOfTheInput(t *testing.T) {
	m, _ := boardWith(t, "a|TODO|cli")

	typeSearch(m, "#cl auth")
	m.search.input.SetCursor(2) // back inside the #cl term

	if cands := m.tagCandidates(); cands != nil {
		t.Errorf("candidates = %+v, want none — completing mid-line rewrites text the user isn't looking at", cands)
	}
}

func TestEarlierTermsNarrowTheCandidateCounts(t *testing.T) {
	m, _ := boardWith(t, "auth one|TODO|cli", "billing|TODO|cli")

	typeSearch(m, "auth #")

	cands := m.tagCandidates()
	if len(cands) != 1 || cands[0].Count != 1 {
		t.Errorf("candidates = %+v, want cli counted against the auth term only", cands)
	}
}

// ─── Global scope ────────────────────────────────────────────────────

// withSprint adds a sprint alongside the main board and fills it, using the
// same "title|status|tags" spec as boardWith.
func withSprint(t *testing.T, name string, specs ...string) *store.Store {
	t.Helper()
	if err := store.CreateSprint(name, ""); err != nil {
		t.Fatalf("create sprint %q: %v", name, err)
	}
	s, err := store.NewSprint(name)
	if err != nil {
		t.Fatalf("open sprint %q: %v", name, err)
	}
	for _, spec := range specs {
		parts := strings.Split(spec, "|")
		status := model.StatusTodo
		if len(parts) > 1 && parts[1] != "" {
			parsed, err := model.ParseStatus(parts[1])
			if err != nil {
				t.Fatalf("status %q: %v", parts[1], err)
			}
			status = parsed
		}
		var tags []string
		if len(parts) > 2 && parts[2] != "" {
			tags = strings.Split(parts[2], ",")
		}
		if _, err := s.Add(parts[0], "", status, tags, "", "test"); err != nil {
			t.Fatalf("add %q: %v", spec, err)
		}
	}
	return s
}

func TestGlobalScopeBorrowsOtherBoardsCards(t *testing.T) {
	m, _ := boardWith(t, "auth local|TODO")
	withSprint(t, "demo", "auth remote|TODO", "billing remote|TODO")

	typeSearch(m, "auth")
	searchKeys(m, "ctrl+g")

	got := titlesOf(m.visibleTickets(model.StatusTodo))
	if len(got) != 2 || got[0] != "auth local" || got[1] != "auth remote" {
		t.Fatalf("Todo = %v, want the local match first then the borrowed one", got)
	}
	// The badge is what stops a borrowed card reading as one of this board's.
	if view := m.View(); !strings.Contains(view, "demo/") {
		t.Error("no board badge on the borrowed card")
	}
	if _, ok := m.ticketOwner(m.visibleTickets(model.StatusTodo)[0].ID); ok {
		t.Error("the local card was reported as borrowed")
	}
}

func TestBorrowedCardsAreReadOnly(t *testing.T) {
	m, _ := boardWith(t, "auth local|TODO")
	withSprint(t, "demo", "auth remote|TODO")

	typeSearch(m, "auth")
	searchKeys(m, "ctrl+g", "enter")
	m.cursors[1] = 1 // the borrowed card

	before := m.board.Tickets[0].Status
	m.moveTicket(1)

	if !strings.Contains(m.notice, "demo") {
		t.Errorf("notice = %q, want it to name the board the card lives on", m.notice)
	}
	if m.board.Tickets[0].Status != before {
		t.Error("a write aimed at a borrowed card landed on this board instead")
	}
	if m.focusedCol != 1 {
		t.Errorf("focus moved to %d — the refused move still walked the cursor", m.focusedCol)
	}
}

func TestAddingStillWorksWithABorrowedCardSelected(t *testing.T) {
	// The add targets the board, not the selection, so a borrowed card under
	// the cursor must not block it.
	m, _ := boardWith(t, "auth local|TODO")
	withSprint(t, "demo", "auth remote|TODO")

	typeSearch(m, "auth")
	searchKeys(m, "ctrl+g", "enter")
	m.cursors[1] = 1

	if _, _ = m.enterAddPopup(); m.view != addView {
		t.Fatalf("view = %v, want the add popup to have opened", m.view)
	}
}

func TestEnterFollowsABorrowedCardHome(t *testing.T) {
	// "hidden" sits ahead of the target in the sprint's column, so the card's
	// index there differs from its index on screen after the jump.
	m, _ := boardWith(t, "auth local|TODO")
	withSprint(t, "demo", "hidden one|TODO", "auth target|TODO", "auth tail|TODO")

	typeSearch(m, "auth")
	searchKeys(m, "ctrl+g", "enter")

	// Land on the borrowed "auth target": local card first, then the sprint's.
	m.cursors[1] = 1
	if sel := m.selectedTicket(); sel == nil || sel.Title != "auth target" {
		t.Fatalf("selected %v, want auth target", sel)
	}

	searchKeys(m, "enter")

	if m.sprintName != "demo" {
		t.Fatalf("still on %q, want the sprint the card lives on", m.sprintName)
	}
	if m.search.query != "auth" {
		t.Errorf("query = %q, want it carried across the jump", m.search.query)
	}
	if m.search.global {
		t.Error("still searching every board after landing on the one that owns the card")
	}
	if sel := m.selectedTicket(); sel == nil || sel.Title != "auth target" {
		t.Errorf("selected %v, want the cursor on the card that was followed", sel)
	}
	// And it is editable now that we are standing on its board.
	if !m.guardMutate() {
		t.Errorf("the card is still read-only on its own board: %q", m.notice)
	}
}

func TestClearingDropsTheScopeToo(t *testing.T) {
	m, _ := boardWith(t, "auth local|TODO")
	withSprint(t, "demo", "auth remote|TODO")

	typeSearch(m, "auth")
	searchKeys(m, "ctrl+g", "enter", "esc")

	if m.search.global {
		t.Error("esc left the search scoped to every board")
	}
	if got := titlesOf(m.visibleTickets(model.StatusTodo)); len(got) != 1 {
		t.Errorf("Todo = %v, want only this board's cards back", got)
	}
}

func TestSwitchingBoardDropsTheFilter(t *testing.T) {
	m, _ := boardWith(t, "auth local|TODO")
	withSprint(t, "demo", "billing remote|TODO")

	typeSearch(m, "auth")
	searchKeys(m, "enter")

	if err := m.switchBoard("demo"); err != nil {
		t.Fatal(err)
	}
	if m.searchActive() {
		t.Error("the filter followed the board switch, hiding the new board's cards for no visible reason")
	}
	if got := titlesOf(m.visibleTickets(model.StatusTodo)); len(got) != 1 {
		t.Errorf("Todo = %v, want the sprint's own card", got)
	}
}

func TestGlobalScopeSkipsArchivedSprints(t *testing.T) {
	m, _ := boardWith(t, "auth local|TODO")
	withSprint(t, "demo", "auth remote|TODO")
	if err := store.ArchiveSprint("demo"); err != nil {
		t.Fatalf("archive sprint: %v", err)
	}

	typeSearch(m, "auth")
	searchKeys(m, "ctrl+g")

	if got := titlesOf(m.visibleTickets(model.StatusTodo)); len(got) != 1 {
		t.Errorf("Todo = %v, want an archived sprint's cards left out", got)
	}
}

// ─── Footer geometry ─────────────────────────────────────────────────

func TestFooterNeverOverflowsTheTerminal(t *testing.T) {
	m, _ := boardWith(t, "auth refresh|TODO|customer,cli", "billing|TODO|cli")

	for _, width := range []int{50, 60, 80, 120, 160} {
		m.width = width
		for _, stage := range []string{"typing", "committed"} {
			typeSearch(m, "auth #c")
			if stage == "committed" {
				searchKeys(m, "enter")
			}
			for _, line := range strings.Split(m.View(), "\n") {
				if got := lipgloss.Width(line); got > width {
					t.Errorf("width %d, %s: a line rendered %d cells", width, stage, got)
					break
				}
			}
			m.clearSearch()
		}
	}
}

// ─── Regressions found by the ship review ────────────────────────────

func TestArchiveUnarchiveIgnoresTheBoardSelection(t *testing.T) {
	// The archive browser has its own cursor over its own entries, which are
	// always local. Consulting the board selection refused an unarchive over
	// a card that was not even on screen.
	m, s := boardWith(t, "auth local|TODO", "old thing|TODO")
	withSprint(t, "demo", "auth remote|TODO")

	board, _ := s.Load()
	var oldID string
	for _, tk := range board.Tickets {
		if tk.Title == "old thing" {
			oldID = tk.ID
		}
	}
	if err := s.ArchiveByID(oldID); err != nil {
		t.Fatal(err)
	}
	m.reload()

	typeSearch(m, "auth")
	searchKeys(m, "ctrl+g", "enter")
	m.cursors[1] = 1 // a borrowed card sits under the board cursor
	if _, ok := m.ticketOwner(m.selectedTicket().ID); !ok {
		t.Fatal("setup: expected a borrowed card under the cursor")
	}

	m.enterArchive()
	for i, e := range m.archiveEntries {
		if !e.isHeader {
			m.archiveCursor = i
			break
		}
	}
	m.unarchiveSelected()

	arch, err := s.LoadArchive()
	if err != nil {
		t.Fatal(err)
	}
	if len(arch.Tickets) != 0 {
		t.Errorf("unarchive was refused over an unrelated selection: %q", m.notice)
	}
}

func TestReorderRefusesABorrowedNeighbour(t *testing.T) {
	// The swap rewrites this board's ticket order, so it stops where the board
	// does. Silently no-oping while still advancing the cursor looked exactly
	// like a successful move.
	m, s := boardWith(t, "auth local|TODO")
	withSprint(t, "demo", "auth remote|TODO")

	typeSearch(m, "auth")
	searchKeys(m, "ctrl+g", "enter")
	m.cursors[1] = 0

	m.moveTicketInColumn(1)

	if m.cursors[1] != 0 {
		t.Errorf("cursor advanced to %d though nothing moved", m.cursors[1])
	}
	if !strings.Contains(m.notice, "demo") {
		t.Errorf("notice = %q, want it to name the board reordering stops at", m.notice)
	}
	after, _ := s.Load()
	if got := titlesOf(after.ByStatus(model.StatusTodo)); len(got) != 1 || got[0] != "auth local" {
		t.Errorf("board order = %v, want it untouched", got)
	}
}

// editorsBoundTo names the ticket a save from the detail pane would write to.
func editorsBoundTo(t *testing.T, m *Model, s *store.Store) string {
	t.Helper()
	board, err := s.Load()
	if err != nil {
		t.Fatal(err)
	}
	for _, tk := range board.Tickets {
		if tk.ID == m.editTicketID {
			return tk.Title
		}
	}
	return "<none>"
}

func TestAddFromSplitDetailLeavesEditorsOnTheNewCard(t *testing.T) {
	// closeAddPopup re-seeds the editors from the cursor, so it has to run
	// after the cursor moves, not before.
	m, s := boardWith(t, "alpha one|TODO")
	m.enterSplit()
	m.splitFocus = 1
	m.refreshDetailEditors()

	m.enterAddPopup()
	m.addTitle.SetValue("beta two")
	m.submitAdd()

	sel := m.selectedTicket()
	if sel == nil || sel.Title != "beta two" {
		t.Fatalf("selected %v, want the new card", sel)
	}
	if got := editorsBoundTo(t, m, s); got != "beta two" {
		t.Errorf("pane shows %q but a save would write to %q", sel.Title, got)
	}
}

func TestFilteringInSplitViewRebindsTheEditors(t *testing.T) {
	// Narrowing a column slides a different card under a stationary cursor.
	// The pane renders from selectedTicket, so a stale editTicketID is
	// invisible until the write lands on the card that scrolled away.
	m, s := boardWith(t, "alpha one|TODO", "beta two|TODO")
	m.enterSplit()
	m.refreshDetailEditors()

	typeSearch(m, "beta")
	searchKeys(m, "enter")

	sel := m.selectedTicket()
	if sel == nil || sel.Title != "beta two" {
		t.Fatalf("selected %v, want the only match", sel)
	}
	if got := editorsBoundTo(t, m, s); got != "beta two" {
		t.Errorf("pane shows %q but a save would write to %q", sel.Title, got)
	}
}

func TestCancellingRestoresTheSelectedCard(t *testing.T) {
	m, _ := boardWith(t, "alpha one|TODO", "beta two|TODO", "gamma three|TODO")
	m.cursors[1] = 2 // on "gamma three"

	typeSearch(m, "alpha") // narrows to one card and clamps the cursor
	searchKeys(m, "esc")

	if sel := m.selectedTicket(); sel == nil || sel.Title != "gamma three" {
		t.Errorf("selected %v, want the card the cursor was on before search opened", sel)
	}
}

func TestOnlyEnterFollowsABorrowedCardHome(t *testing.T) {
	m, _ := boardWith(t, "auth local|TODO")
	withSprint(t, "demo", "auth remote|TODO")

	typeSearch(m, "auth")
	searchKeys(m, "ctrl+g", "enter")
	m.cursors[1] = 1

	// Zoom is off while a search is active, and must not jump either way.
	searchKeys(m, "+")
	if m.sprintName != "" {
		t.Errorf("zoom followed the card to %q; only enter should", m.sprintName)
	}
	if !strings.Contains(m.notice, "zoom is off") {
		t.Errorf("notice = %q, want zoom to say why it did nothing", m.notice)
	}

	searchKeys(m, "enter")
	if m.sprintName != "demo" {
		t.Errorf("enter did not follow the card home: on %q", m.sprintName)
	}
}

func TestArchiveBrowserKeepsItsOwnFooter(t *testing.T) {
	m, _ := boardWith(t, "auth refresh|TODO")

	typeSearch(m, "auth")
	searchKeys(m, "enter")
	m.enterArchive()
	m.width = 70

	footer := m.footerLine()
	if strings.Contains(footer, "1 of 1") {
		t.Error("the board's match count is captioning the archive list")
	}
	if !strings.Contains(footer, "back") {
		t.Errorf("archive footer lost its way out: %q", footer)
	}
}

func TestFilterChipSurvivesALongQuery(t *testing.T) {
	m, _ := boardWith(t, "auth refresh|TODO")
	m.width = 60

	typeSearch(m, "a very long search query indeed")
	searchKeys(m, "enter")

	footer := m.footerLine()
	if !strings.Contains(footer, "0 of 1") {
		t.Errorf("the chip was trimmed away, leaving no reason for the empty board: %q", footer)
	}
}

func TestCompletingAMultiWordTagQuotesIt(t *testing.T) {
	m, _ := boardWith(t, "a|TODO|needs review", "b|TODO|cli")

	typeSearch(m, "#nee")
	if strip := m.completionStrip(60); !strings.Contains(strip, `#"needs review"`) {
		t.Errorf("strip = %q, want the quoted form the completion will write", strip)
	}
	searchKeys(m, "tab")

	if got := m.search.input.Value(); got != `#"needs review" ` {
		t.Errorf("input = %q, want the tag quoted so it stays one term", got)
	}
	if got := titlesOf(m.visibleTickets(model.StatusTodo)); len(got) != 1 || got[0] != "a" {
		t.Errorf("Todo = %v, want only the card carrying the tag", got)
	}
}

func TestCompletionOffersInsideAnOpenQuote(t *testing.T) {
	// A space inside an open quote is part of the tag, not the end of a term,
	// so the candidate list has to survive it.
	m, _ := boardWith(t, "a|TODO|needs review")

	typeSearch(m, `#"needs r`)
	if cands := m.tagCandidates(); len(cands) != 1 || cands[0].Tag != "needs review" {
		t.Errorf("candidates = %+v, want the multi-word tag still offered mid-quote", cands)
	}
}

func TestCompletedTagStopsOfferingItself(t *testing.T) {
	// tab appends a trailing space, which ends the term — otherwise the strip
	// would keep offering a completion that is already in place.
	m, _ := boardWith(t, "a|TODO|cli")

	typeSearch(m, "#cl")
	searchKeys(m, "tab")

	if cands := m.tagCandidates(); cands != nil {
		t.Errorf("candidates = %+v, want none once the term is finished", cands)
	}
}
