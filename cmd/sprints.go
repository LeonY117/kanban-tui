package cmd

import (
	"encoding/json"
	"fmt"

	"github.com/LeonY117/kanban-tui/internal/store"
	"github.com/spf13/cobra"
)

var sprintsCmd = &cobra.Command{
	Use:   "sprints",
	Short: "Manage sprint boards",
	Long:  "Without a subcommand, lists sprints and their ticket counts. By default only active sprints are shown; use --archived or --all to include archived sprints.",
	RunE: func(cmd *cobra.Command, args []string) error {
		sprints, err := store.ListSprints()
		if err != nil {
			return err
		}

		showArchived, _ := cmd.Flags().GetBool("archived")
		showAll, _ := cmd.Flags().GetBool("all")

		filtered := sprints[:0:0]
		for _, s := range sprints {
			if showAll || s.Archived == showArchived {
				filtered = append(filtered, s)
			}
		}

		asJSON, _ := cmd.Flags().GetBool("json")
		if asJSON {
			if filtered == nil {
				filtered = []store.SprintInfo{}
			}
			data, err := json.MarshalIndent(filtered, "", "  ")
			if err != nil {
				return err
			}
			fmt.Println(string(data))
			return nil
		}

		if len(filtered) == 0 {
			if !showArchived && !showAll && len(sprints) > 0 {
				fmt.Println("No active sprints. Use --archived or --all to see archived sprints.")
			} else {
				fmt.Println("No sprints.")
			}
			return nil
		}
		for _, s := range filtered {
			tag := ""
			if s.Archived {
				tag = "  (archived)"
			}
			fmt.Printf("  %-24s %-4s %d ticket(s)%s\n", s.Name, s.Prefix, s.TicketCount, tag)
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
		prefix, _ := cmd.Flags().GetString("prefix")
		if err := store.CreateSprint(name, prefix); err != nil {
			return err
		}
		s, err := store.NewSprint(name)
		if err != nil {
			return err
		}
		board, err := s.Load()
		if err != nil {
			return err
		}
		fmt.Printf("Created sprint %q — ticket ids %s1, %s2, …\n", name, board.Prefix, board.Prefix)
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
			if !store.SprintExists(name) {
				return fmt.Errorf("sprint %q doesn't exist", name)
			}
			s, err := store.NewSprint(name)
			if err != nil {
				return err
			}
			board, err := s.Load()
			if err != nil {
				return err
			}
			tag := ""
			if store.IsSprintArchived(name) {
				tag = " (archived)"
			}
			q := fmt.Sprintf("Remove sprint %q%s and all %d ticket(s)? [y/N]: ", name, tag, len(board.Tickets))
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

var sprintsArchiveCmd = &cobra.Command{
	Use:   "archive <name>",
	Short: "Archive a sprint (hide from default views, freeze writes)",
	Long:  "Archived sprints are hidden from `kanban sprints` and the TUI sprint picker by default. Reads still succeed; writes return an error pointing at unarchive.",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		name := args[0]
		if err := store.ArchiveSprint(name); err != nil {
			return err
		}
		fmt.Printf("Archived sprint %q.\n", name)
		return nil
	},
}

var sprintsUnarchiveCmd = &cobra.Command{
	Use:   "unarchive <name>",
	Short: "Unarchive a sprint",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		name := args[0]
		if err := store.UnarchiveSprint(name); err != nil {
			return err
		}
		fmt.Printf("Unarchived sprint %q.\n", name)
		return nil
	},
}

func init() {
	sprintsCmd.Flags().Bool("json", false, "Output as JSON")
	sprintsCmd.Flags().Bool("archived", false, "Show only archived sprints")
	sprintsCmd.Flags().Bool("all", false, "Show both active and archived sprints")
	sprintsNewCmd.Flags().String("prefix", "", "Ticket id prefix (1-4 letters; defaults to the first two letters of the name)")
	sprintsRmCmd.Flags().Bool("force", false, "Skip confirmation prompt")
	sprintsCmd.AddCommand(sprintsNewCmd)
	sprintsCmd.AddCommand(sprintsRmCmd)
	sprintsCmd.AddCommand(sprintsArchiveCmd)
	sprintsCmd.AddCommand(sprintsUnarchiveCmd)
}
