package cmd

import (
	"encoding/json"
	"fmt"

	"github.com/leon/kanban/internal/store"
	"github.com/spf13/cobra"
)

var sprintsCmd = &cobra.Command{
	Use:   "sprints",
	Short: "Manage sprint boards",
	Long:  "Without a subcommand, lists all sprints and their ticket counts.",
	RunE: func(cmd *cobra.Command, args []string) error {
		sprints, err := store.ListSprints()
		if err != nil {
			return err
		}

		asJSON, _ := cmd.Flags().GetBool("json")
		if asJSON {
			if sprints == nil {
				sprints = []store.SprintInfo{}
			}
			data, err := json.MarshalIndent(sprints, "", "  ")
			if err != nil {
				return err
			}
			fmt.Println(string(data))
			return nil
		}

		if len(sprints) == 0 {
			fmt.Println("No sprints.")
			return nil
		}
		for _, s := range sprints {
			fmt.Printf("  %-24s %d ticket(s)\n", s.Name, s.TicketCount)
		}
		return nil
	},
}

var sprintsNewCmd = &cobra.Command{
	Use:   "new <name>",
	Short: "Create a new sprint board",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		name := args[0]
		if err := store.CreateSprint(name); err != nil {
			return err
		}
		fmt.Printf("Created sprint %q.\n", name)
		return nil
	},
}

var sprintsRmCmd = &cobra.Command{
	Use:   "rm <name>",
	Short: "Remove a sprint board and all its tickets",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		name := args[0]
		force, _ := cmd.Flags().GetBool("force")

		if !force {
			s, err := store.NewSprint(name)
			if err != nil {
				return err
			}
			if !store.SprintExists(name) {
				return fmt.Errorf("sprint %q doesn't exist", name)
			}
			board, err := s.Load()
			if err != nil {
				return err
			}
			q := fmt.Sprintf("Remove sprint %q and all %d ticket(s)? [y/N]: ", name, len(board.Tickets))
			if !promptYN(q) {
				fmt.Println("Aborted.")
				return nil
			}
		}

		if err := store.RemoveSprint(name); err != nil {
			return err
		}
		fmt.Printf("Removed sprint %q.\n", name)
		return nil
	},
}

func init() {
	sprintsCmd.Flags().Bool("json", false, "Output as JSON")
	sprintsRmCmd.Flags().Bool("force", false, "Skip confirmation prompt")
	sprintsCmd.AddCommand(sprintsNewCmd)
	sprintsCmd.AddCommand(sprintsRmCmd)
}
