package cmd

import (
	"os"
	"strings"
	"testing"
	"time"
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
