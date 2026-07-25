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
		t := ticket
		t.Status = newStatus
		t.UpdatedAt = time.Now()
		// The short ID is only unique within a board, so it's re-derived
		// against the destination's tickets.
		t.ShortID = dst.uniqueShortID(board, t.ID)
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
		_, idx := board.FindByID(id)
		if idx < 0 {
			return nil
		}
		board.Tickets = append(board.Tickets[:idx], board.Tickets[idx+1:]...)
		return src.Save(board)
	})
}
