package cmd

import (
	"fmt"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/leon/kanban/internal/tui"
)

func runTUI(sprintName string) error {
	m, err := tui.NewModel(st, sprintName)
	if err != nil {
		return err
	}
	p := tea.NewProgram(m, tea.WithAltScreen())
	if _, err := p.Run(); err != nil {
		return fmt.Errorf("TUI error: %w", err)
	}
	return nil
}
