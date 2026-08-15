// Package termwidth reconciles what the board lays out with what a terminal
// actually paints.
//
// lipgloss measures text with modern grapheme-aware tables, so it spends two
// cells on an emoji. Plenty of terminals spend one — xterm.js ships Unicode 6
// width tables by default, where every codepoint above the BMP is a single
// cell. A line containing one emoji then paints a cell short of the width it
// was built for, and its column borders slide left, dragging the rest of the
// row with them.
//
// The gap cannot be closed during layout. While the emoji is in the string,
// lipgloss's width minus the terminal's is a fixed number, and padding moves
// both totals together — a space is one cell to everyone. So the correction
// happens after layout instead: Compensate walks the finished frame and injects
// the missing cells directly after the glyph that owes them.
//
// That restores the grid rather than merely disguising it. A character's
// painted column becomes the sum of the terminal's widths before it plus the
// shortfalls injected before it — which is exactly the sum of lipgloss's
// widths, i.e. the column it was laid out at. Mouse coordinates and click
// zones therefore stay correct with no further adjustment.
package termwidth

import (
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/charmbracelet/x/ansi"
	"github.com/rivo/uniseg"
)

// Profile is how a terminal measures a grapheme cluster.
type Profile int

const (
	// Grapheme is a terminal that measures the way lipgloss does — one cell
	// per column of the whole cluster. Ghostty, iTerm2, WezTerm, and VS Code
	// or Cursor once they are on Unicode 11. Nothing to correct.
	Grapheme Profile = iota

	// Narrow is a terminal carrying pre-emoji width tables — xterm.js's
	// built-in Unicode 6 provider, which is Markus Kuhn's wcwidth: per
	// codepoint, zero for the combining marks, two for the East Asian ranges
	// it knows about, one for everything else.
	//
	// Everything else is the part that matters. Unicode 6 predates emoji being
	// wide, so the emoji blocks are one cell there and lipgloss spends two,
	// which is the shortfall this package hands back. A skin tone or a flag
	// comes out right by accident — two codepoints at one cell each happens to
	// equal the two cells lipgloss wanted.
	Narrow
)

// u6Wide is Unicode 6's double-width set, which is the East Asian ranges and
// nothing else. Modelling this rather than "one cell per codepoint" is not a
// refinement: CJK has been two cells in wcwidth since long before emoji, so
// calling 界 one cell injected a space after every ideograph and skewed exactly
// the rows this package exists to straighten. Only the emoji blocks are narrow
// here, and they are narrow because Unicode 6 has nothing to say about them.
func u6Wide(r rune) bool {
	switch {
	case r >= 0x1100 && r <= 0x115F, // Hangul Jamo, initial consonants
		r >= 0x2E80 && r <= 0xA4CF && r != 0x303F, // CJK radicals through Yi
		r >= 0xAC00 && r <= 0xD7A3,                // Hangul syllables
		r >= 0xF900 && r <= 0xFAFF,                // CJK compatibility ideographs
		r >= 0xFE10 && r <= 0xFE19,                // vertical forms
		r >= 0xFE30 && r <= 0xFE6F,                // CJK compatibility forms
		r >= 0xFF00 && r <= 0xFF60,                // fullwidth forms
		r >= 0xFFE0 && r <= 0xFFE6,                // fullwidth signs
		r >= 0x20000 && r <= 0x2FFFD,              // CJK extension B and later
		r >= 0x30000 && r <= 0x3FFFD:
		return true
	}
	return false
}

func (p Profile) String() string {
	if p == Narrow {
		return "narrow"
	}
	return "grapheme"
}

// ParseProfile reads the name a config or a flag carries.
func ParseProfile(s string) (Profile, bool) {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "", "grapheme", "wide":
		return Grapheme, true
	case "narrow":
		return Narrow, true
	}
	return Grapheme, false
}

// Cells is the width this profile's terminal spends on one grapheme cluster.
func (p Profile) Cells(cluster string) int {
	if p != Narrow {
		return ansi.StringWidth(cluster)
	}
	// Per codepoint. Variation selectors, the keycap mark, the zero-width
	// joiner and the combining marks take none — which is what makes `#️⃣` a
	// single cell here and `👍🏽` two.
	n := 0
	for _, r := range cluster {
		switch {
		case r == 0x200D, r == 0xFE0F, r == 0xFE0E, r == 0x20E3:
		case unicode.Is(unicode.Mn, r), unicode.Is(unicode.Me, r):
		case r >= 0xE0020 && r <= 0xE007F:
		case u6Wide(r):
			n += 2
		default:
			n++
		}
	}
	return n
}

// Shortfall is how many cells a cluster is owed: what the board laid out for,
// minus what the terminal will spend. Never negative — a cluster the terminal
// draws wider than lipgloss expected cannot be fixed by padding, and is left
// alone rather than made worse.
func (p Profile) Shortfall(cluster string) int {
	if n := ansi.StringWidth(cluster) - p.Cells(cluster); n > 0 {
		return n
	}
	return 0
}

// Reserve is how many cells per line to hold back from the layout so that the
// injected spaces have somewhere to go.
//
// Bubble Tea truncates any line wider than the window using lipgloss's own
// measurement, which would cut off exactly the cells Compensate added and undo
// the whole correction. Laying the board out this much narrower keeps every
// compensated line inside that limit; the cost is a few unused columns down
// the right-hand edge.
const Reserve = 8

// Compensate injects each cluster's shortfall directly after it, line by line,
// so the terminal paints the grid the frame was laid out on.
//
// Escape sequences are stepped over rather than measured, and each line is
// corrected independently up to budget cells.
//
// The budget is a hard ceiling, not a tuning knob. A line is laid out to
// termWidth-Reserve, so injecting more than Reserve cells pushes it past the
// window and Bubble Tea truncates it — cutting off the very cells just added.
// Reserve is therefore the number of disagreeing clusters one screen row can
// carry: 8, against a board of at most five columns whose titles lead with one
// emoji each. A row that exceeds it keeps the cells it got and stays short by
// the remainder, which is better than being truncated but is still the defect
// this package exists to remove. Raising Reserve buys headroom and costs that
// many columns of board on the terminals that need it.
func Compensate(frame string, p Profile, budget int) string {
	if p == Grapheme || budget <= 0 {
		return frame
	}
	lines := strings.Split(frame, "\n")
	for i, line := range lines {
		lines[i] = compensateLine(line, p, budget)
	}
	return strings.Join(lines, "\n")
}

func compensateLine(line string, p Profile, budget int) string {
	if isASCII(line) {
		return line // ASCII can never disagree, whatever the width tables say
	}

	var b strings.Builder
	b.Grow(len(line) + budget)
	spent, state := 0, -1
	rest := line
	for len(rest) > 0 {
		// Escape sequences carry no width; copy them across untouched so the
		// clusterer never sees them and never counts one as a cell.
		//
		// The parser state is dropped with them. Carrying it across an escape
		// hands uniseg a state describing text that no longer adjoins what
		// comes next, and it then breaks the following cluster in the wrong
		// place: `\x1b[37m🗄️` split into a bare base plus its selector, each
		// owed nothing, so the styled line kept none of its missing cells.
		// Styles wrap whole strings, so no cluster is ever split by one.
		if n := escapeLen(rest); n > 0 {
			b.WriteString(rest[:n])
			rest = rest[n:]
			state = -1
			continue
		}

		var cluster string
		cluster, rest, _, state = uniseg.FirstGraphemeClusterInString(rest, state)
		b.WriteString(cluster)
		if short := p.Shortfall(cluster); short > 0 && spent+short <= budget {
			b.WriteString(strings.Repeat(" ", short))
			spent += short
		}
	}
	return b.String()
}

// escapeLen is the length of the escape sequence starting s, or 0 if s does
// not start with one. Frames from lipgloss carry SGR (CSI ... m) and the
// occasional OSC; both are copied through untouched.
func escapeLen(s string) int {
	if len(s) < 2 || s[0] != 0x1b {
		return 0
	}
	switch s[1] {
	case '[': // CSI: parameters, then a final byte in 0x40..0x7E
		for i := 2; i < len(s); i++ {
			if s[i] >= 0x40 && s[i] <= 0x7E {
				return i + 1
			}
		}
		return len(s)
	case ']': // OSC: terminated by BEL or ST
		for i := 2; i < len(s); i++ {
			if s[i] == 0x07 {
				return i + 1
			}
			if s[i] == 0x1b && i+1 < len(s) && s[i+1] == '\\' {
				return i + 2
			}
		}
		return len(s)
	default:
		return 2
	}
}

// isASCII is the one cheap test that is also correct: every ASCII byte is a
// single cell under any width table, so such a line can never need correcting.
//
// A tempting-looking shortcut — "no rune above U+2FFF" — is wrong, and was:
// ⚡ is U+26A1 and ✅ is U+2705, so every BMP emoji sits below that line and
// would have been skipped.
func isASCII(s string) bool {
	for i := 0; i < len(s); i++ {
		if s[i] >= utf8.RuneSelf {
			return false
		}
	}
	return true
}
