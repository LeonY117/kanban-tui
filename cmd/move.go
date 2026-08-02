package cmd

import (
	"fmt"

	"github.com/LeonY117/kanban-tui/internal/model"
	"github.com/LeonY117/kanban-tui/internal/store"
	"github.com/spf13/cobra"
)

var moveCmd = &cobra.Command{
	Use:   "move <id> <board>",
	Short: "Move a ticket to another board",
	Long:  `Move a ticket from the current board (--sprint, or main) to another board. Use "main" as the destination for the main board. The ticket keeps its short id unless the destination already issued it, in which case it takes a fresh one from the destination's prefix.`,
	Args:  cobra.ExactArgs(2),
	RunE: func(cmd *cobra.Command, args []string) error {
		id, target := args[0], args[1]

		dst, dstName, err := targetStore(target)
		if err != nil {
			return err
		}

		board, err := st.Load()
		if err != nil {
			return err
		}
		ticket, _ := board.FindByID(id)
		if ticket == nil {
			return fmt.Errorf("ticket not found: %s", id)
		}

		status := ticket.Status
		if cmd.Flags().Changed("status") {
			s, _ := cmd.Flags().GetString("status")
			status, err = model.ParseStatus(s)
			if err != nil {
				return err
			}
		}

		if st.BoardPath() == dst.BoardPath() && status == ticket.Status {
			return fmt.Errorf("%s is already on %s", ticket.ShortID, dstName)
		}

		if err := store.MoveTicket(st, dst, ticket.ID, status); err != nil {
			return err
		}

		// Report the id it landed under — a collision on the destination
		// assigns a fresh one.
		if dstBoard, err := dst.Load(); err == nil {
			if landed, _ := dstBoard.FindByUUID(ticket.ID); landed != nil {
				fmt.Printf("Moved %s to %s as %s (%s)\n", ticket.ShortID, dstName, landed.ShortID, landed.Status)
				return nil
			}
		}
		fmt.Printf("Moved %s to %s\n", ticket.ShortID, dstName)
		return nil
	},
}

// targetStore resolves a destination board name — "main" for the main board,
// otherwise an existing sprint. Like other subcommands, a missing sprint
// hard-errors rather than being silently created.
func targetStore(name string) (*store.Store, string, error) {
	if name == "main" {
		return store.New(""), "main", nil
	}
	if err := store.ValidateSprintName(name); err != nil {
		return nil, "", err
	}
	if !store.SprintExists(name) {
		return nil, "", fmt.Errorf("sprint %q doesn't exist. Create with: kanban sprints new %s", name, name)
	}
	s, err := store.NewSprint(name)
	return s, name, err
}

func init() {
	moveCmd.Flags().String("status", "", "Land in this status (default: keep the ticket's current status)")
}
