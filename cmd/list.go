package cmd

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/LeonY117/kanban-tui/internal/model"
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
				SortByPriority(filtered)
				for _, t := range filtered {
					printTicketLine(t)
				}
			}
			fmt.Println()
		} else {
			SortByPriority(tickets)
			for _, t := range tickets {
				printTicketLine(t)
			}
		}
		return nil
	},
}

// SortByPriority sorts tickets by priority, descending.
// showing the highest priority tickets first when using "kanban list"
func SortByPriority(tickets []model.Ticket) {
	sort.Slice(tickets, func(i, j int) bool {
		return tickets[i].Priority > tickets[j].Priority
	})
}

// truncate truncates a string to a maximum length
func truncate(s string, max int) string {
	if len(s) > max {
		return s[:max-3] + "..."
	}
	return s
}

func printTicketLine(t model.Ticket) {
	title := truncate(t.Title, 10)
	parts := []string{fmt.Sprintf("|%-4s|%-10s|P%-3d ", t.ShortID, title, t.Priority)}
	if len(t.Tags) > 0 {
		parts = append(parts, fmt.Sprintf(" [%s]", strings.Join(t.Tags, ", ")))
	}
	if t.AssignedTo != "" {
		parts = append(parts, fmt.Sprintf(" → %s", t.AssignedTo))
	}
	fmt.Println(strings.Join(parts, " "))
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
	listCmd.Flags().String("status", "", fmt.Sprintf("Filter by status (%s)", statusValues()))
	listCmd.Flags().String("tag", "", "Filter by tag")
	listCmd.Flags().String("assigned-to", "", "Filter by assignee")
	listCmd.Flags().Bool("json", false, "Output as JSON")
}
