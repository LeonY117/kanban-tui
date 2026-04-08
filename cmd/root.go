package cmd

import (
	"fmt"
	"os"

	"github.com/leon/kanban/internal/store"
	"github.com/spf13/cobra"
)

var st *store.Store

var rootCmd = &cobra.Command{
	Use:   "kanban",
	Short: "Terminal kanban board for humans and AI agents",
	Long:  "A terminal-based kanban board and task tracker. Run without subcommands to launch the TUI.",
	RunE: func(cmd *cobra.Command, args []string) error {
		return runTUI()
	},
}

func Execute() {
	st = store.New("")
	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func init() {
	rootCmd.AddCommand(addCmd)
	rootCmd.AddCommand(listCmd)
	rootCmd.AddCommand(showCmd)
	rootCmd.AddCommand(updateCmd)
}
