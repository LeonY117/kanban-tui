package cmd

import (
	"fmt"
	"strings"

	"github.com/LeonY117/kanban-tui/internal/model"
	"github.com/spf13/cobra"
)

var addCmd = &cobra.Command{
	Use:   "add <title>",
	Short: "Create a new ticket",
	Long: `Create a new ticket.

Emoji in titles: prefer plain single-codepoint emoji (🔒 📦 🎯). Variation-selector
emoji (✏️ ⚠️ 🗄️), multi-part sequences (👍🏽 🇬🇧 #️⃣) and post-2018 emoji (🫠) measure
narrower in VS Code and Cursor than kanban lays out for, which skews that
ticket's board row; kanban prints a note when a title contains one.

No emoji survives every terminal — one carrying older width tables draws all of
them a cell narrow, whatever they are made of.`,
	Args: cobra.MinimumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		title := strings.Join(args, " ")

		statusStr, _ := cmd.Flags().GetString("status")
		status, err := model.ParseStatus(statusStr)
		if err != nil {
			return err
		}

		desc, _ := cmd.Flags().GetString("desc")
		tags, _ := cmd.Flags().GetStringSlice("tag")
		assignedTo, _ := cmd.Flags().GetString("assigned-to")
		createdBy, _ := cmd.Flags().GetString("created-by")

		ticket, err := st.Add(title, desc, status, tags, assignedTo, createdBy)
		if err != nil {
			return err
		}

		fmt.Printf("Created %s: %s\n", ticket.ShortID, ticket.Title)
		warnFragileEmoji(cmd, ticket.Title)
		return nil
	},
}

func init() {
	addCmd.Flags().String("desc", "", "Description")
	addCmd.Flags().String("status", "TODO", fmt.Sprintf("Status (%s)", statusValues()))
	addCmd.Flags().StringSlice("tag", nil, "Tags (repeatable)")
	addCmd.Flags().String("assigned-to", "", "Assigned agent or person")
	addCmd.Flags().String("created-by", "", "Creator name")
}
