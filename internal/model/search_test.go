package model

import (
	"strings"
	"testing"
)

func ticket(title, desc, shortID, assignee string, tags ...string) Ticket {
	return Ticket{
		Title:       title,
		Description: desc,
		ShortID:     shortID,
		AssignedTo:  assignee,
		Tags:        tags,
		Status:      StatusTodo,
	}
}

func TestBareTermMatchesEveryReadableField(t *testing.T) {
	cases := []struct {
		name   string
		query  string
		ticket Ticket
	}{
		{"title", "search", ticket("Board search", "", "KA11", "")},
		{"description", "hashtag", ticket("Board", "via a hashtag", "KA11", "")},
		{"short id", "ka11", ticket("Board", "", "KA11", "")},
		{"assignee", "dana", ticket("Board", "", "KA11", "danazou")},
		{"tag", "cli", ticket("Board", "", "KA11", "", "cli")},
		{"case folded", "BOARD", ticket("Board", "", "KA11", "")},
		{"mid word", "earc", ticket("Board search", "", "KA11", "")},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if !ParseQuery(c.query).Match(c.ticket) {
				t.Errorf("%q did not match %+v", c.query, c.ticket)
			}
		})
	}
}

func TestHashTermMatchesTagsOnly(t *testing.T) {
	// "cli" is in the title but is nobody's tag, so #cli must not match it.
	titled := ticket("cli formatting", "", "KA9", "")
	if ParseQuery("#cli").Match(titled) {
		t.Error("#cli matched a card whose title merely contains cli")
	}
	if !ParseQuery("cli").Match(titled) {
		t.Error("the bare term should still match the title")
	}
	if !ParseQuery("#cl").Match(ticket("x", "", "KA9", "", "cli")) {
		t.Error("a half-typed #cl should narrow to the cli tag while typing")
	}
}

func TestTermsAreANDed(t *testing.T) {
	target := ticket("Board search", "", "KA11", "", "cli")
	other := ticket("Board settings", "", "KA7", "", "ui")

	q := ParseQuery("board #cli")
	if !q.Match(target) {
		t.Error("the card satisfying both terms did not match")
	}
	if q.Match(other) {
		t.Error("a card satisfying only the bare term matched an ANDed query")
	}
}

func TestBareHashIsNotATerm(t *testing.T) {
	// A lone # is the prefix that opens tag completion. Treating it as a term
	// would empty the board at the moment the user asks what the tags are.
	q := ParseQuery("#")
	if !q.Empty() {
		t.Fatalf("a lone # parsed into %d terms, want none", len(q.terms))
	}
	if !q.Match(ticket("untagged card", "", "KA1", "")) {
		t.Error("a lone # filtered out an untagged card")
	}
}

func TestTagCandidateCountIsWhatSelectingItYields(t *testing.T) {
	// "customers" also matches the #customer term, because tag matching is a
	// substring test. A count of literal carriers would say 1 and then show 2
	// — the drift PR #5 shipped.
	pool := []Ticket{
		ticket("a", "", "1", "", "customer"),
		ticket("b", "", "2", "", "customers"),
		ticket("c", "", "3", "", "cli"),
	}

	for _, c := range TagCandidates(pool, "c") {
		got := len(ParseQuery("#" + c.Tag).MatchAll(pool))
		if got != c.Count {
			t.Errorf("#%s offered %d cards, selecting it yields %d", c.Tag, c.Count, got)
		}
	}
}

func TestTagCandidatesRankAndDedupe(t *testing.T) {
	pool := []Ticket{
		ticket("a", "", "1", "", "cli"),
		ticket("b", "", "2", "", "CLI"), // same tag, different spelling
		ticket("c", "", "3", "", "ui"),
	}

	got := TagCandidates(pool, "")
	if len(got) != 2 {
		t.Fatalf("candidates = %+v, want one entry each for cli and ui", got)
	}
	if got[0].Tag != "cli" || got[0].Count != 2 {
		t.Errorf("first candidate = %+v, want cli with 2 — the busier tag leads", got[0])
	}
	if got[1].Tag != "ui" || got[1].Count != 1 {
		t.Errorf("second candidate = %+v, want ui with 1", got[1])
	}
}

func TestNestedTagIsCountedAsItFilters(t *testing.T) {
	// Substring matching means #cli also selects a card tagged client. That is
	// the cost of one consistent rule across every field, and the candidate
	// count is required to own it rather than promise a tidier number.
	pool := []Ticket{
		ticket("a", "", "1", "", "cli"),
		ticket("b", "", "2", "", "client"),
	}

	got := TagCandidates(pool, "cl")
	if len(got) != 2 {
		t.Fatalf("candidates = %+v, want both cli and client offered", got)
	}
	for _, c := range got {
		want := len(ParseQuery("#" + c.Tag).MatchAll(pool))
		if c.Count != want {
			t.Errorf("#%s offered %d, yields %d", c.Tag, c.Count, want)
		}
	}
	if got[0].Tag != "cli" || got[0].Count != 2 {
		t.Errorf("cli = %+v, want a count of 2: it selects the client card too", got[0])
	}
}

func TestBareHashOffersEveryTag(t *testing.T) {
	pool := []Ticket{
		ticket("a", "", "1", "", "cli"),
		ticket("b", "", "2", "", "ui"),
		ticket("c", "", "3", ""),
	}
	if got := TagCandidates(pool, ""); len(got) != 2 {
		t.Errorf("bare # offered %+v, want both tags", got)
	}
}

func TestTagCandidatesCountDoneCards(t *testing.T) {
	// PR #5 built its tag list from open cards only, so a tag living solely on
	// done work could never be filtered to at all.
	done := ticket("shipped", "", "1", "", "release")
	done.Status = StatusDone

	got := TagCandidates([]Ticket{done}, "")
	if len(got) != 1 || got[0].Tag != "release" || got[0].Count != 1 {
		t.Errorf("candidates = %+v, want release with 1 — a done card's tag still exists", got)
	}
}

func TestQuotedTagIsOneTerm(t *testing.T) {
	// Without quoting, `#needs review` is a tag term plus an unrelated bare
	// one, so completing a multi-word tag produced a query that meant
	// something other than what its count promised.
	target := ticket("a", "", "1", "", "needs review")
	// Tagged "needs", with "review" only in the title: it satisfies the two
	// independent terms the unquoted form parses into, but does not carry the
	// tag the user picked from the completion list.
	loose := ticket("review the deck", "", "2", "", "needs")

	q := ParseQuery(`#"needs review"`)
	if !q.Match(target) {
		t.Error("the quoted tag did not match the card carrying it")
	}
	if q.Match(loose) {
		t.Error("the quoted tag matched a card that merely has both words")
	}

	if !ParseQuery("#needs review").Match(loose) {
		t.Error("setup: unquoted, the two words match independently — that is the drift quoting exists to close")
	}
}

func TestUnterminatedQuoteRunsToTheEnd(t *testing.T) {
	// The query is reparsed on every keystroke, so the half-typed state is the
	// common one and has to keep narrowing.
	if !ParseQuery(`#"needs rev`).Match(ticket("a", "", "1", "", "needs review")) {
		t.Error("a half-typed quoted tag stopped matching")
	}
}

func TestQuotedBareTermMatchesAPhrase(t *testing.T) {
	if !ParseQuery(`"board search"`).Match(ticket("Board search with tags", "", "1", "")) {
		t.Error("a quoted bare term did not match the phrase")
	}
	if ParseQuery(`"board search"`).Match(ticket("search the board", "", "2", "")) {
		t.Error("a quoted bare term matched words that are not adjacent")
	}
}

func TestQuoteTagQuotesOnlyWhenNeeded(t *testing.T) {
	if got := QuoteTag("cli"); got != "#cli" {
		t.Errorf("QuoteTag(cli) = %q, want #cli", got)
	}
	if got := QuoteTag("needs review"); got != `#"needs review"` {
		t.Errorf("QuoteTag(needs review) = %q, want it quoted", got)
	}
}

func TestTagsHoldingAQuoteAreNotOffered(t *testing.T) {
	// Quoting is the escape and there is no escape for the escape, so such a
	// tag cannot be written as a term at all. Offering it would promise a
	// count the resulting query cannot deliver.
	pool := []Ticket{ticket("a", "", "1", "", `say "hi"`), ticket("b", "", "2", "", "cli")}
	for _, c := range TagCandidates(pool, "") {
		if strings.Contains(c.Tag, `"`) {
			t.Errorf("offered an unwritable tag: %q", c.Tag)
		}
	}
}

func TestNegationExcludes(t *testing.T) {
	tagged := ticket("a", "", "1", "", "cli")
	bare := ticket("auth work", "", "2", "")

	if ParseQuery("-#cli").Match(tagged) {
		t.Error("-#cli matched a card tagged cli")
	}
	if !ParseQuery("-#cli").Match(bare) {
		t.Error("-#cli excluded a card that isn't tagged cli")
	}
	if ParseQuery("-auth").Match(bare) {
		t.Error("-auth matched a card whose title contains auth")
	}
}

func TestUntaggedQuery(t *testing.T) {
	// Absence has no substring, so this is the only way to ask for it.
	tagged := ticket("a", "", "1", "", "cli")
	bare := ticket("b", "", "2", "")

	q := ParseQuery(Untagged)
	if q.Empty() {
		t.Fatal("-# parsed to nothing; it has to be a real term")
	}
	if q.Match(tagged) {
		t.Error("-# matched a tagged card")
	}
	if !q.Match(bare) {
		t.Error("-# did not match an untagged card")
	}
	// And its unnegated twin is still the completion prefix, filtering nothing.
	if !ParseQuery("#").Empty() {
		t.Error("a lone # became a term")
	}
}

func TestQuotedTermKeepsALeadingDash(t *testing.T) {
	// Quoting makes the dash literal, so a card can still be found by text
	// that starts with one.
	if !ParseQuery(`"-draft"`).Match(ticket("-draft spec", "", "1", "")) {
		t.Error("a quoted leading dash was read as negation")
	}
}
