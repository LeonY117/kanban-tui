package cmd

import (
	"fmt"

	"github.com/LeonY117/kanban-tui/internal/model"
	"github.com/spf13/cobra"
)

var updateCmd = &cobra.Command{
	Use:   "update <id>",
	Short: "Update a ticket",
	Long:  "Update a ticket. <id> is the short form — 42 on main, KA7 on a sprint — and the prefix is implied on its own board, so 'kanban --sprint kanban update 7' resolves to KA7.",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		id := args[0]

		// Parse before st.Update — its callback has no error return, so a
		// status parsed inside it could only ever be dropped silently.
		var status model.Status
		if cmd.Flags().Changed("status") {
			s, _ := cmd.Flags().GetString("status")
			parsed, err := model.ParseStatus(s)
			if err != nil {
				return err
			}
			status = parsed
		}

		err := st.Update(id, func(t *model.Ticket) {
			if status != "" {
				t.Status = status
			}
			if cmd.Flags().Changed("title") {
				t.Title, _ = cmd.Flags().GetString("title")
			}
			if cmd.Flags().Changed("desc") {
				t.Description, _ = cmd.Flags().GetString("desc")
			}
			if cmd.Flags().Changed("assigned-to") {
				t.AssignedTo, _ = cmd.Flags().GetString("assigned-to")
			}
			if cmd.Flags().Changed("tag") {
				t.Tags, _ = cmd.Flags().GetStringSlice("tag")
			}
		})
		if err != nil {
			return err
		}

		fmt.Printf("Updated %s\n", id)
		if cmd.Flags().Changed("title") {
			title, _ := cmd.Flags().GetString("title")
			warnFragileEmoji(cmd, title)
		}
		return nil
	},
}

func init() {
	updateCmd.Flags().String("status", "", fmt.Sprintf("New status (%s)", statusValues()))
	updateCmd.Flags().String("title", "", "New title")
	updateCmd.Flags().String("desc", "", "New description — replaces the existing one rather than appending")
	updateCmd.Flags().String("assigned-to", "", "New assignee")
	updateCmd.Flags().StringSlice("tag", nil, "Replace tags")
}
