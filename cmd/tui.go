package cmd

import (
	"fmt"
	"os"
	"strings"

	"github.com/LeonY117/kanban-tui/internal/store"
	"github.com/LeonY117/kanban-tui/internal/tui"
	tea "github.com/charmbracelet/bubbletea"
)

func runTUI(sprintName string) error {
	if refused := tui.ApplyConfig(store.LoadConfig()); len(refused) > 0 {
		fmt.Fprintf(os.Stderr, "kanban: ignored %d key binding(s) in config.json: %s\n",
			len(refused), strings.Join(refused, ", "))
	}
	m, err := tui.NewModel(st, sprintName)
	if err != nil {
		return err
	}
	// Mouse cell motion gives us clicks and wheel events. Terminal text
	// selection still works with shift held down.
	p := tea.NewProgram(m, tea.WithAltScreen(), tea.WithMouseCellMotion())
	if _, err := p.Run(); err != nil {
		return fmt.Errorf("TUI error: %w", err)
	}
	return nil
}
