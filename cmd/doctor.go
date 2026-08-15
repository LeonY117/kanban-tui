package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/charmbracelet/x/ansi"
	"github.com/charmbracelet/x/term"
	"github.com/spf13/cobra"

	"github.com/LeonY117/kanban-tui/internal/store"
	"github.com/LeonY117/kanban-tui/internal/termwidth"
)

// doctor exists because width tables are a guess about what a terminal will
// do, and the guess is wrong often enough to wreck a board. Rather than argue
// from Unicode data files, ask: print a glyph, then ask the terminal where the
// cursor ended up. That number is the only ground truth there is.
//
// It is a command rather than something the TUI does at startup because the
// answer is a property of the terminal, not of the board — you run it once per
// terminal you care about, and it tells you whether emoji are usable there.

// widthSample is one thing to measure, and why it is interesting.
type widthSample struct {
	Text string `json:"text"`
	Note string `json:"note"`
}

var widthSamples = []widthSample{
	{"ab", "two ASCII cells — the control"},
	{"⚡", "single codepoint, BMP"},
	{"🐛", "single codepoint, astral"},
	{"🧪", "single codepoint, added in Unicode 11"},
	{"🫠", "added after Unicode 11"},
	{"🗄️", "variation-selector sequence"},
	{"👨\u200d👩\u200d👧", "ZWJ sequence"},
	{"👍🏽", "skin-tone modifier"},
	{"🇬🇧", "regional indicators"},
	{"#️⃣", "keycap"},
}

type widthResult struct {
	widthSample
	Terminal int `json:"terminal"` // cells the terminal actually advanced
	Kanban   int `json:"kanban"`   // cells kanban lays out for
}

var doctorCmd = &cobra.Command{
	Use:   "doctor",
	Short: "Measure whether this terminal agrees with kanban about emoji width",
	Long: `Measure whether this terminal agrees with kanban about emoji width.

The board draws columns by padding every line to the same width. If the
terminal advances a different number of cells than kanban laid out for, that
line ends up short or long and its column borders shift — one bad character
skews the whole row, including every column to its right.

Which characters disagree is a property of the terminal, not of the emoji:
terminals carry width tables of different vintages, and the same emoji can be
one cell in one and two in another. So this asks the terminal directly rather
than consulting a table — it prints each sample and reads back where the
cursor landed.

Run it in each terminal you use the board in. It needs a real terminal, so it
cannot be piped.`,
	Args: cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		results, err := measureWidths()
		if err != nil {
			return err
		}

		if jsonOut, _ := cmd.Flags().GetBool("json"); jsonOut {
			out, err := json.MarshalIndent(map[string]any{
				"term":         os.Getenv("TERM"),
				"term_program": strings.TrimSpace(os.Getenv("TERM_PROGRAM") + " " + os.Getenv("TERM_PROGRAM_VERSION")),
				"samples":      results,
			}, "", "  ")
			if err != nil {
				return err
			}
			fmt.Fprintln(cmd.OutOrStdout(), string(out))
			return nil
		}

		w := cmd.OutOrStdout()
		name := os.Getenv("TERM")
		if p := strings.TrimSpace(os.Getenv("TERM_PROGRAM") + " " + os.Getenv("TERM_PROGRAM_VERSION")); p != "" {
			name += " (" + p + ")"
		}
		fmt.Fprintf(w, "Terminal: %s\n\n", name)
		fmt.Fprintf(w, "  %-6s %-9s %-7s\n", "", "terminal", "kanban")
		for _, r := range results {
			verdict := "ok"
			if r.Terminal != r.Kanban {
				verdict = fmt.Sprintf("off by %+d", r.Terminal-r.Kanban)
			}
			// Pad the sample from the width we just measured, not from a
			// width table — this is the one program that knows what these
			// glyphs really cost here, and a table would misalign the very
			// column that exists to expose misalignment.
			fmt.Fprintf(w, "  %s %-9d %-7d %-10s %s\n",
				padCells(r.Text, r.Terminal, 6), r.Terminal, r.Kanban, verdict, r.Note)
		}

		fmt.Fprintf(w, "\n%s\n", summarize(results))

		// Where cells are being lost, name the setting that hands them back.
		switch p, ok := recommend(results); {
		case ok && p != termwidth.Grapheme:
			fmt.Fprintf(w, "\nTo correct it, set the terminal profile in %s:\n\n    \"terminalWidth\": %q\n\n%s\n",
				store.ConfigPath(), p.String(),
				"The board then hands back the cells this terminal declines to spend,\n"+
					"and lays out a few columns narrower to make room for them.")
		case !ok:
			fmt.Fprintf(w, "\n%s\n",
				"No profile matches this terminal: some samples are short and others\n"+
					"are already right, so no single correction fits. Choosing one would\n"+
					"straighten some rows by bending the rest. Keep emoji out of titles\n"+
					"here, or move to a terminal on modern width tables.")
		}
		return nil
	},
}

// padCells pads s out to width, given that s really occupies cells columns in
// this terminal.
func padCells(s string, cells, width int) string {
	if n := width - cells; n > 0 {
		return s + strings.Repeat(" ", n)
	}
	return s + " "
}

// summarize says the one thing worth acting on. The pattern matters more than
// the count: a terminal short on every single emoji is carrying pre-2018 width
// tables — xterm.js still ships Unicode 6 by default, where every codepoint
// above the BMP is one cell — and that is a different problem from kanban
// mismeasuring one sequence.
func summarize(results []widthResult) string {
	var narrow, wide []string
	for _, r := range results {
		switch {
		case r.Terminal < r.Kanban:
			narrow = append(narrow, r.Text)
		case r.Terminal > r.Kanban:
			wide = append(wide, r.Text)
		}
	}

	switch {
	case len(narrow) == 0 && len(wide) == 0:
		return "Everything agrees. Emoji are safe in titles here."

	case len(narrow) > 0:
		s := fmt.Sprintf("This terminal draws %s in fewer cells than kanban lays out for, so\n"+
			"every board row containing one is short and its borders slide left.\n\n"+
			"That is the terminal's width tables rather than the board's arithmetic,\n"+
			"and it cannot be fixed while laying out — a space is one cell to both\n"+
			"sides, so padding moves both totals together. The board corrects the\n"+
			"finished frame instead, handing back the cells this terminal declines\n"+
			"to spend.", strings.Join(narrow, " "))
		if len(wide) > 0 {
			s += fmt.Sprintf("\n\nSeparately, kanban under-measures %s — that one is kanban's bug.",
				strings.Join(wide, " "))
		}
		return s

	default:
		return fmt.Sprintf("kanban measures %s narrower than this terminal draws it, so a title\n"+
			"containing one pushes its row wide. That one is kanban's bug rather\n"+
			"than the terminal's — everything else here agrees.", strings.Join(wide, " "))
	}
}

// recommend names the profile whose model matches every measurement, and
// reports false when none does.
//
// It used to offer `narrow` after any short sample, which is wrong in the case
// that matters most: a terminal on intermediate width tables draws older emoji
// at exactly the width kanban laid out for and only newer ones short. Narrow
// would then pad the ones that were already right, straightening some rows by
// bending others. A profile is a claim about a whole table, so it is only
// offered when the whole table agrees with it.
func recommend(results []widthResult) (termwidth.Profile, bool) {
	for _, p := range []termwidth.Profile{termwidth.Grapheme, termwidth.Narrow} {
		matches := true
		for _, r := range results {
			if r.Terminal != p.Cells(r.Text) {
				matches = false
				break
			}
		}
		if matches {
			return p, true
		}
	}
	return termwidth.Grapheme, false
}

// measureWidths prints each sample and reads back the cursor column. The
// terminal has to be raw for the reply to reach us rather than the line editor.
//
// Measurement traffic goes to /dev/tty rather than to stdout, because the
// natural way to use --json is `kanban doctor --json > widths.json`, and
// writing the cursor query to stdout then put the escape in the file, left the
// terminal with nothing to answer, and timed out once per sample.
func measureWidths() ([]widthResult, error) {
	tty, err := os.OpenFile("/dev/tty", os.O_RDWR, 0)
	if err != nil {
		return nil, fmt.Errorf("doctor measures the terminal it is attached to, so it needs one — run it directly")
	}
	defer tty.Close()

	fd := tty.Fd()
	if !term.IsTerminal(fd) {
		return nil, fmt.Errorf("doctor measures the terminal it is attached to, so it can't be piped or redirected — run it directly")
	}
	state, err := term.MakeRaw(fd)
	if err != nil {
		return nil, fmt.Errorf("putting the terminal in raw mode: %w", err)
	}
	defer term.Restore(fd, state)

	out := make([]widthResult, 0, len(widthSamples))
	for _, s := range widthSamples {
		// Back to column 1, clear, print the sample, ask where we ended up.
		fmt.Fprintf(tty, "\r\x1b[2K%s\x1b[6n", s.Text)
		col, err := readCursorColumn(tty, 2*time.Second)
		if err != nil {
			return nil, err
		}
		out = append(out, widthResult{
			widthSample: s,
			Terminal:    col - 1,
			Kanban:      ansi.StringWidth(s.Text),
		})
	}
	fmt.Fprint(tty, "\r\x1b[2K")
	return out, nil
}

// readCursorColumn reads one CPR reply, `ESC [ row ; col R`. The timeout is
// the point: a terminal that does not answer must cost a couple of seconds,
// not hang the command forever with the tty still in raw mode.
// A reply is read until the terminating R rather than in one call: a tty
// usually delivers the whole thing at once, but nothing promises it, and a read
// that returned just "\x1b[" would have been reported as an unparsable reply
// while the rest of it was still on the way.
func readCursorColumn(f *os.File, wait time.Duration) (int, error) {
	type read struct {
		b   []byte
		err error
	}
	ch := make(chan read, 1)
	go func() {
		buf := make([]byte, 32)
		for {
			n, err := f.Read(buf)
			ch <- read{append([]byte(nil), buf[:n]...), err}
			if err != nil {
				return
			}
		}
	}()

	deadline := time.After(wait)
	var reply []byte
	for {
		select {
		case r := <-ch:
			if r.err != nil {
				return 0, r.err
			}
			reply = append(reply, r.b...)
			if strings.ContainsRune(string(reply), 'R') {
				return parseCursorColumn(string(reply))
			}
		case <-deadline:
			return 0, fmt.Errorf("this terminal did not answer a cursor-position request, so its widths can't be measured")
		}
	}
}

// parseCursorColumn pulls the column out of `ESC [ row ; col R`. It reads up
// to the FIRST R and takes the last `;` before it: a terminal is free to send
// more than we asked for, and a device-attributes reply trailing the CPR has
// its own semicolon that would otherwise be mistaken for ours.
func parseCursorColumn(reply string) (int, error) {
	end := strings.IndexByte(reply, 'R')
	if end < 0 {
		return 0, fmt.Errorf("unparsable cursor reply %q", reply)
	}
	head := reply[:end]
	semi := strings.LastIndex(head, ";")
	if semi < 0 {
		return 0, fmt.Errorf("unparsable cursor reply %q", reply)
	}
	return strconv.Atoi(strings.TrimSpace(head[semi+1:]))
}

func init() {
	doctorCmd.Flags().Bool("json", false, "Output as JSON")
	rootCmd.AddCommand(doctorCmd)
}
