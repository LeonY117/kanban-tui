package cmd

import (
	"fmt"
	"time"

	"github.com/spf13/cobra"
)

var archiveCmd = &cobra.Command{
	Use:   "archive",
	Short: "Move done tickets to archive",
	RunE: func(cmd *cobra.Command, args []string) error {
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
