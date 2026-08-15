// Package emoji classifies emoji by whether terminals agree on their width.
// The board measures text with modern grapheme-aware tables (lipgloss); most
// terminals carry an older per-codepoint table, and any sequence the two
// disagree on shifts an entire board row by a cell.
//
// There are two failure modes and this package only addresses the first.
// Terminals on Unicode 11 tables — VS Code and Cursor, which default
// terminal.integrated.unicodeVersion to "11" — measure a variation-selector
// sequence, a post-2018 emoji or a ZWJ cluster narrower than lipgloss does,
// and Fragile reports exactly those. Terminals still on Unicode 6 — which is
// what bare xterm.js ships, so an app embedding it without loading
// addon-unicode11 gets it — return 1 for every codepoint above the BMP, so
// they draw *every* emoji a cell narrow and nothing in here helps.
package emoji

import (
	"unicode"

	"github.com/rivo/uniseg"
)

// Fragile returns the emoji in s whose cell width is terminal-dependent, in
// order of first appearance, deduplicated. Fragile means any of: a pictograph
// outside the frozen safe set (variation-selector emoji like ✏️, post-2018
// emoji like 🫠), or a multi-codepoint sequence (ZWJ families, flags,
// keycaps, skin tones). Safe-set emoji (🔒 📦 🎯) return nothing.
func Fragile(s string) []string {
	var out []string
	seen := make(map[string]bool)
	state := -1
	for len(s) > 0 {
		var cluster string
		cluster, s, _, state = uniseg.FirstGraphemeClusterInString(s, state)
		if fragile(cluster) && !seen[cluster] {
			seen[cluster] = true
			out = append(out, cluster)
		}
	}
	return out
}

// Lead returns the emoji grapheme cluster s starts with, fragile or safe, or
// "" when s starts with anything that isn't an emoji.
func Lead(s string) string {
	cluster, _, _, _ := uniseg.FirstGraphemeClusterInString(s, -1)
	if isEmoji(cluster) {
		return cluster
	}
	return ""
}

// isEmoji reports whether a cluster is drawn as an emoji rather than as text.
//
// Extended_Pictographic alone is not that question, and Lead used to ask it
// that way: the property also covers ©, ™ and ‼, so picking an emoji for a
// title called "© 2026" deleted the © on the way past. Lead's answer is acted
// on destructively, which is why it uses this stricter test and Fragile — which
// only prints a warning — keeps its own.
//
// Presentation is the line. A pictograph with default emoji presentation
// measures two cells; a text-presentation one is promoted by VS16, which is
// what makes ✏️ an emoji and bare ✏ a pencil character. Sequences carry their
// own proof: nothing joins with a ZWJ, a keycap mark, a regional indicator, a
// skin tone or a tag character except an emoji.
//
// The cost is that a hand-typed bare ✏ leading a title is left in place rather
// than replaced. Leaving a character behind beats deleting one.
func isEmoji(cluster string) bool {
	for _, r := range cluster {
		switch {
		case r == 0xFE0F, // VS16: text presentation promoted to emoji
			r == 0x200D,                  // zero-width joiner: 👨‍👩‍👧
			r == 0x20E3,                  // combining keycap: #️⃣
			r >= 0x1F1E6 && r <= 0x1F1FF, // regional indicators: 🇬🇧
			r >= 0x1F3FB && r <= 0x1F3FF, // skin-tone modifiers: 👍🏽
			r >= 0xE0020 && r <= 0xE007F: // tag characters (subdivision flags)
			return true
		case unicode.Is(pictograph, r) && uniseg.StringWidth(string(r)) == 2:
			return true
		}
	}
	return false
}

func fragile(cluster string) bool {
	for _, r := range cluster {
		switch {
		case r == 0x200D, // zero-width joiner: 👨‍👩‍👧
			r == 0x20E3,                  // combining keycap: #️⃣
			r >= 0x1F1E6 && r <= 0x1F1FF, // regional indicators: 🇬🇧
			r >= 0x1F3FB && r <= 0x1F3FF, // skin-tone modifiers: 👍🏽
			r >= 0xE0020 && r <= 0xE007F: // tag characters (subdivision flags)
			return true
		case unicode.Is(pictograph, r) && !unicode.Is(safeEmoji, r):
			return true
		}
	}
	return false
}
