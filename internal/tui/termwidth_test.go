package tui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
	"github.com/rivo/uniseg"

	"github.com/LeonY117/kanban-tui/internal/termwidth"
)

// paintedWidth is the number of cells a terminal of this profile advances for
// a rendered line — what the user actually sees, as opposed to what lipgloss
// laid out.
func paintedWidth(t *testing.T, line string, p termwidth.Profile) int {
	t.Helper()
	rest, total, state := ansi.Strip(line), 0, -1
	for len(rest) > 0 {
		var cluster string
		cluster, rest, _, state = uniseg.FirstGraphemeClusterInString(rest, state)
		total += p.Cells(cluster)
	}
	return total
}

// withProfile points the package at a terminal profile for one test.
func withProfile(t *testing.T, p termwidth.Profile) {
	t.Helper()
	prev := widthProfile
	widthProfile = p
	t.Cleanup(func() { widthProfile = prev })
}

// emojiBoard is the seeded test board: safe-set emoji, the kind that broke in
// Codex while looking perfect in Ghostty.
func emojiBoard(t *testing.T) *Model {
	return testModel(t,
		"🐛 Duplicate rows after a partial retry",
		"📦 Split the worker image from the API image",
		"🔍 Rank exact title matches above body hits",
		"⚡ Cache the tenant lookup on the hot path",
		"🎨 Tighten the empty states across the app",
		"a title with no emoji at all",
	)
}

// The bug, stated as a test: on a narrow terminal the board's lines paint at
// different widths, which is what drags the column borders out of line.
func TestBoardIsRaggedOnANarrowTerminalWithoutCompensation(t *testing.T) {
	m := emojiBoard(t)
	withProfile(t, termwidth.Grapheme) // i.e. no correction applied
	m.width, m.height = 160, 40

	widths := map[int]bool{}
	for _, line := range strings.Split(m.View(), "\n") {
		widths[paintedWidth(t, line, termwidth.Narrow)] = true
	}
	if len(widths) == 1 {
		t.Fatal("setup: the uncompensated board was expected to paint ragged on a narrow terminal")
	}
}

// And the fix: with the narrow profile set, every line of the frame paints the
// same number of cells, so the borders line up.
func TestBoardPaintsEvenlyOnANarrowTerminal(t *testing.T) {
	m := emojiBoard(t)
	withProfile(t, termwidth.Narrow)
	// The window is 160 wide; the model holds back the reserve, as it does
	// from a WindowSizeMsg.
	m.width, m.height = 160-termwidth.Reserve, 40

	lines := strings.Split(m.View(), "\n")
	first := paintedWidth(t, lines[0], termwidth.Narrow)
	for i, line := range lines {
		if got := paintedWidth(t, line, termwidth.Narrow); got != first {
			t.Errorf("line %d paints %d cells, line 0 paints %d:\n%s", i, got, first, line)
		}
	}

	// And nothing may exceed the real window, or Bubble Tea truncates it and
	// undoes the correction.
	for i, line := range lines {
		if got := ansi.StringWidth(line); got > 160 {
			t.Errorf("line %d measures %d to lipgloss, over the 160-cell window", i, got)
		}
	}
}

// A grapheme terminal must be left exactly as it was — this is the terminal
// Leon actually works in, and it was never broken.
func TestGraphemeTerminalRenderIsUntouched(t *testing.T) {
	m := emojiBoard(t)
	withProfile(t, termwidth.Grapheme)
	m.width, m.height = 160, 40
	before := m.View()

	withProfile(t, termwidth.Narrow)
	m.width = 160 - termwidth.Reserve
	m.View() // exercise the compensated path

	withProfile(t, termwidth.Grapheme)
	m.width = 160
	if after := m.View(); after != before {
		t.Error("the grapheme render changed once a narrow profile existed")
	}
}

// Compensation restores the grid, so a click still lands on the zone that was
// registered for it — no coordinate translation needed.
func TestClickZonesSurviveCompensation(t *testing.T) {
	m := emojiBoard(t)
	withProfile(t, termwidth.Narrow)
	m.width, m.height = 160-termwidth.Reserve, 40
	m.View() // register zones

	z := zoneOf(t, m, zoneTicket, 1, 0)
	m.mouseClick(mouseAt(z.x, z.y))
	if m.cursors[1] != 0 || m.focusedCol != 1 {
		t.Errorf("click landed on col %d idx %d, want the zone it was registered at",
			m.focusedCol, m.cursors[1])
	}
}

// Compensation is per-cluster rather than per-table, so it reaches well past
// the safe set: variation selectors, post-Unicode-11 emoji, flags and skin
// tones all land right on a narrow terminal. What it cannot reach is the other
// direction — see TestOverWideClustersCannotBeCompensated.
func TestMostEmojiClassesPaintEvenlyOnANarrowTerminal(t *testing.T) {
	m := testModel(t,
		"🗄️ variation selector",
		"🫠 post-Unicode-11",
		"🇬🇧 regional indicators",
		"👍🏽 skin tone",
		"🐛 plain single codepoint",
		"no emoji at all",
	)
	withProfile(t, termwidth.Narrow)
	m.width, m.height = 160-termwidth.Reserve, 40

	lines := strings.Split(m.View(), "\n")
	first := paintedWidth(t, lines[0], termwidth.Narrow)
	for i, line := range lines {
		if got := paintedWidth(t, line, termwidth.Narrow); got != first {
			t.Errorf("line %d paints %d cells, line 0 paints %d:\n%s", i, got, first, line)
		}
	}
}

// The boundary of the whole approach, stated once so it is not rediscovered as
// a bug: injecting spaces can only ADD cells. A cluster the terminal draws
// WIDER than kanban laid out for cannot be fixed this way, and there are two.
//
//   - A ZWJ sequence on a narrow terminal: 👨‍👩‍👧 is three codepoints at one
//     cell each there, where lipgloss laid out two.
//   - A keycap on a grapheme terminal: kanban measures 1 (an x/ansi bug — the
//     ASCII base is counted outside the cluster) where the terminal draws 2.
//
// Both stay fragile, and internal/emoji still calls them so.
func TestOverWideClustersCannotBeCompensated(t *testing.T) {
	// A ZWJ family is three codepoints at one cell each on a narrow terminal,
	// where lipgloss laid out two. The profile can say so, and does.
	if got, want := termwidth.Narrow.Cells("👨‍👩‍👧"), ansi.StringWidth("👨‍👩‍👧"); got <= want {
		t.Errorf("Narrow.Cells(ZWJ family) = %d, lipgloss = %d — expected the terminal to draw it wider", got, want)
	}

	// The keycap is over-wide too, but only measurement knows it: Ghostty
	// advanced 2 where x/ansi reports 1, and the Grapheme profile is defined
	// as x/ansi, so it cannot model its own bug.
	if ansi.StringWidth("#️⃣") >= 2 {
		t.Skip("x/ansi has been fixed upstream — drop this test and the caveat with it")
	}

	// What matters for both: a negative shortfall clamps rather than padding,
	// because injecting spaces can only ever add cells.
	for _, tc := range []struct {
		cluster string
		profile termwidth.Profile
		name    string
	}{
		{"👨‍👩‍👧", termwidth.Narrow, "ZWJ sequence on a narrow terminal"},
		{"#️⃣", termwidth.Grapheme, "keycap on a grapheme terminal"},
	} {
		if got := tc.profile.Shortfall(tc.cluster); got != 0 {
			t.Errorf("%s: Shortfall = %d, want 0", tc.name, got)
		}
	}
}

// The two features met on main: the typeahead composites its popup onto the
// frame, and compensation then walks the finished frame. The popup is full of
// emoji and sits on top of already-padded board lines, so it is the one place
// where an overlay could leave a line the correction cannot square up. Order
// matters here — compensation has to be the last thing that touches the frame.
func TestTypeaheadPopupPaintsEvenlyOnANarrowTerminal(t *testing.T) {
	m := emojiBoard(t)
	withProfile(t, termwidth.Narrow)
	m.width, m.height = 160-termwidth.Reserve, 40

	m.Update(keyPress("a"))
	for _, k := range []string{":", "f", "i", "r", "e"} {
		m.Update(keyPress(k))
	}
	if !m.typeaheadShowing() {
		t.Fatal("setup: the popup should be on screen")
	}

	lines := strings.Split(m.View(), "\n")
	first := paintedWidth(t, lines[0], termwidth.Narrow)
	for i, line := range lines {
		if got := paintedWidth(t, line, termwidth.Narrow); got != first {
			t.Errorf("line %d paints %d cells, line 0 paints %d:\n%s", i, got, first, line)
		}
		if got := ansi.StringWidth(line); got > 160 {
			t.Errorf("line %d measures %d to lipgloss, over the 160-cell window", i, got)
		}
	}
}
