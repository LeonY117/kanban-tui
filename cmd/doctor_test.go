package cmd

import (
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"

	"github.com/LeonY117/kanban-tui/internal/termwidth"
)

func TestParseCursorColumn(t *testing.T) {
	cases := []struct {
		name, reply string
		want        int
		wantErr     bool
	}{
		{"ordinary reply", "\x1b[12;3R", 3, false},
		{"first column", "\x1b[1;1R", 1, false},
		{"wide column", "\x1b[40;181R", 181, false},
		// Terminals answer asynchronously, so the reply can arrive with
		// whatever else was in the buffer around it.
		{"padded reply", "\x1b[7;2R\x1b[?1;2c", 2, false},
		{"truncated", "\x1b[12;", 0, true},
		{"nothing at all", "", 0, true},
		{"not a number", "\x1b[12;xR", 0, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := parseCursorColumn(tc.reply)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("parseCursorColumn(%q) = %d, want an error", tc.reply, got)
				}
				return
			}
			if err != nil {
				t.Fatalf("parseCursorColumn(%q): %v", tc.reply, err)
			}
			if got != tc.want {
				t.Errorf("parseCursorColumn(%q) = %d, want %d", tc.reply, got, tc.want)
			}
		})
	}
}

// A terminal that ignores the request must cost a moment and a clear message,
// never a hang with the tty left in raw mode.
func TestReadCursorColumnTimesOut(t *testing.T) {
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	defer w.Close()

	start := time.Now()
	_, err = readCursorColumn(r, 50*time.Millisecond)
	if err == nil {
		t.Fatal("a silent terminal should be an error, not a hang")
	}
	if !strings.Contains(err.Error(), "did not answer") {
		t.Errorf("error = %q, want it to name the cause", err)
	}
	if elapsed := time.Since(start); elapsed > time.Second {
		t.Errorf("took %s to give up, want the timeout to bound it", elapsed)
	}
}

// The command is a measurement of the attached terminal, so it has to refuse
// rather than report nonsense when there isn't one — that is how it will be
// run under an agent.
func TestDoctorRefusesWithoutATerminal(t *testing.T) {
	if _, err := measureWidths(); err == nil {
		t.Fatal("expected a refusal when stdin is not a terminal")
	} else if !strings.Contains(err.Error(), "run it directly") {
		t.Errorf("error = %q, want it to say what to do instead", err)
	}
}

// The sample column is padded from the measured width, because a width table
// would misalign the very column that exists to expose misalignment.
func TestPadCellsUsesMeasuredWidth(t *testing.T) {
	// "🗄️" is two runes but one cell in a Unicode-6 terminal; fmt's %-6s would
	// pad it to four trailing spaces and pull the row a cell left.
	if got, want := padCells("🗄️", 1, 6), "🗄️     "; got != want {
		t.Errorf("padCells(1 cell) = %q, want %q", got, want)
	}
	if got, want := padCells("🗄️", 2, 6), "🗄️    "; got != want {
		t.Errorf("padCells(2 cells) = %q, want %q", got, want)
	}
	if got := padCells("wide-sample", 11, 6); got != "wide-sample " {
		t.Errorf("an oversized sample should still be separated, got %q", got)
	}
}

func TestSummarizeNamesTheCulprit(t *testing.T) {
	all := []widthResult{{widthSample{"ab", ""}, 2, 2}, {widthSample{"🐛", ""}, 2, 2}}
	if got := summarize(all); !strings.Contains(got, "Everything agrees") {
		t.Errorf("agreement summary = %q", got)
	}

	// The measured Codex profile: emoji short, keycap fine.
	codex := []widthResult{
		{widthSample{"ab", ""}, 2, 2},
		{widthSample{"🐛", ""}, 1, 2},
		{widthSample{"🧪", ""}, 1, 2},
	}
	got := summarize(codex)
	for _, want := range []string{"🐛", "🧪", "terminal's width tables", "corrects the"} {
		if !strings.Contains(got, want) {
			t.Errorf("narrow summary should mention %q:\n%s", want, got)
		}
	}

	// The measured Ghostty profile: only the keycap, and it is our bug.
	ghostty := []widthResult{
		{widthSample{"ab", ""}, 2, 2},
		{widthSample{"#️⃣", ""}, 2, 1},
	}
	got = summarize(ghostty)
	if !strings.Contains(got, "kanban's bug") || strings.Contains(got, "corrects the") {
		t.Errorf("a lone over-wide sample is kanban's bug, not the terminal's:\n%s", got)
	}
}

// recommend offers a profile only when the whole measured table agrees with
// it. Offering narrow off any single short sample told a partly-modern
// terminal to pad the emoji it was already drawing correctly.
func TestRecommendNeedsTheWholeTableToAgree(t *testing.T) {
	measured := func(f func(widthSample) int) []widthResult {
		out := make([]widthResult, 0, len(widthSamples))
		for _, s := range widthSamples {
			out = append(out, widthResult{widthSample: s, Terminal: f(s), Kanban: ansi.StringWidth(s.Text)})
		}
		return out
	}

	// Ghostty: agrees with kanban about everything.
	if p, ok := recommend(measured(func(s widthSample) int { return ansi.StringWidth(s.Text) })); !ok || p != termwidth.Grapheme {
		t.Errorf("a terminal that agrees should resolve to grapheme, got %v ok=%v", p, ok)
	}

	// Codex / xterm.js: Unicode 6 throughout — the case the profile exists for.
	if p, ok := recommend(measured(func(s widthSample) int { return termwidth.Narrow.Cells(s.Text) })); !ok || p != termwidth.Narrow {
		t.Errorf("a Unicode 6 terminal should resolve to narrow, got %v ok=%v", p, ok)
	}

	// Partly modern: older emoji already right, newer ones short. Neither
	// profile fits, and narrow would make the correct rows over-wide.
	partial := measured(func(s widthSample) int {
		if s.Text == "🫠" {
			return 1
		}
		return ansi.StringWidth(s.Text)
	})
	if _, ok := recommend(partial); ok {
		t.Error("no profile should be offered when only some samples disagree")
	}
	if !hasShortMeasurement(partial) {
		t.Error("a short sample with no matching profile still needs profile advice")
	}

	// Ghostty agrees with the Grapheme profile on every correctable sample,
	// but draws the known-unfixable keycap wider than x/ansi. That is still no
	// exact whole-table match; it just must not trigger advice about short rows.
	modern := measured(func(s widthSample) int { return ansi.StringWidth(s.Text) })
	for i := range modern {
		if modern[i].Text == "#️⃣" {
			modern[i].Terminal = modern[i].Kanban + 1
		}
	}
	if _, ok := recommend(modern); ok {
		t.Error("the keycap mismatch means the whole table does not match")
	}
	if hasShortMeasurement(modern) {
		t.Error("an over-wide-only mismatch cannot be corrected by a profile")
	}
}

// A CPR reply can arrive in pieces. Parsing the first read alone reported an
// unparsable reply while the rest of it was still in flight.
func TestCursorColumnAcceptsASplitReply(t *testing.T) {
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe: %v", err)
	}
	defer r.Close()

	go func() {
		defer w.Close()
		w.Write([]byte("\x1b["))
		time.Sleep(10 * time.Millisecond)
		w.Write([]byte("12;3R"))
	}()

	col, err := readCursorColumn(r, 2*time.Second)
	if err != nil {
		t.Fatalf("readCursorColumn: %v", err)
	}
	if col != 3 {
		t.Errorf("column = %d, want 3", col)
	}
}

// A completed read must release the pipe before the next measurement starts.
// Leaving its goroutine blocked in another Read lets it steal the next CPR.
func TestCursorColumnDoesNotLeaveAReaderBehind(t *testing.T) {
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe: %v", err)
	}
	defer r.Close()
	defer w.Close()

	if _, err := w.Write([]byte("\x1b[1;2R")); err != nil {
		t.Fatalf("write first reply: %v", err)
	}
	if col, err := readCursorColumn(r, time.Second); err != nil || col != 2 {
		t.Fatalf("first reply: column=%d err=%v", col, err)
	}

	done := make(chan error, 1)
	go func() {
		col, err := readCursorColumn(r, time.Second)
		if err == nil && col != 3 {
			err = fmt.Errorf("column = %d, want 3", col)
		}
		done <- err
	}()
	time.Sleep(10 * time.Millisecond)
	if _, err := w.Write([]byte("\x1b[1;3R")); err != nil {
		t.Fatalf("write second reply: %v", err)
	}
	if err := <-done; err != nil {
		t.Fatalf("second reply: %v", err)
	}
}
