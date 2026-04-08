package cmd

import (
	"fmt"

	"github.com/leon/kanban/internal/model"
	"github.com/spf13/cobra"
)

var updateCmd = &cobra.Command{
	Use:   "update <id>",
	Short: "Update a ticket",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		id := args[0]

		err := st.Update(id, func(t *model.Ticket) {
			if cmd.Flags().Changed("status") {
				s, _ := cmd.Flags().GetString("status")
				status, err := model.ParseStatus(s)
				if err == nil {
					t.Status = status
				}
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
		return nil
	},
}

func init() {
	updateCmd.Flags().String("status", "", "New status")
	updateCmd.Flags().String("title", "", "New title")
	updateCmd.Flags().String("desc", "", "New description")
	updateCmd.Flags().String("assigned-to", "", "New assignee")
	updateCmd.Flags().StringSlice("tag", nil, "Replace tags")
}
