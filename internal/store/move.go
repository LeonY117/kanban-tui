package store

import (
	"fmt"
	"time"

	"github.com/LeonY117/kanban-tui/internal/model"
)

// MoveTicket moves a ticket from one board to another, landing it in
// newStatus. Same-board moves collapse to a plain status update.
//
// The write order is deliberate: the ticket is added to dst before it's
// removed from src, so a crash between the two steps leaves a duplicate
// rather than losing the ticket. Each board is locked separately — never
// both at once — so two concurrent moves in opposite directions can't
// deadlock.
func MoveTicket(src, dst *Store, id string, newStatus model.Status) error {
	if src.BoardPath() == dst.BoardPath() {
		return src.Update(id, func(t *model.Ticket) {
			t.Status = newStatus
		})
	}

	var ticket model.Ticket
	if err := src.WithLock(func() error {
		board, err := src.Load()
		if err != nil {
			return err
		}
		t, _ := board.FindByID(id)
		if t == nil {
			return fmt.Errorf("ticket not found: %s", id)
		}
		ticket = *t
		return nil
	}); err != nil {
		return err
	}

	if err := dst.WithLock(func() error {
		board, err := dst.Load()
		if err != nil {
			return err
		}
		// A move interrupted between the two writes leaves the ticket on both
		// boards, and the natural response is to run it again. Recognising our
		// own earlier write by UUID makes that retry finish the move instead of
		// appending a second copy sharing the first one's UUID — which no
		// lookup could tell apart afterwards.
		if existing, _ := board.FindByUUID(ticket.ID); existing != nil {
			return nil
		}
		t := ticket
		t.Status = newStatus
		t.UpdatedAt = time.Now()
		// The ticket keeps its id — references to it in commits and notes stay
		// good — unless the destination already uses that id, in which case it
		// takes a fresh one from the destination's own prefix.
		if board.ShortIDTaken(t.ShortID) {
			newID, err := NextTicketID(dst.ensurePrefix(board))
			if err != nil {
				return err
			}
			t.ShortID = newID
		}
		board.Tickets = append(board.Tickets, t)
		return dst.Save(board)
	}); err != nil {
		return err
	}

	return src.WithLock(func() error {
		board, err := src.Load()
		if err != nil {
			return err
		}
		// By UUID, not by what the user typed: the id they gave resolves against
		// a board that may have changed since, and removing "whatever that
		// resolves to now" could delete a different ticket than the one moved.
		_, idx := board.FindByUUID(ticket.ID)
		if idx < 0 {
			return nil
		}
		board.Tickets = append(board.Tickets[:idx], board.Tickets[idx+1:]...)
		return src.Save(board)
	})
}
