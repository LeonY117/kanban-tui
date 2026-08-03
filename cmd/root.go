package cmd

import (
	"bufio"
	"fmt"
	"os"
	"strings"

	"github.com/LeonY117/kanban-tui/internal/model"
	"github.com/LeonY117/kanban-tui/internal/store"
	"github.com/spf13/cobra"
)

var st *store.Store

var rootCmd = &cobra.Command{
	Use:   "kanban",
	Short: "Terminal kanban board for humans and AI agents",
	Long: `A terminal kanban board and task tracker. Run without a subcommand to launch the TUI.

A ticket carries a status (BACKLOG, TODO, DOING, DONE, HOLD), title, description, tags and an
assignee. Tickets live on boards: the main board plus any number of named sprint boards, each
reached with --sprint <name>. Ids are short and per-board — 42 on main, KA7 on a sprint.

  kanban list --json                                     read the board
  kanban show <id> --json                                read one ticket
  kanban add "Title" --tag <tag> --desc "context"        create
  kanban update <id> --status DOING --assigned-to <who>  change fields
  kanban move <id> <board>                               send to another board
  kanban archive <id>                                    archive one, or all DONE if bare

Boards are JSON under ~/.kanban, or beside KANBAN_FILE when that is set. Writes take a lock,
so concurrent callers are safe.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		sprint, _ := cmd.Flags().GetString("sprint")
		return runTUI(sprint)
	},
}

func Execute() {
	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, "Error:", err)
		os.Exit(1)
	}
}

// statusValues lists the valid --status values straight from the model, so the
// four commands that take one can't drift from ParseStatus or from each other.
func statusValues() string {
	names := make([]string, len(model.AllStatuses))
	for i, s := range model.AllStatuses {
		names[i] = string(s)
	}
	return strings.Join(names, ", ")
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

	if err := store.CreateSprint(sprint, ""); err != nil {
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
	// Execute() already prints the error. Without these, cobra prints it a
	// second time and dumps the full usage block after it — three screens of
	// noise around one line an agent needs to read.
	rootCmd.SilenceErrors = true
	rootCmd.SilenceUsage = true

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
	rootCmd.AddCommand(moveCmd)
	rootCmd.AddCommand(archiveCmd)
	rootCmd.AddCommand(sprintsCmd)
}
