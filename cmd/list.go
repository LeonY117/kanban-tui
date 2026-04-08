package cmd

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/leon/kanban/internal/model"
	"github.com/spf13/cobra"
)

var listCmd = &cobra.Command{
	Use:   "list",
	Short: "List tickets",
	RunE: func(cmd *cobra.Command, args []string) error {
		board, err := st.Load()
		if err != nil {
			return err
		}

		opts := model.FilterOptions{}

		if s, _ := cmd.Flags().GetString("status"); s != "" {
			status, err := model.ParseStatus(s)
			if err != nil {
				return err
			}
			opts.Status = &status
		}
		if t, _ := cmd.Flags().GetString("tag"); t != "" {
			opts.Tag = t
		}
		if a, _ := cmd.Flags().GetString("assigned-to"); cmd.Flags().Changed("assigned-to") {
			opts.AssignedTo = &a
		}

		tickets := board.Filter(opts)

		asJSON, _ := cmd.Flags().GetBool("json")
		if asJSON {
			data, err := json.MarshalIndent(tickets, "", "  ")
			if err != nil {
				return err
			}
			fmt.Println(string(data))
			return nil
		}

		if len(tickets) == 0 {
			fmt.Println("No tickets found.")
			return nil
		}

		// Group by status for display
		if opts.Status == nil {
			for _, status := range model.ColumnOrder {
				group := board.Filter(model.FilterOptions{Status: &status})
				// Re-apply other filters
				var filtered []model.Ticket
				for _, t := range group {
					if opts.Tag != "" && !tagsContain(t.Tags, opts.Tag) {
						continue
					}
					if opts.AssignedTo != nil && t.AssignedTo != *opts.AssignedTo {
						continue
					}
					filtered = append(filtered, t)
				}
				if len(filtered) == 0 {
					continue
				}
				fmt.Printf("\n%s (%d)\n", status, len(filtered))
				fmt.Println(strings.Repeat("─", 40))
				for _, t := range filtered {
					printTicketLine(t)
				}
			}
			fmt.Println()
		} else {
			for _, t := range tickets {
				printTicketLine(t)
			}
		}
		return nil
	},
}

func printTicketLine(t model.Ticket) {
	parts := []string{fmt.Sprintf("  %s  %s", t.ShortID, t.Title)}
	if len(t.Tags) > 0 {
		parts = append(parts, fmt.Sprintf(" [%s]", strings.Join(t.Tags, ", ")))
	}
	if t.AssignedTo != "" {
		parts = append(parts, fmt.Sprintf(" → %s", t.AssignedTo))
	}
	fmt.Println(strings.Join(parts, ""))
}

func tagsContain(tags []string, tag string) bool {
	tag = strings.ToLower(tag)
	for _, t := range tags {
		if strings.ToLower(t) == tag {
			return true
		}
	}
	return false
}

func init() {
	listCmd.Flags().String("status", "", "Filter by status")
	listCmd.Flags().String("tag", "", "Filter by tag")
	listCmd.Flags().String("assigned-to", "", "Filter by assignee")
	listCmd.Flags().Bool("json", false, "Output as JSON")
}
