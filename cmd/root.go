package cmd

import (
	"bufio"
	"fmt"
	"os"
	"strings"

	"github.com/leon/kanban/internal/store"
	"github.com/spf13/cobra"
)

var st *store.Store

var rootCmd = &cobra.Command{
	Use:   "kanban",
	Short: "Terminal kanban board for humans and AI agents",
	Long:  "A terminal-based kanban board and task tracker. Run without subcommands to launch the TUI.",
	RunE: func(cmd *cobra.Command, args []string) error {
		sprint, _ := cmd.Flags().GetString("sprint")
		return runTUI(sprint)
	},
}

func Execute() {
	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

// promptYN returns true only on "y"/"yes" (case-insensitive). EOF, empty, and
// non-TTY stdin with no data are all treated as no — so an agent piping into a
// subcommand can never accidentally trip a prompt into creating/deleting state.
func promptYN(question string) bool {
	fmt.Print(question)
	reader := bufio.NewReader(os.Stdin)
	line, _ := reader.ReadString('\n')
	reply := strings.ToLower(strings.TrimSpace(line))
	return reply == "y" || reply == "yes"
}

// resolveStore sets `st` from the --sprint flag. The TUI entrypoint (cmd ==
// rootCmd) prompts y/N when the sprint doesn't exist; subcommands hard-error
// instead so agents/scripts can't hang on a prompt or silently create a typo'd sprint.
func resolveStore(cmd *cobra.Command) error {
	sprint, _ := cmd.Flags().GetString("sprint")

	if sprint == "" {
		st = store.New("")
		return nil
	}

	if err := store.ValidateSprintName(sprint); err != nil {
		return err
	}

	if store.SprintExists(sprint) {
		s, err := store.NewSprint(sprint)
		if err != nil {
			return err
		}
		st = s
		return nil
	}

	if cmd != rootCmd {
		return fmt.Errorf("sprint %q doesn't exist. Create with: kanban --sprint %s (or: kanban sprints new %s)", sprint, sprint, sprint)
	}

	if !promptYN(fmt.Sprintf("Sprint %q doesn't exist. Create it? [y/N]: ", sprint)) {
		fmt.Println("Aborted.")
		os.Exit(0)
	}

	if err := store.CreateSprint(sprint); err != nil {
		return err
	}
	fmt.Printf("Created sprint %q.\n", sprint)

	s, err := store.NewSprint(sprint)
	if err != nil {
		return err
	}
	st = s
	return nil
}

func init() {
	rootCmd.PersistentFlags().String("sprint", "", "Use a named sprint board instead of the main board")

	rootCmd.PersistentPreRunE = func(cmd *cobra.Command, args []string) error {
		return resolveStore(cmd)
	}

	// sprintsCmd and its children manage sprints themselves — they don't need
	// `st` resolved. This no-op PreRunE overrides the one inherited from rootCmd.
	sprintsCmd.PersistentPreRunE = func(cmd *cobra.Command, args []string) error {
		return nil
	}

	rootCmd.AddCommand(addCmd)
	rootCmd.AddCommand(listCmd)
	rootCmd.AddCommand(showCmd)
	rootCmd.AddCommand(updateCmd)
	rootCmd.AddCommand(archiveCmd)
	rootCmd.AddCommand(sprintsCmd)
}
