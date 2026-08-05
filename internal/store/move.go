package store

import (
	"fmt"
	"time"

	"github.com/LeonY117/kanban-tui/internal/model"
)

// MoveTicket moves a ticket from one board to another, landing it in
// newStatus. Same-board moves collapse to a plain status update.
//
// Both boards are locked for the whole move, so an edit arriving mid-move
// either lands before the ticket is read or waits until it has left. Holding
// one lock at a time was cheaper but lost such an edit outright: it applied to
// the source copy, which the move then deleted.
//
// Two locks invite deadlock, so they're always taken in board-path order —
// moves in opposite directions queue behind each other instead of each holding
// what the other wants.
func MoveTicket(src, dst *Store, id string, newStatus model.Status) error {
	if src.BoardPath() == dst.BoardPath() {
		return src.Update(id, func(t *model.Ticket) {
			t.Status = newStatus
		})
	}

	first, second := src, dst
	if src.BoardPath() > dst.BoardPath() {
		first, second = dst, src
	}
	return first.WithLock(func() error {
		return second.WithLock(func() error {
			return moveLocked(src, dst, id, newStatus)
		})
	})
}

// moveLocked performs the move with both boards already locked.
//
// The write order is still deliberate: the ticket reaches dst before it leaves
// src, so a crash between the two leaves a duplicate rather than losing the
// ticket — and re-running the move clears it up (see the UUID check below).
func moveLocked(src, dst *Store, id string, newStatus model.Status) error {
	srcBoard, err := src.Load()
	if err != nil {
		return err
	}
	found, _ := srcBoard.FindByID(id)
	if found == nil {
		return fmt.Errorf("ticket not found: %s", id)
	}
	ticket := *found

	dstBoard, err := dst.Load()
	if err != nil {
		return err
	}
	// A move interrupted between the two writes leaves the ticket on both
	// boards, and the natural response is to run it again. Recognising our own
	// earlier write by UUID makes that retry finish the move instead of
	// appending a second copy sharing the first one's UUID — which no lookup
	// could tell apart afterwards.
	t := ticket
	t.Status = newStatus
	t.UpdatedAt = time.Now()
	if existing, _ := dstBoard.FindByUUID(ticket.ID); existing != nil {
		// Refresh it rather than leave the copy the interrupted attempt froze:
		// the src copy stayed editable in the meantime, so it is the newer of
		// the two. The short id this board already minted stays as it is —
		// anything referring to the ticket here already uses it.
		t.ShortID = existing.ShortID
		*existing = t
	} else {
		// The ticket keeps its id — references to it in commits and notes stay
		// good — unless the destination already uses that id, in which case it
		// takes a fresh one from the destination's own prefix.
		if dstBoard.ShortIDTaken(t.ShortID) {
			newID, err := NextTicketID(dst.ensurePrefix(dstBoard))
			if err != nil {
				return err
			}
			t.ShortID = newID
		}
		dstBoard.Tickets = append(dstBoard.Tickets, t)
	}
	if err := dst.Save(dstBoard); err != nil {
		return err
	}

	// By UUID, not by what the user typed: the id they gave resolves against a
	// board that may hold other candidates, and removing "whatever that
	// resolves to" could delete a different ticket than the one moved.
	_, idx := srcBoard.FindByUUID(ticket.ID)
	if idx < 0 {
		return nil
	}
	srcBoard.Tickets = append(srcBoard.Tickets[:idx], srcBoard.Tickets[idx+1:]...)
	return src.Save(srcBoard)
}
