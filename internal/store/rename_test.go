package store

import (
	"strings"
	"testing"

	"github.com/LeonY117/kanban-tui/internal/model"
)

func addTicket(t *testing.T, s *Store, title string) *model.Ticket {
	t.Helper()
	ticket, err := s.Add(title, "", model.StatusTodo, nil, "", "test")
	if err != nil {
		t.Fatalf("add %q: %v", title, err)
	}
	return ticket
}

func shortIDs(t *testing.T, s *Store) []string {
	t.Helper()
	board, err := s.Load()
	if err != nil {
		t.Fatal(err)
	}
	ids := make([]string, 0, len(board.Tickets))
	for _, ticket := range board.Tickets {
		ids = append(ids, ticket.ShortID)
	}
	return ids
}

func TestRenameSprintMovesTheBoard(t *testing.T) {
	sandboxRoot(t)
	mustCreateSprint(t, "old-name")
	s, err := NewSprint("old-name")
	if err != nil {
		t.Fatal(err)
	}
	addTicket(t, s, "keep me")

	if err := UpdateSprint("old-name", "new-name", ""); err != nil {
		t.Fatalf("rename: %v", err)
	}

	if SprintExists("old-name") {
		t.Error("the old name still resolves")
	}
	renamed, err := NewSprint("new-name")
	if err != nil {
		t.Fatal(err)
	}
	board, err := renamed.Load()
	if err != nil {
		t.Fatal(err)
	}
	if len(board.Tickets) != 1 || board.Tickets[0].Title != "keep me" {
		t.Errorf("tickets did not travel with the rename: %+v", board.Tickets)
	}
	// Ids are the sprint's identity to anything outside the board — a rename
	// alone must not touch them.
	if got := board.Tickets[0].ShortID; got != "OL1" {
		t.Errorf("short id = %s, want OL1 unchanged by the rename", got)
	}
}

// A sprint created before prefixes existed has none stored and derives one from
// its name, so a rename would silently move its future ids to a new prefix.
func TestRenamePinsDownADerivedPrefix(t *testing.T) {
	sandboxRoot(t)
	mustCreateSprint(t, "alpha")
	s, err := NewSprint("alpha")
	if err != nil {
		t.Fatal(err)
	}
	board, err := s.Load()
	if err != nil {
		t.Fatal(err)
	}
	board.Prefix = "" // as an old board on disk would be
	if err := s.Save(board); err != nil {
		t.Fatal(err)
	}

	if err := UpdateSprint("alpha", "zulu", ""); err != nil {
		t.Fatalf("rename: %v", err)
	}

	renamed, err := NewSprint("zulu")
	if err != nil {
		t.Fatal(err)
	}
	board, err = renamed.Load()
	if err != nil {
		t.Fatal(err)
	}
	if board.Prefix != "AL" {
		t.Errorf("prefix = %q, want AL — the rename let it re-derive from the new name", board.Prefix)
	}
	ticket := addTicket(t, renamed, "after")
	if !strings.HasPrefix(ticket.ShortID, "AL") {
		t.Errorf("next id = %s, want an AL id", ticket.ShortID)
	}
}

func TestRenameCarriesThePinAndItsPosition(t *testing.T) {
	sandboxRoot(t)
	for _, name := range []string{"first", "second", "third"} {
		mustCreateSprint(t, name)
		if err := Pin(name); err != nil {
			t.Fatalf("pin %q: %v", name, err)
		}
	}

	if err := UpdateSprint("second", "middle", ""); err != nil {
		t.Fatalf("rename: %v", err)
	}

	if got, want := pinNames(t), []string{"first", "middle", "third"}; !equalStrings(got, want) {
		t.Errorf("pins = %v, want %v", got, want)
	}
}

func TestRenameRejectsTakenAndInvalidNames(t *testing.T) {
	sandboxRoot(t)
	mustCreateSprint(t, "alpha")
	mustCreateSprint(t, "beta")

	if err := UpdateSprint("alpha", "beta", ""); err == nil {
		t.Error("renaming onto an existing sprint succeeded")
	}
	if err := UpdateSprint("alpha", "not a valid name", ""); err == nil {
		t.Error("an invalid name was accepted")
	}
	if !SprintExists("alpha") {
		t.Error("alpha was disturbed by a refused rename")
	}
}

func TestRenameRejectsArchivedSprint(t *testing.T) {
	sandboxRoot(t)
	mustCreateSprint(t, "old")
	if err := ArchiveSprint("old"); err != nil {
		t.Fatal(err)
	}
	err := UpdateSprint("old", "older", "")
	if err == nil {
		t.Fatal("renamed an archived sprint")
	}
	if !strings.Contains(err.Error(), "unarchive") {
		t.Errorf("error %q doesn't point at unarchive", err)
	}
}

// The number is the part people quote — a retag carries it over so KA7 stays
// findable as KB7.
func TestPrefixChangeRewritesIDsKeepingNumbers(t *testing.T) {
	sandboxRoot(t)
	mustCreateSprint(t, "kanban")
	s, err := NewSprint("kanban")
	if err != nil {
		t.Fatal(err)
	}
	addTicket(t, s, "one")
	second := addTicket(t, s, "two")
	if err := s.ArchiveByID(second.ID); err != nil {
		t.Fatalf("archive ticket: %v", err)
	}
	addTicket(t, s, "three")
	if got, want := shortIDs(t, s), []string{"KA1", "KA3"}; !equalStrings(got, want) {
		t.Fatalf("setup ids = %v, want %v", got, want)
	}

	if err := UpdateSprint("kanban", "kanban", "zz"); err != nil {
		t.Fatalf("retag: %v", err)
	}

	if got, want := shortIDs(t, s), []string{"ZZ1", "ZZ3"}; !equalStrings(got, want) {
		t.Errorf("board ids = %v, want %v", got, want)
	}
	archive, err := s.LoadArchive()
	if err != nil {
		t.Fatal(err)
	}
	if len(archive.Tickets) != 1 || archive.Tickets[0].ShortID != "ZZ2" {
		t.Errorf("archived id = %+v, want ZZ2 — the archive was left on the old prefix", archive.Tickets)
	}
	board, err := s.Load()
	if err != nil {
		t.Fatal(err)
	}
	if board.Prefix != "ZZ" {
		t.Errorf("stored prefix = %q, want ZZ", board.Prefix)
	}

	// The counter for the new prefix has to clear the ids just minted.
	next := addTicket(t, s, "four")
	if next.ShortID != "ZZ4" {
		t.Errorf("next id = %s, want ZZ4 — the retag left the counter behind", next.ShortID)
	}
}

func TestPrefixChangeRefusesIDsIssuedElsewhere(t *testing.T) {
	sandboxRoot(t)
	mustCreateSprint(t, "kanban")
	mustCreateSprint(t, "brain")
	kanban, err := NewSprint("kanban")
	if err != nil {
		t.Fatal(err)
	}
	brain, err := NewSprint("brain")
	if err != nil {
		t.Fatal(err)
	}
	addTicket(t, kanban, "ka one")
	addTicket(t, brain, "br one") // BR1 — the id KA1 would become

	err = UpdateSprint("kanban", "kanban", "br")
	if err == nil {
		t.Fatal("retag onto an id another board owns succeeded")
	}
	if !strings.Contains(err.Error(), "BR1") {
		t.Errorf("error %q doesn't name the clashing id", err)
	}
	if got, want := shortIDs(t, kanban), []string{"KA1"}; !equalStrings(got, want) {
		t.Errorf("ids = %v, want %v — a refused retag rewrote them anyway", got, want)
	}
}

// An archived ticket still owns its id, so it has to block a retag too.
func TestPrefixChangeRefusesIDsHeldByAnArchive(t *testing.T) {
	sandboxRoot(t)
	mustCreateSprint(t, "kanban")
	kanban, err := NewSprint("kanban")
	if err != nil {
		t.Fatal(err)
	}
	addTicket(t, kanban, "ka one")

	main := New("")
	gone := addTicket(t, main, "main one")
	if err := main.ArchiveByID(gone.ID); err != nil {
		t.Fatal(err)
	}
	// Main issues bare numbers, so its archived ticket holds "1" — which is
	// exactly the id an empty-prefix retag would mint. Prefixes are letters, so
	// instead prove the archive is scanned via a sprint.
	mustCreateSprint(t, "brain")
	brain, err := NewSprint("brain")
	if err != nil {
		t.Fatal(err)
	}
	archived := addTicket(t, brain, "br one")
	if err := brain.ArchiveByID(archived.ID); err != nil {
		t.Fatal(err)
	}

	if err := UpdateSprint("kanban", "kanban", "br"); err == nil {
		t.Error("retag succeeded despite BR1 sitting in another board's archive")
	}
}

// Boards are allowed to share a prefix and interleave their numbers, so the
// check is per-id, not per-prefix: retagging onto a prefix another board already
// uses goes through as long as no individual id collides.
func TestPrefixChangeAllowsASharedPrefixWithFreeNumbers(t *testing.T) {
	sandboxRoot(t)
	if err := CreateSprint("shared", "SH"); err != nil {
		t.Fatal(err)
	}
	shared, err := NewSprint("shared")
	if err != nil {
		t.Fatal(err)
	}
	addTicket(t, shared, "sh one") // SH1

	mustCreateSprint(t, "later")
	later, err := NewSprint("later")
	if err != nil {
		t.Fatal(err)
	}
	ticket := addTicket(t, later, "la one")
	// Move it clear of SH1, so the retag would mint SH9 rather than SH1.
	if err := later.Update(ticket.ID, func(tk *model.Ticket) { tk.ShortID = "LA9" }); err != nil {
		t.Fatal(err)
	}

	if err := UpdateSprint("later", "later", "SH"); err != nil {
		t.Fatalf("retag onto a free number under a shared prefix was refused: %v", err)
	}
	if got, want := shortIDs(t, later), []string{"SH9"}; !equalStrings(got, want) {
		t.Errorf("ids = %v, want %v", got, want)
	}
	if got, want := shortIDs(t, shared), []string{"SH1"}; !equalStrings(got, want) {
		t.Errorf("the other board's ids changed: %v, want %v", got, want)
	}
}

// A cross-board move keeps the ticket's short id, so a board can hold ids from a
// foreign prefix. Retagging onto that prefix would mint an id the board already
// holds — and the cross-board check can't see it, because it skips this board.
func TestPrefixChangeRefusesIDsHeldByTheBoardItself(t *testing.T) {
	sandboxRoot(t)
	if err := CreateSprint("alpha", "AL"); err != nil {
		t.Fatal(err)
	}
	mustCreateSprint(t, "kanban")
	alpha, err := NewSprint("alpha")
	if err != nil {
		t.Fatal(err)
	}
	kanban, err := NewSprint("kanban")
	if err != nil {
		t.Fatal(err)
	}

	moved := addTicket(t, alpha, "born on alpha") // AL1
	addTicket(t, kanban, "ka one")                // KA1
	if err := MoveTicket(alpha, kanban, moved.ID, landIn(model.StatusTodo)); err != nil {
		t.Fatal(err)
	}
	if got, want := shortIDs(t, kanban), []string{"KA1", "AL1"}; !equalStrings(got, want) {
		t.Fatalf("setup ids = %v, want %v", got, want)
	}

	err = UpdateSprint("kanban", "kanban", "AL")
	if err == nil {
		t.Fatal("retag succeeded, leaving two AL1 tickets on one board")
	}
	if !strings.Contains(err.Error(), "AL1") {
		t.Errorf("error %q doesn't name the clashing id", err)
	}
	if got, want := shortIDs(t, kanban), []string{"KA1", "AL1"}; !equalStrings(got, want) {
		t.Errorf("ids = %v, want %v — a refused retag rewrote them anyway", got, want)
	}
}

// Capitalisation has to be fixable. On a case-insensitive filesystem the new name
// resolves to the sprint's own directory, which must not read as "taken".
func TestRenameAllowsACaseOnlyChange(t *testing.T) {
	sandboxRoot(t)
	mustCreateSprint(t, "kanban")
	s, err := NewSprint("kanban")
	if err != nil {
		t.Fatal(err)
	}
	addTicket(t, s, "one")

	if err := UpdateSprint("kanban", "Kanban", ""); err != nil {
		t.Fatalf("case-only rename refused: %v", err)
	}
	renamed, err := NewSprint("Kanban")
	if err != nil {
		t.Fatal(err)
	}
	board, err := renamed.Load()
	if err != nil {
		t.Fatal(err)
	}
	if len(board.Tickets) != 1 {
		t.Errorf("tickets after a case-only rename: %d, want 1", len(board.Tickets))
	}
}

// An archived sprint is hidden from the default picker, so a bare "already
// exists" names a board the user cannot see.
func TestRenameOntoArchivedNameSaysItIsArchived(t *testing.T) {
	sandboxRoot(t)
	mustCreateSprint(t, "alpha")
	mustCreateSprint(t, "beta")
	if err := ArchiveSprint("beta"); err != nil {
		t.Fatal(err)
	}

	err := UpdateSprint("alpha", "beta", "")
	if err == nil {
		t.Fatal("rename onto an archived name succeeded")
	}
	if !strings.Contains(err.Error(), "archived") {
		t.Errorf("error %q doesn't say the existing sprint is archived", err)
	}
}

func TestPrefixChangeRejectsInvalidPrefix(t *testing.T) {
	sandboxRoot(t)
	mustCreateSprint(t, "kanban")
	for _, bad := range []string{"TOOLONG", "K1", "-"} {
		if err := UpdateSprint("kanban", "kanban", bad); err == nil {
			t.Errorf("prefix %q was accepted", bad)
		}
	}
}

// Renaming and retagging in one call has to leave both applied.
func TestRenameAndRetagTogether(t *testing.T) {
	sandboxRoot(t)
	mustCreateSprint(t, "kanban")
	s, err := NewSprint("kanban")
	if err != nil {
		t.Fatal(err)
	}
	addTicket(t, s, "one")
	if err := Pin("kanban"); err != nil {
		t.Fatal(err)
	}

	if err := UpdateSprint("kanban", "tools", "TL"); err != nil {
		t.Fatalf("rename+retag: %v", err)
	}

	renamed, err := NewSprint("tools")
	if err != nil {
		t.Fatal(err)
	}
	if got, want := shortIDs(t, renamed), []string{"TL1"}; !equalStrings(got, want) {
		t.Errorf("ids = %v, want %v", got, want)
	}
	if SprintExists("kanban") {
		t.Error("the old directory survived")
	}
	if got, want := pinNames(t), []string{"tools"}; !equalStrings(got, want) {
		t.Errorf("pins = %v, want %v", got, want)
	}
}

// A no-op call is not an error, and must not disturb the board.
func TestUpdateSprintNoOp(t *testing.T) {
	sandboxRoot(t)
	mustCreateSprint(t, "kanban")
	s, err := NewSprint("kanban")
	if err != nil {
		t.Fatal(err)
	}
	addTicket(t, s, "one")

	if err := UpdateSprint("kanban", "kanban", ""); err != nil {
		t.Fatalf("no-op update: %v", err)
	}
	if err := UpdateSprint("kanban", "", "KA"); err != nil {
		t.Fatalf("no-op update with an empty new name: %v", err)
	}
	if got, want := shortIDs(t, s), []string{"KA1"}; !equalStrings(got, want) {
		t.Errorf("ids = %v, want %v", got, want)
	}
}

// Ids that predate prefixes are the head of the ticket's own uuid — there is no
// number to carry, so a retag has to leave them alone rather than mangle them.
func TestPrefixChangeLeavesLegacyIDsAlone(t *testing.T) {
	sandboxRoot(t)
	mustCreateSprint(t, "kanban")
	s, err := NewSprint("kanban")
	if err != nil {
		t.Fatal(err)
	}
	addTicket(t, s, "modern")

	board, err := s.Load()
	if err != nil {
		t.Fatal(err)
	}
	// A pre-prefix ticket: its short id is the head of its own uuid, which is
	// exactly what marks it legacy.
	const legacyUUID = "82d31c9e-1111-4222-8333-444455556666"
	legacy := legacyUUID[:6]
	board.Tickets = append(board.Tickets, model.Ticket{
		ID:      legacyUUID,
		ShortID: legacy,
		Title:   "ancient",
		Status:  model.StatusTodo,
	})
	if err := s.Save(board); err != nil {
		t.Fatal(err)
	}

	if err := UpdateSprint("kanban", "kanban", "ZZ"); err != nil {
		t.Fatalf("retag: %v", err)
	}
	if got, want := shortIDs(t, s), []string{"ZZ1", legacy}; !equalStrings(got, want) {
		t.Errorf("ids = %v, want %v", got, want)
	}
}
