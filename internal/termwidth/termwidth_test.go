package termwidth

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
	"github.com/rivo/uniseg"
)

func firstCluster(s string, state int) (string, string, int, int) {
	return uniseg.FirstGraphemeClusterInString(s, state)
}

// Real measurements from each terminal, which is what the two profiles exist
// to model. Taken by printing each glyph and reading back the cursor column —
// `go run ./tools/doctor` still does that, and is how to check these against a
// terminal that disagrees. It is deliberately not part of the shipped binary.
func TestProfileMatchesMeasuredTerminals(t *testing.T) {
	cases := []struct {
		text           string
		ghostty, codex int
		note           string
	}{
		{"ab", 2, 2, "ASCII agrees everywhere"},
		{"⚡", 2, 1, "BMP emoji"},
		{"🐛", 2, 1, "astral emoji"},
		{"🧪", 2, 1, "Unicode 11 emoji"},
		{"🫠", 2, 1, "post-Unicode 11 emoji"},
		{"🗄️", 2, 1, "variation-selector sequence"},
		{"👍🏽", 2, 2, "base plus modifier — two codepoints, two cells either way"},
		{"🇬🇧", 2, 2, "two regional indicators"},
		{"#️⃣", 2, 1, "keycap: ASCII base, two zero-width marks"},
	}
	for _, tc := range cases {
		if got := Narrow.Cells(tc.text); got != tc.codex {
			t.Errorf("Narrow.Cells(%s) = %d, want %d (%s)", tc.text, got, tc.codex, tc.note)
		}
		// Ghostty agreed with lipgloss on everything except the keycap, which
		// is an x/ansi bug (it counts the ASCII base outside the cluster), not
		// something a profile should model.
		if tc.text == "#️⃣" {
			continue
		}
		if got := Grapheme.Cells(tc.text); got != tc.ghostty {
			t.Errorf("Grapheme.Cells(%s) = %d, want %d (%s)", tc.text, got, tc.ghostty, tc.note)
		}
	}
}

func TestShortfallIsNeverNegative(t *testing.T) {
	// Narrow spends 2 on a flag and so does lipgloss: nothing owed.
	if got := Narrow.Shortfall("🇬🇧"); got != 0 {
		t.Errorf("Shortfall(🇬🇧) = %d, want 0", got)
	}
	if got := Narrow.Shortfall("🐛"); got != 1 {
		t.Errorf("Shortfall(🐛) = %d, want 1", got)
	}
	// The keycap is the case where lipgloss under-measures; padding cannot fix
	// that and must not try.
	if got := Narrow.Shortfall("#️⃣"); got < 0 {
		t.Errorf("Shortfall(#️⃣) = %d, want no negative", got)
	}
}

// The invariant the whole package exists for: after compensation every line
// paints the same number of cells in the target terminal.
func TestCompensateMakesLinesPaintEqually(t *testing.T) {
	// Two rows laid out to the same lipgloss width, one carrying an emoji.
	frame := strings.Join([]string{
		"│ 🐛 fix login   │",
		"│ plain text     │",
		"│ 🚀 ship it 🔥  │",
	}, "\n")

	for _, line := range strings.Split(frame, "\n") {
		if got, want := ansi.StringWidth(line), 18; got != want {
			t.Fatalf("setup: %q is %d wide by lipgloss, want %d", line, got, want)
		}
	}

	got := Compensate(frame, Narrow, Reserve)
	var painted []int
	for _, line := range strings.Split(got, "\n") {
		painted = append(painted, paintedWidth(line, Narrow))
	}
	for i, w := range painted {
		if w != painted[0] {
			t.Errorf("line %d paints %d cells, line 0 paints %d — still ragged:\n%s",
				i, w, painted[0], got)
		}
	}
	// And it should paint at the width it was laid out for.
	if painted[0] != 18 {
		t.Errorf("painted width = %d, want the laid-out 18", painted[0])
	}
}

// A grapheme terminal needs no correction, and must not be given one.
func TestCompensateIsAnNoOpForGraphemeTerminals(t *testing.T) {
	frame := "│ 🐛 fix login │"
	if got := Compensate(frame, Grapheme, Reserve); got != frame {
		t.Errorf("Compensate on a grapheme terminal changed the frame:\n%q", got)
	}
}

// Styled text is the normal case in this app; the escape sequences carry no
// width and must survive untouched.
func TestCompensatePreservesEscapeSequences(t *testing.T) {
	frame := "\x1b[31m🐛\x1b[0m fix"
	got := Compensate(frame, Narrow, Reserve)
	if !strings.Contains(got, "\x1b[31m") || !strings.Contains(got, "\x1b[0m") {
		t.Errorf("escape sequences were mangled: %q", got)
	}
	if !strings.Contains(got, "🐛 ") {
		t.Errorf("the shortfall should sit directly after the glyph: %q", got)
	}
	if ansi.StringWidth(got) != ansi.StringWidth(frame)+1 {
		t.Errorf("width grew by %d, want 1", ansi.StringWidth(got)-ansi.StringWidth(frame))
	}
}

// Past its budget a line must stop rather than overflow the window, because a
// line Bubble Tea truncates is worse than one that is short.
func TestCompensateRespectsItsBudget(t *testing.T) {
	frame := "🐛🐛🐛🐛"
	got := Compensate(frame, Narrow, 2)
	if n := ansi.StringWidth(got) - ansi.StringWidth(frame); n != 2 {
		t.Errorf("injected %d cells, want the budget of 2", n)
	}
}

func TestParseProfile(t *testing.T) {
	for _, s := range []string{"", "grapheme", "wide", "GRAPHEME"} {
		if p, ok := ParseProfile(s); !ok || p != Grapheme {
			t.Errorf("ParseProfile(%q) = %v, %v", s, p, ok)
		}
	}
	if p, ok := ParseProfile("narrow"); !ok || p != Narrow {
		t.Errorf("ParseProfile(narrow) = %v, %v", p, ok)
	}
	if _, ok := ParseProfile("unicode6"); ok {
		t.Error("an unknown profile should be reported, not guessed at")
	}
}

// paintedWidth is what the target terminal would advance for a rendered line.
func paintedWidth(line string, p Profile) int {
	stripped := ansi.Strip(line)
	total, state := 0, -1
	for len(stripped) > 0 {
		var cluster string
		cluster, stripped, _, state = firstCluster(stripped, state)
		total += p.Cells(cluster)
	}
	return total
}

// BMP emoji are the ones a naive "is there anything above U+2FFF" fast path
// silently skips: ⚡ is U+26A1, ✅ is U+2705, both below any plausible
// threshold and both a cell short on a narrow terminal.
func TestCompensateCatchesBMPEmoji(t *testing.T) {
	for _, e := range []string{"⚡", "✅", "⭐", "🐛"} {
		line := "│ " + e + " x │"
		got := Compensate(line, Narrow, Reserve)
		if paintedWidth(got, Narrow) != ansi.StringWidth(line) {
			t.Errorf("%s: paints %d, laid out for %d — not compensated",
				e, paintedWidth(got, Narrow), ansi.StringWidth(line))
		}
	}
}

// The narrow profile models Unicode 6, which is the East Asian ranges plus
// nothing for emoji. Reading it as "one cell per codepoint" instead made CJK a
// single cell, so a Japanese title had a space injected after every ideograph
// and skewed the very rows this package straightens.
func TestNarrowProfileLeavesEastAsianWidthAlone(t *testing.T) {
	for _, s := range []string{"界", "日本語", "한국", "ａ", "〈", "〉", "→", "…", "é", "ab"} {
		if got := Narrow.Shortfall(s); got != 0 {
			t.Errorf("Narrow.Shortfall(%q) = %d, want 0 — wcwidth has agreed about this since before emoji", s, got)
		}
	}
	// And the emoji it exists for are still owed their cell.
	for _, s := range []string{"🐛", "⚡", "🫠", "🗄️"} {
		if got := Narrow.Shortfall(s); got != 1 {
			t.Errorf("Narrow.Shortfall(%q) = %d, want 1", s, got)
		}
	}
}

// Styled text is the normal case on a rendered board, and the grapheme parser
// state must not survive the escape that precedes it: carried across, uniseg
// broke `🗄️` into a bare base plus its selector, each owed nothing, and the
// line silently kept none of its missing cells.
func TestCompensateSeesClustersAfterStyling(t *testing.T) {
	for _, line := range []string{
		"ab\x1b[37m🗄️cd",
		"\x1b[1;32m🐛\x1b[0m a ticket",
		"│ \x1b[38;5;240m🫠\x1b[0m │",
	} {
		got := compensateLine(line, Narrow, Reserve)
		painted := 0
		rest, state := got, -1
		for len(rest) > 0 {
			if n := escapeLen(rest); n > 0 {
				rest = rest[n:]
				continue
			}
			var cluster string
			cluster, rest, _, state = firstCluster(rest, state)
			painted += Narrow.Cells(cluster)
		}
		if want := ansi.StringWidth(line); painted != want {
			t.Errorf("%q paints %d cells, laid out for %d", line, painted, want)
		}
	}
}
