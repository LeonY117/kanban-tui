package cmd

import (
	"fmt"
	"time"

	"github.com/spf13/cobra"
)

var archiveCmd = &cobra.Command{
	Use:   "archive [id]",
	Short: "Move tickets to archive",
	Long:  "Archive a single ticket by ID (any status), or archive all done tickets when no ID is given.",
	Args:  cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		// Single ticket archive
		if len(args) == 1 {
			if err := st.ArchiveByID(args[0]); err != nil {
				return err
			}
			fmt.Printf("Archived ticket %s.\n", args[0])
			return nil
		}

		// Bulk archive all DONE tickets
		var before *time.Time

		if cmd.Flags().Changed("before") {
			s, _ := cmd.Flags().GetString("before")
			t, err := time.Parse("2006-01-02", s)
			if err != nil {
				return fmt.Errorf("invalid date %q, use YYYY-MM-DD format", s)
			}
			// Set to end of day
			t = t.Add(24*time.Hour - time.Nanosecond)
			before = &t
		}

		count, err := st.Archive(before)
		if err != nil {
			return err
		}

		if count == 0 {
			fmt.Println("No done tickets to archive.")
		} else {
			fmt.Printf("Archived %d ticket(s).\n", count)
		}
		return nil
	},
}

func init() {
	archiveCmd.Flags().String("before", "", "Only archive tickets done before this date (YYYY-MM-DD)")
}
