package emoji

import (
	"reflect"
	"testing"
)

func TestFragile(t *testing.T) {
	tests := []struct {
		name  string
		title string
		want  []string
	}{
		{"plain ascii", "Tests: revamp tests", nil},
		{"punctuation that terminals agree on", "Slice 2 — Redis → worker × 3…", nil},
		{"safe single-codepoint emoji", "🔒 Review agent tool permissions", nil},
		{"safe emoji from Unicode 11 itself", "🧪 Set up NanoBot", nil},
		{"redundant VS16 on a safe base", "🔒️ still two cells everywhere", nil},

		{"variation-selector emoji", "🗄️ Slice 2 — Redis + worker split", []string{"🗄️"}},
		{"more VS16 emoji", "✏️⚠️⚙️ tools", []string{"✏️", "⚠️", "⚙️"}},
		{"bare text-default pictograph", "✏ pencil without VS16", []string{"✏"}},
		{"post-Unicode-11 emoji", "🫠 melting", []string{"🫠"}},
		{"zwj family", "👨‍👩‍👧 household", []string{"👨‍👩‍👧"}},
		{"flag", "🇬🇧 rollout", []string{"🇬🇧"}},
		{"keycap", "#️⃣ tags", []string{"#️⃣"}},
		{"skin tone", "👍🏽 approved", []string{"👍🏽"}},

		{"deduplicated, order kept", "🗄️ then 🛡️ then 🗄️ again", []string{"🗄️", "🛡️"}},
		{"safe and fragile mixed", "🔒 lock vs 🗂️ dividers", []string{"🗂️"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := Fragile(tt.title); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("Fragile(%q) = %q, want %q", tt.title, got, tt.want)
			}
		})
	}
}

// Lead's answer is acted on destructively — setTitlePrefix deletes what it
// returns — so it asks whether a cluster is presented as an emoji, not merely
// whether it is Extended_Pictographic. That property also covers ©, ™ and ‼,
// and a title of "© 2026" lost its © the moment someone picked an emoji.
func TestLeadKeepsTextPictographs(t *testing.T) {
	for _, tc := range []struct {
		title string
		want  string
	}{
		{"© 2026 acme", ""},
		{"™ trademark", ""},
		{"‼ urgent", ""},
		{"plain title", ""},
		{"🐛 a bug", "🐛"},
		{"⌚ a watch", "⌚"},
		{"🗄️ archived", "🗄️"}, // VS16 promotes it
		{"👨‍👩‍👧 household", "👨‍👩‍👧"}, // a ZWJ sequence is its own proof
		{"🇬🇧 rollout", "🇬🇧"},
	} {
		if got := Lead(tc.title); got != tc.want {
			t.Errorf("Lead(%q) = %q, want %q", tc.title, got, tc.want)
		}
	}
}
