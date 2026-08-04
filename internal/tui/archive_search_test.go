package tui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/LeonY117/kanban-tui/internal/model"
	"github.com/LeonY117/kanban-tui/internal/store"
	tea "github.com/charmbracelet/bubbletea"
)

// archiveWith builds a board, archives every ticket whose title starts with
// "arc:", and opens the archive browser on it.
func archiveWith(t *testing.T, specs ...string) (*Model, *store.Store) {
	t.Helper()
	m, s := boardWith(t, specs...)
	board, err := s.Load()
	if err != nil {
		t.Fatal(err)
	}
	for _, tk := range board.Tickets {
		if strings.HasPrefix(tk.Title, "arc:") {
			if err := s.ArchiveByID(tk.ID); err != nil {
				t.Fatal(err)
			}
		}
	}
	m.reload()
	m.enterArchive()
	return m, s
}

func archiveTitles(m *Model) []string {
	var out []string
	for _, e := range m.visibleArchiveEntries() {
		if !e.isHeader {
			out = append(out, e.ticket.Title)
		}
	}
	return out
}

func TestArchiveSearchNarrowsTheList(t *testing.T) {
	m, _ := archiveWith(t, "arc: auth refresh|TODO", "arc: billing page|TODO", "live one|TODO")

	if got := archiveTitles(m); len(got) != 2 {
		t.Fatalf("setup: archive = %v, want both archived cards", got)
	}

	typeSearch(m, "auth")

	if got := archiveTitles(m); len(got) != 1 || got[0] != "arc: auth refresh" {
		t.Errorf("archive = %v, want just the match", got)
	}
	if shown, total := m.archiveCounts(); shown != 1 || total != 2 {
		t.Errorf("counts = %d of %d, want 1 of 2", shown, total)
	}
	if m.view != archiveView {
		t.Errorf("view = %v, want to stay in the archive", m.view)
	}
}

func TestArchiveFilterIsSeparateFromTheBoardFilter(t *testing.T) {
	// Two lists, two meanings. A query typed over one must not silently
	// re-scope the other (Leon, 2026-08-04).
	m, _ := archiveWith(t, "arc: auth old|TODO", "arc: billing old|TODO", "auth live|TODO", "billing live|TODO")

	typeSearch(m, "auth")
	searchKeys(m, "enter")
	if got := archiveTitles(m); len(got) != 1 {
		t.Fatalf("archive = %v, want it filtered", got)
	}
	if m.search.active() {
		t.Error("filtering the archive also filtered the board")
	}

	// Leaving the archive keeps the board as it was.
	m.Update(tea.KeyMsg{Type: tea.KeyEsc}) // clears the archive filter
	m.Update(tea.KeyMsg{Type: tea.KeyEsc}) // leaves the browser
	if m.view != boardView {
		t.Fatalf("view = %v, want the board", m.view)
	}
	if got := titlesOf(m.visibleTickets(model.StatusTodo)); len(got) != 2 {
		t.Errorf("board = %v, want both live cards — the board was never filtered", got)
	}
}

func TestArchiveDateHeadersCollapseToMatches(t *testing.T) {
	// A header whose whole day filtered out must go with it; one that keeps
	// some of its group must stay, because the date is what the list is
	// sorted by.
	m, s := archiveWith(t, "arc: auth one|TODO", "arc: billing one|TODO")

	// Push the two cards onto different days.
	arch, err := s.LoadArchive()
	if err != nil {
		t.Fatal(err)
	}
	if len(arch.Tickets) != 2 {
		t.Fatalf("setup: %d archived, want 2", len(arch.Tickets))
	}
	older := time.Now().AddDate(0, 0, -3)
	for i := range arch.Tickets {
		if strings.Contains(arch.Tickets[i].Title, "billing") {
			arch.Tickets[i].ArchivedAt = &older
		}
	}
	m.archiveEntries = buildArchiveEntries(arch.Tickets)

	headers := func() int {
		n := 0
		for _, e := range m.visibleArchiveEntries() {
			if e.isHeader {
				n++
			}
		}
		return n
	}
	if headers() != 2 {
		t.Fatalf("setup: %d headers, want one per day", headers())
	}

	m.archiveSearch.query = "auth"
	m.archiveSearch.parsed = model.ParseQuery("auth")
	if got := headers(); got != 1 {
		t.Errorf("%d headers, want the emptied day dropped", got)
	}
	entries := m.visibleArchiveEntries()
	if len(entries) == 0 || !entries[0].isHeader {
		t.Error("the surviving group lost its date header")
	}
}

func TestArchiveCursorCannotSelectAHiddenEntry(t *testing.T) {
	m, _ := archiveWith(t, "arc: billing|TODO", "arc: auth one|TODO", "arc: billing two|TODO")

	// Park on the last row of the unfiltered list, then narrow past it.
	m.archiveCursor = len(m.visibleArchiveEntries()) - 1
	typeSearch(m, "auth")

	sel := m.archiveSelected()
	if sel == nil {
		t.Fatal("nothing selected after the list narrowed")
	}
	if !strings.Contains(sel.Title, "auth") {
		t.Errorf("selected %q, want the only visible card", sel.Title)
	}
}

func TestArchiveCursorNeverRestsOnADateHeader(t *testing.T) {
	m, _ := archiveWith(t, "arc: auth one|TODO", "arc: auth two|TODO")

	for i := 0; i < 6; i++ {
		m.Update(tea.KeyMsg{Type: tea.KeyDown})
		if e := m.visibleArchiveEntries(); m.archiveCursor < len(e) && e[m.archiveCursor].isHeader {
			t.Fatalf("cursor landed on a date header at %d", m.archiveCursor)
		}
	}
	for i := 0; i < 6; i++ {
		m.Update(tea.KeyMsg{Type: tea.KeyUp})
		if e := m.visibleArchiveEntries(); m.archiveCursor < len(e) && e[m.archiveCursor].isHeader {
			t.Fatalf("cursor landed on a date header at %d", m.archiveCursor)
		}
	}
}

func TestArchiveEscClearsBeforeItLeaves(t *testing.T) {
	m, _ := archiveWith(t, "arc: auth|TODO", "arc: billing|TODO")

	typeSearch(m, "auth")
	searchKeys(m, "enter")

	m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if m.view != archiveView {
		t.Fatalf("view = %v, want the first esc to clear rather than leave", m.view)
	}
	if m.archiveSearch.active() {
		t.Error("esc did not clear the filter")
	}
	m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if m.view != boardView {
		t.Errorf("view = %v, want the second esc to leave", m.view)
	}
}

func TestArchiveHiddenEntriesAreNotRendered(t *testing.T) {
	m, _ := archiveWith(t, "arc: auth refresh|TODO", "arc: billing page|TODO")

	typeSearch(m, "auth")
	searchKeys(m, "enter")

	view := m.View()
	if !strings.Contains(view, "auth refresh") {
		t.Error("the matching card is not on screen")
	}
	if strings.Contains(view, "billing page") {
		t.Error("a filtered-out card is still rendered")
	}
	if !strings.Contains(view, "1 of 2") {
		t.Errorf("footer does not carry the archive's own count:\n%s", view)
	}
}

func TestArchiveGlobalScopeBorrowsOtherArchives(t *testing.T) {
	m, _ := archiveWith(t, "arc: auth local|TODO")

	// A second board with its own archived card.
	other := withSprint(t, "demo", "auth remote|TODO")
	ob, err := other.Load()
	if err != nil {
		t.Fatal(err)
	}
	if err := other.ArchiveByID(ob.Tickets[0].ID); err != nil {
		t.Fatal(err)
	}

	typeSearch(m, "auth")
	if got := archiveTitles(m); len(got) != 1 {
		t.Fatalf("this board's archive = %v, want only the local card", got)
	}

	searchKeys(m, "ctrl+g")

	got := archiveTitles(m)
	if len(got) != 2 {
		t.Fatalf("global archive = %v, want the other board's archived card too", got)
	}
	var remote *model.Ticket
	for _, e := range m.visibleArchiveEntries() {
		if !e.isHeader && strings.Contains(e.ticket.Title, "remote") {
			tk := e.ticket
			remote = &tk
		}
	}
	if remote == nil {
		t.Fatal("the borrowed card is not in the list")
	}
	if badge := m.boardBadge(remote.ID); badge != "demo/" {
		t.Errorf("borrowed row badge = %q, want demo/", badge)
	}
}

func TestUnarchivingABorrowedRowIsRefused(t *testing.T) {
	// It belongs to another store; unarchiving it here would write this
	// board's file.
	m, s := archiveWith(t, "arc: auth local|TODO")
	other := withSprint(t, "demo", "auth remote|TODO")
	ob, _ := other.Load()
	if err := other.ArchiveByID(ob.Tickets[0].ID); err != nil {
		t.Fatal(err)
	}

	typeSearch(m, "auth")
	searchKeys(m, "ctrl+g", "enter")
	for i, e := range m.visibleArchiveEntries() {
		if !e.isHeader && strings.Contains(e.ticket.Title, "remote") {
			m.archiveCursor = i
		}
	}
	if sel := m.archiveSelected(); sel == nil || !strings.Contains(sel.Title, "remote") {
		t.Fatalf("setup: selected %v, want the borrowed row", sel)
	}

	m.unarchiveSelected()

	if !strings.Contains(m.notice, "demo") {
		t.Errorf("notice = %q, want it to name the board the row lives on", m.notice)
	}
	board, err := s.Load()
	if err != nil {
		t.Fatal(err)
	}
	for _, tk := range board.Tickets {
		if strings.Contains(tk.Title, "remote") {
			t.Fatal("the borrowed card was unarchived onto this board")
		}
	}
}

func TestArchiveCursorStopsAtTheEndOfTheVisibleList(t *testing.T) {
	// alpha and zeta share a day, so the unfiltered list is [header, alpha,
	// zeta] and filtering to alpha leaves [header, alpha]. Reading the
	// unfiltered length here walks the cursor onto a row that is not on
	// screen.
	m, _ := archiveWith(t, "arc: alpha|TODO", "arc: zeta|TODO")

	typeSearch(m, "alpha")
	searchKeys(m, "enter")
	if got := archiveTitles(m); len(got) != 1 {
		t.Fatalf("setup: visible = %v, want just alpha", got)
	}
	m.archiveCursor = len(m.visibleArchiveEntries()) - 1

	m.Update(tea.KeyMsg{Type: tea.KeyDown})

	sel := m.archiveSelected()
	if sel == nil {
		t.Fatal("down walked the cursor off the visible list")
	}
	if !strings.Contains(sel.Title, "alpha") {
		t.Errorf("selected %q, want to stay on the only visible card", sel.Title)
	}
}

func TestArchiveCancelRestoresTheCardItWasOn(t *testing.T) {
	// Two non-matching cards sort ahead of the target, so "match b" sits at
	// index 3 unfiltered and index 1 once filtered. Restoring it against the
	// unfiltered list overshoots to 3, and the clamp then quietly parks on
	// "match a" — a different card, not an obvious crash, which is why the
	// filtered list has to be the one searched.
	// Archived last sorts first, so archiving the skips last is what puts them
	// ahead of the target in the list.
	m, _ := archiveWith(t,
		"arc: match a|TODO", "arc: match b|TODO", "arc: skip one|TODO", "arc: skip two|TODO")

	typeSearch(m, "match")
	searchKeys(m, "enter")
	if got := archiveTitles(m); len(got) != 2 {
		t.Fatalf("setup: visible = %v, want both match cards", got)
	}
	m.archiveCursor = 1
	if sel := m.archiveSelected(); sel == nil || !strings.Contains(sel.Title, "match b") {
		t.Fatalf("setup: selected %v, want match b", sel)
	}

	// Reopen, type something that matches nothing, then think better of it.
	typeSearch(m, "qqq")
	searchKeys(m, "esc")

	if m.archiveSearch.query != "match" {
		t.Errorf("query = %q, want the committed filter back", m.archiveSearch.query)
	}
	sel := m.archiveSelected()
	if sel == nil {
		t.Fatal("cancel left the cursor off the visible list")
	}
	if !strings.Contains(sel.Title, "match b") {
		t.Errorf("selected %q, want the card the search started on", sel.Title)
	}
}

func TestReopeningTheArchiveKeepsItsFilterAndLandsOnAVisibleCard(t *testing.T) {
	// The archive's filter outlives the browser, so re-entering must land on
	// something that is actually on screen.
	m, _ := archiveWith(t, "arc: alpha|TODO", "arc: zeta|TODO")

	typeSearch(m, "zeta")
	searchKeys(m, "enter")
	// Leave with the browser own key, which does not clear the filter.
	m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("X")})
	if m.view != boardView {
		t.Fatalf("view = %v, want the board", m.view)
	}

	m.enterArchive()

	if !m.archiveSearch.active() {
		t.Error("the archive's filter did not survive leaving and coming back")
	}
	sel := m.archiveSelected()
	if sel == nil {
		t.Fatal("re-entering landed the cursor off the visible list")
	}
	if !strings.Contains(sel.Title, "zeta") {
		t.Errorf("selected %q, want a card the filter actually shows", sel.Title)
	}
}

// ─── Regressions found by the PR 10 review ───────────────────────────

func TestSwitchingBoardDropsBothFilters(t *testing.T) {
	// switchBoard reset the filter belonging to whichever surface happened to
	// be on screen, leaving the other one standing. A filter belongs to the
	// board it was typed on — both of them do.
	m, _ := archiveWith(t, "arc: auth old|TODO")
	withSprint(t, "demo", "unrelated|TODO")

	typeSearch(m, "auth") // the archive's filter
	searchKeys(m, "enter")
	if !m.archiveSearch.active() {
		t.Fatal("setup: archive filter not applied")
	}

	// Leave the archive with the filter still set — X does not clear it — so
	// the switch happens with the board on screen. That is the path that left
	// the archive's filter standing: it reset whichever filter the current
	// view selected, and the archive was no longer the current view.
	m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("X")})
	if m.view != boardView {
		t.Fatalf("setup: view = %v, want the board", m.view)
	}
	if !m.archiveSearch.active() {
		t.Fatal("setup: leaving the archive cleared its filter")
	}

	if err := m.switchBoard("demo"); err != nil {
		t.Fatal(err)
	}
	if m.archiveSearch.active() {
		t.Errorf("archive filter %q followed the board switch", m.archiveSearch.query)
	}
	if m.archiveSearch.query != "" || m.archiveSearch.input.Value() != "" {
		t.Errorf("archive query/input survived: %q / %q", m.archiveSearch.query, m.archiveSearch.input.Value())
	}
	if m.archiveSearch.owners != nil {
		t.Error("archive owners map survived — local rows would read as borrowed")
	}
}

func TestJumpingHomeFromTheArchiveDropsTheBoardFilter(t *testing.T) {
	// The mirror of the above: the jump happens with the archive on screen, so
	// the board's filter was the one left behind, foreign rows and all.
	m, _ := archiveWith(t, "arc: auth local|TODO", "auth live|TODO")
	other := withSprint(t, "demo", "auth remote|TODO")
	ob, _ := other.Load()
	if err := other.ArchiveByID(ob.Tickets[0].ID); err != nil {
		t.Fatal(err)
	}

	// A board filter with borrowed cards, left standing behind the archive.
	m.view = boardView
	typeSearch(m, "auth")
	searchKeys(m, "ctrl+g", "enter")
	if m.search.owners == nil {
		t.Fatal("setup: the board search borrowed nothing")
	}

	m.enterArchive()
	typeSearch(m, "auth")
	searchKeys(m, "ctrl+g", "enter")
	for i, e := range m.visibleArchiveEntries() {
		if !e.isHeader && strings.Contains(e.ticket.Title, "remote") {
			m.archiveCursor = i
		}
	}
	if sel := m.archiveSelected(); sel == nil || !strings.Contains(sel.Title, "remote") {
		t.Fatalf("setup: selected %v, want the borrowed row", sel)
	}

	if !m.jumpToForeignArchive() {
		t.Fatal("the jump did not fire")
	}

	if m.sprintName != "demo" {
		t.Fatalf("landed on %q, want demo", m.sprintName)
	}
	if m.search.active() {
		t.Errorf("board filter %q survived the jump", m.search.query)
	}
	if m.search.owners != nil || m.search.foreign != nil {
		t.Error("the board's borrowed cards survived a switch to another board")
	}
}

// breakArchive replaces a board's archive file with something unparseable.
func breakArchive(t *testing.T, sprint string) {
	t.Helper()
	root := filepath.Dir(os.Getenv("KANBAN_FILE"))
	path := filepath.Join(root, "archive.json")
	if sprint != "" {
		path = filepath.Join(root, "sprints", sprint, "archive.json")
	}
	if err := os.WriteFile(path, []byte("{ not json"), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestAnUnreadableArchiveKeepsYouOnTheBoard(t *testing.T) {
	// Entering the view first and loading after meant a failed read opened an
	// empty archive browser, which looks exactly like an archive with nothing
	// in it.
	m, _ := boardWith(t, "live one|TODO")
	breakArchive(t, "")

	m.enterArchive()

	if m.view != boardView {
		t.Errorf("view = %v, want to stay on the board when the archive cannot be read", m.view)
	}
	if m.err == nil {
		t.Error("the read failure was swallowed")
	}
}

func TestAFailedReloadKeepsBorrowedRowsBorrowed(t *testing.T) {
	// Ownership was discarded before the read that could fail, so the previous
	// rows stayed on screen with no owner: badge gone, enter dead, and the
	// guard that stops this board unarchiving another board's card bypassed.
	m, _ := archiveWith(t, "arc: auth local|TODO")
	other := withSprint(t, "demo", "auth remote|TODO")
	ob, _ := other.Load()
	if err := other.ArchiveByID(ob.Tickets[0].ID); err != nil {
		t.Fatal(err)
	}

	typeSearch(m, "auth")
	searchKeys(m, "ctrl+g", "enter")
	var remoteID string
	for _, e := range m.visibleArchiveEntries() {
		if !e.isHeader && strings.Contains(e.ticket.Title, "remote") {
			remoteID = e.ticket.ID
		}
	}
	if remoteID == "" {
		t.Fatal("setup: nothing was borrowed")
	}

	// Now the local archive goes bad under us, and something triggers a reload.
	breakArchive(t, "")
	m.toggleSearchScope()

	for _, e := range m.visibleArchiveEntries() {
		if !e.isHeader && e.ticket.ID == remoteID {
			if badge := m.boardBadge(remoteID); badge == "" {
				t.Error("a borrowed row still on screen lost its badge, so it reads as local")
			}
			if _, ok := m.ticketOwner(remoteID); !ok {
				t.Error("a borrowed row still on screen lost its owner, so unarchive would write this board")
			}
		}
	}
}
