package cmd

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/spf13/cobra"
)

var showCmd = &cobra.Command{
	Use:   "show <id>",
	Short: "Show ticket details",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		board, err := st.Load()
		if err != nil {
			return err
		}

		t, _ := board.FindByID(args[0])
		if t == nil {
			return fmt.Errorf("ticket not found: %s", args[0])
		}

		asJSON, _ := cmd.Flags().GetBool("json")
		if asJSON {
			data, err := json.MarshalIndent(t, "", "  ")
			if err != nil {
				return err
			}
			fmt.Println(string(data))
			return nil
		}

		fmt.Printf("ID:        %s (%s)\n", t.ShortID, t.ID)
		fmt.Printf("Title:     %s\n", t.Title)
		fmt.Printf("Status:    %s\n", t.Status)
		if len(t.Tags) > 0 {
			fmt.Printf("Tags:      %s\n", strings.Join(t.Tags, ", "))
		}
		if t.AssignedTo != "" {
			fmt.Printf("Assigned:  %s\n", t.AssignedTo)
		}
		if t.CreatedBy != "" {
			fmt.Printf("Created by: %s\n", t.CreatedBy)
		}
		fmt.Printf("Created:   %s\n", t.CreatedAt.Format("2006-01-02 15:04"))
		fmt.Printf("Updated:   %s\n", t.UpdatedAt.Format("2006-01-02 15:04"))
		if t.Description != "" {
			fmt.Printf("\n%s\n", t.Description)
		}
		return nil
	},
}

func init() {
	showCmd.Flags().Bool("json", false, "Output as JSON")
}
