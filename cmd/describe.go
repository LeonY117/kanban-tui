package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
)

// boardLabel names the board a command is scoped to, for messages.
func boardLabel() string {
	if name := st.BoardName(); name != "" {
		return name
	}
	return "main"
}

var describeCmd = &cobra.Command{
	Use:   "describe",
	Short: "Print or set the board's description",
	Long: `Print the current board's description, or replace it with --set.

A board's description is context about the board itself rather than any one
ticket: what this sprint is, what belongs on it, and anything an agent picking
up a ticket here should know. Think of it as a CLAUDE.md for the board.

The board is the one --sprint selects, so plain "kanban describe" reads the main
board's and "kanban --sprint demo describe" reads the sprint's.

Reading is the default: --set is the only way to write, so a stray argument
can't overwrite what's there. --set replaces the whole description, the same as
` + "`kanban update --desc`" + ` on a ticket — read it first if you mean to add
to it. To write something long, draft it in a file and pass it in:

    kanban describe --set "$(cat notes.md)"

Descriptions are capped; over the cap is an error rather than a silent trim, so
link a doc in the project repo instead of inlining a long one.`,
	Args: cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		if cmd.Flags().Changed("set") {
			desc, _ := cmd.Flags().GetString("set")
			if err := st.SetDescription(desc); err != nil {
				return err
			}
			if desc == "" {
				fmt.Printf("Cleared the description of %s.\n", boardLabel())
			} else {
				fmt.Printf("Updated the description of %s.\n", boardLabel())
			}
			return nil
		}

		board, err := st.Load()
		if err != nil {
			return err
		}
		if board.Description == "" {
			fmt.Printf("%s has no description. Set one with: kanban describe --set \"...\"\n", boardLabel())
			return nil
		}
		fmt.Println(board.Description)
		return nil
	},
}

func init() {
	describeCmd.Flags().String("set", "", "Replace the description with this text (empty string clears it)")
}
