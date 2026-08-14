// Package emoji classifies emoji by whether terminals agree on their width.
// The board measures text with modern grapheme-aware tables (lipgloss), but
// xterm.js-based terminals (VS Code, Cursor, and the agent-app panes built on
// them) count per codepoint with tables frozen at Unicode 11 — any sequence
// the two disagree on shifts an entire board row by a cell.
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
	if fragile(cluster) {
		return cluster
	}
	for _, r := range cluster {
		if unicode.Is(pictograph, r) {
			return cluster
		}
	}
	return ""
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
