package cmd

import (
	"encoding/json"
	"fmt"
	"strings"

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
			if s.Pinned {
				tag = "  (pinned)"
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

var sprintsPinCmd = &cobra.Command{
	Use:   "pin <name>",
	Short: "Pin a sprint to the top of the board picker",
	Long:  "Pinned sprints sort above the rest in `kanban sprints` and in the TUI board picker, in the order they were pinned. Reorder them with J/K in the picker. A pinned sprint can't be archived until it's unpinned.",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		name := args[0]
		if err := store.Pin(name); err != nil {
			return err
		}
		fmt.Printf("Pinned sprint %q.\n", name)
		return nil
	},
}

var sprintsUnpinCmd = &cobra.Command{
	Use:   "unpin <name>",
	Short: "Unpin a sprint",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		name := args[0]
		if err := store.Unpin(name); err != nil {
			return err
		}
		fmt.Printf("Unpinned sprint %q.\n", name)
		return nil
	},
}

var sprintsRenameCmd = &cobra.Command{
	Use:   "rename <name> <new-name>",
	Short: "Rename a sprint board",
	Long:  "Renames the sprint's directory and carries its pin across. Ticket ids are untouched — use `kanban sprints prefix` to change those.",
	Args:  cobra.ExactArgs(2),
	RunE: func(cmd *cobra.Command, args []string) error {
		oldName, newName := args[0], args[1]
		if err := store.UpdateSprint(oldName, newName, ""); err != nil {
			return err
		}
		fmt.Printf("Renamed sprint %q to %q.\n", oldName, newName)
		return nil
	},
}

var sprintsPrefixCmd = &cobra.Command{
	Use:   "prefix <name> <prefix>",
	Short: "Change the ticket-id prefix of a sprint's tickets",
	Long:  "Rewrites the short id of every ticket on the board and in its archive, keeping the number: KA7 becomes KB7. Refused if another board already issued one of the target ids.",
	Args:  cobra.ExactArgs(2),
	RunE: func(cmd *cobra.Command, args []string) error {
		name, prefix := args[0], args[1]
		if err := store.UpdateSprint(name, name, prefix); err != nil {
			return err
		}
		fmt.Printf("Sprint %q ids now carry %s.\n", name, strings.ToUpper(prefix))
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
	sprintsCmd.AddCommand(sprintsPinCmd)
	sprintsCmd.AddCommand(sprintsUnpinCmd)
	sprintsCmd.AddCommand(sprintsRenameCmd)
	sprintsCmd.AddCommand(sprintsPrefixCmd)
}
