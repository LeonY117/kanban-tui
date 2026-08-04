package tui

import (
	"reflect"
	"testing"

	"github.com/LeonY117/kanban-tui/internal/model"
)

func TestAddPopupStoresPriority(t *testing.T) {
	m := testModel(t, "existing")
	m.enterAddPopup()
	m.addTitle.SetValue("urgent")
	m.addPriority = 3
	m.submitAdd()

	ticket, _ := m.board.FindByID("2")
	if ticket == nil || ticket.Priority != 3 {
		t.Fatalf("added ticket priority = %v, want 3", ticket)
	}
}

func TestVisibleTicketsSortsByPriorityHighestFirst(t *testing.T) {
	m := &Model{board: &model.Board{Tickets: []model.Ticket{
		{ID: "low", Title: "low", Status: model.StatusTodo, Priority: 0},
		{ID: "high", Title: "high", Status: model.StatusTodo, Priority: 3},
		{ID: "medium", Title: "medium", Status: model.StatusTodo, Priority: 2},
		{ID: "same", Title: "same", Status: model.StatusTodo, Priority: 2},
	}}}

	var got []string
	for _, ticket := range m.visibleTickets(model.StatusTodo) {
		got = append(got, ticket.ID)
	}
	want := []string{"high", "medium", "same", "low"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("visible ticket order = %v, want %v", got, want)
	}
}
