package cmd

import (
	"fmt"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/leon/kanban/internal/tui"
)

func runTUI() error {
	m, err := tui.NewModel(st)
	if err != nil {
		return err
	}
	p := tea.NewProgram(m, tea.WithAltScreen())
	if _, err := p.Run(); err != nil {
		return fmt.Errorf("TUI error: %w", err)
	}
	return nil
}
