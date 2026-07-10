package model

import (
	"fmt"
	"strings"
	"time"
)

type Status string

const (
	StatusBacklog Status = "BACKLOG"
	StatusTodo    Status = "TODO"
	StatusDoing   Status = "DOING"
	StatusWaiting Status = "WAITING"
	StatusDone    Status = "DONE"
	StatusHold    Status = "HOLD"
)

var AllStatuses = []Status{StatusBacklog, StatusTodo, StatusDoing, StatusWaiting, StatusDone, StatusHold}

// ColumnOrder defines display order for TUI columns.
var ColumnOrder = []Status{StatusBacklog, StatusTodo, StatusDoing, StatusWaiting, StatusDone, StatusHold}

func ParseStatus(s string) (Status, error) {
	switch strings.ToUpper(strings.TrimSpace(s)) {
	case "BACKLOG":
		return StatusBacklog, nil
	case "TODO":
		return StatusTodo, nil
	case "DOING":
		return StatusDoing, nil
	case "WAITING", "WAITING ON", "WAITING-ON", "WAITING_ON":
		return StatusWaiting, nil
	case "DONE":
		return StatusDone, nil
	case "HOLD":
		return StatusHold, nil
	default:
		return "", fmt.Errorf("invalid status %q, valid: BACKLOG, TODO, DOING, WAITING, DONE, HOLD", s)
	}
}

type Priority string

const (
	P0 Priority = "P0"
	P1 Priority = "P1"
	P2 Priority = "P2"
	P3 Priority = "P3"
)

func ParsePriority(s string) (Priority, error) {
	switch strings.ToUpper(strings.TrimSpace(s)) {
	case "P0":
		return P0, nil
	case "P1":
		return P1, nil
	case "P2":
		return P2, nil
	case "P3":
		return P3, nil
	default:
		return "", fmt.Errorf("invalid priority %q, valid: P0, P1, P2, P3", s)
	}
}

type Ticket struct {
	ID          string            `json:"id"`
	ShortID     string            `json:"short_id"`
	Title       string            `json:"title"`
	Description string            `json:"description,omitempty"`
	Status      Status            `json:"status"`
	Priority    Priority          `json:"priority"`
	Tags        []string          `json:"tags,omitempty"`
	AssignedTo  string            `json:"assigned_to,omitempty"`
	CreatedAt   time.Time         `json:"created_at"`
	UpdatedAt   time.Time         `json:"updated_at"`
	ArchivedAt  *time.Time        `json:"archived_at,omitempty"`
	CreatedBy   string            `json:"created_by,omitempty"`
	Meta        map[string]string `json:"meta,omitempty"`
}

// Board is the top-level container persisted to JSON. Prefix is the letter
// code new ticket ids on this board carry (empty on the main board, whose ids
// are bare numbers).
type Board struct {
	Version int      `json:"version"`
	Prefix  string   `json:"prefix,omitempty"`
	Tickets []Ticket `json:"tickets"`
}

// FilterOptions for querying tickets.
type FilterOptions struct {
	Status     *Status
	Priority   *Priority
	Tag        string
	AssignedTo *string // pointer so we can distinguish "not set" from "filter for empty"
}

func (b *Board) Filter(opts FilterOptions) []Ticket {
	var result []Ticket
	for _, t := range b.Tickets {
		if opts.Status != nil && t.Status != *opts.Status {
			continue
		}
		if opts.Priority != nil && t.Priority != *opts.Priority {
			continue
		}
		if opts.Tag != "" && !containsTag(t.Tags, opts.Tag) {
			continue
		}
		if opts.AssignedTo != nil && t.AssignedTo != *opts.AssignedTo {
			continue
		}
		result = append(result, t)
	}
	return result
}

func (b *Board) ByStatus(status Status) []Ticket {
	s := status
	return b.Filter(FilterOptions{Status: &s})
}

// FindByID resolves a ticket by full UUID, short id, or UUID prefix. Short ids
// match case-insensitively, and on a prefixed board a bare number resolves too
// — `kanban update 13` finds K13 — since the prefix is implied by the board
// you're already talking to.
func (b *Board) FindByID(id string) (*Ticket, int) {
	id = strings.TrimSpace(id)
	lower := strings.ToLower(id)
	upper := strings.ToUpper(id)

	implied := ""
	if b.Prefix != "" && isAllDigits(id) {
		implied = strings.ToUpper(b.Prefix) + upper
	}

	// Exact matches first, across the whole board, before any implied-prefix
	// match. A ticket moved in from another board keeps its id, so a board can
	// hold both `1` and `KA1` — and then `update 1` meant whichever came first
	// in board order. What the user typed wins over what the board implies.
	for i := range b.Tickets {
		t := &b.Tickets[i]
		if t.ID == lower || strings.ToUpper(t.ShortID) == upper {
			return t, i
		}
	}
	if implied != "" {
		for i := range b.Tickets {
			if strings.ToUpper(b.Tickets[i].ShortID) == implied {
				return &b.Tickets[i], i
			}
		}
	}
	// UUID-prefix matching is the legacy path, tried last so a short id can
	// never be shadowed by an unrelated UUID that happens to start the same.
	for i := range b.Tickets {
		if strings.HasPrefix(b.Tickets[i].ID, lower) {
			return &b.Tickets[i], i
		}
	}
	return nil, -1
}

// FindByUUID resolves a ticket by its full UUID and nothing else. Used where a
// caller has to recognise one specific ticket rather than resolve what a human
// typed — FindByID's prefix and implied-prefix matching are wrong there.
func (b *Board) FindByUUID(id string) (*Ticket, int) {
	id = strings.ToLower(strings.TrimSpace(id))
	for i := range b.Tickets {
		if b.Tickets[i].ID == id {
			return &b.Tickets[i], i
		}
	}
	return nil, -1
}

// ShortIDTaken reports whether a ticket already carries this exact short id,
// case-insensitively.
//
// Deliberately not FindByID: that resolver also matches UUID prefixes, so short
// id "1" would collide with any ticket whose UUID merely starts with 1 — a
// false collision that renames a ticket for no reason.
func (b *Board) ShortIDTaken(shortID string) bool {
	want := strings.ToUpper(strings.TrimSpace(shortID))
	if want == "" {
		return false
	}
	for i := range b.Tickets {
		if strings.ToUpper(b.Tickets[i].ShortID) == want {
			return true
		}
	}
	return false
}

func isAllDigits(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

func containsTag(tags []string, tag string) bool {
	tag = strings.ToLower(tag)
	for _, t := range tags {
		if strings.ToLower(t) == tag {
			return true
		}
	}
	return false
}
