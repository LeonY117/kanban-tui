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
	StatusDone    Status = "DONE"
	StatusHold    Status = "HOLD"
)

var AllStatuses = []Status{StatusBacklog, StatusTodo, StatusDoing, StatusDone, StatusHold}

// ColumnOrder defines display order for TUI columns.
var ColumnOrder = []Status{StatusBacklog, StatusTodo, StatusDoing, StatusDone, StatusHold}

func ParseStatus(s string) (Status, error) {
	switch strings.ToUpper(strings.TrimSpace(s)) {
	case "BACKLOG":
		return StatusBacklog, nil
	case "TODO":
		return StatusTodo, nil
	case "DOING":
		return StatusDoing, nil
	case "DONE":
		return StatusDone, nil
	case "HOLD":
		return StatusHold, nil
	default:
		return "", fmt.Errorf("invalid status %q, valid: BACKLOG, TODO, DOING, DONE, HOLD", s)
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

// Board is the top-level container persisted to JSON.
type Board struct {
	Version int      `json:"version"`
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

func (b *Board) FindByID(id string) (*Ticket, int) {
	id = strings.ToLower(id)
	for i := range b.Tickets {
		if b.Tickets[i].ID == id || b.Tickets[i].ShortID == id || strings.HasPrefix(b.Tickets[i].ID, id) {
			return &b.Tickets[i], i
		}
	}
	return nil, -1
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
