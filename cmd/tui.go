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
	cfg := store.LoadConfig()
	refused, unbound := tui.ApplyConfig(cfg)
	if len(refused) > 0 {
		fmt.Fprintf(os.Stderr, "kanban: ignored %d key binding(s) in config.json: %s\n",
			len(refused), strings.Join(refused, ", "))
	}
	if len(unbound) > 0 {
		// Their default key went to an override that asked for it by name, so
		// these actions have no key at all until the config gives them one.
		fmt.Fprintf(os.Stderr, "kanban: no key bound for %s — its default was taken by a rebind\n",
			strings.Join(unbound, ", "))
	}
	m, err := tui.NewModel(st, sprintName)
	if err != nil {
		return err
	}
	// An absent terminalWidth means the question has never been asked, not that
	// the default was chosen — an answer is always written, the default one
	// included. Asked here because this is where config.json is read.
	if cfg.TerminalWidth == "" {
		m.AskTerminalWidth()
	}
	// Mouse cell motion gives us clicks and wheel events. Terminal text
	// selection still works with shift held down.
	p := tea.NewProgram(m, tea.WithAltScreen(), tea.WithMouseCellMotion())
	if _, err := p.Run(); err != nil {
		return fmt.Errorf("TUI error: %w", err)
	}
	return nil
}
