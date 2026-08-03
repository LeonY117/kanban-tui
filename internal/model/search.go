package model

import (
	"sort"
	"strings"
)

// Query is a parsed search query. Terms are ANDed: a ticket matches only when
// every term matches it. A term written `#foo` matches tags only; a bare term
// matches any field a reader can see — title, description, short id, tags or
// assignee.
//
// Matching is literal case-insensitive substring, not fuzzy subsequence. At
// ticket-title lengths typing the word properly costs nothing, while a
// subsequence run over a long description matches almost any short query and
// drags half the board into every result.
type Query struct {
	terms []queryTerm
}

type queryTerm struct {
	text   string // lowercased; never empty
	tagged bool   // written with a leading #
}

// ParseQuery splits raw input into terms on whitespace.
//
// A lone "#" is dropped rather than treated as "any tag": it is the prefix
// that opens tag completion, so filtering on it would empty the board at the
// exact moment the user is asking what the tags are.
func ParseQuery(raw string) Query {
	var q Query
	for _, field := range strings.Fields(raw) {
		tagged := strings.HasPrefix(field, "#")
		text := strings.ToLower(strings.TrimPrefix(field, "#"))
		if text == "" {
			continue
		}
		q.terms = append(q.terms, queryTerm{text: text, tagged: tagged})
	}
	return q
}

// Empty reports whether the query would filter nothing out.
func (q Query) Empty() bool { return len(q.terms) == 0 }

// Match reports whether t satisfies every term.
func (q Query) Match(t Ticket) bool {
	for _, term := range q.terms {
		if !term.match(t) {
			return false
		}
	}
	return true
}

// MatchAll returns the tickets satisfying the query, in their original order.
func (q Query) MatchAll(tickets []Ticket) []Ticket {
	if q.Empty() {
		return tickets
	}
	out := make([]Ticket, 0, len(tickets))
	for _, t := range tickets {
		if q.Match(t) {
			out = append(out, t)
		}
	}
	return out
}

func (term queryTerm) match(t Ticket) bool {
	if term.matchesTag(t.Tags) {
		return true
	}
	if term.tagged {
		return false
	}
	return containsFold(t.Title, term.text) ||
		containsFold(t.Description, term.text) ||
		containsFold(t.ShortID, term.text) ||
		containsFold(t.AssignedTo, term.text)
}

// matchesTag is a substring test, looser than containsTag's exact match, so a
// half-typed `#cu` narrows to #customer while you are still typing it. Once
// completion fills the name in, the two agree.
func (term queryTerm) matchesTag(tags []string) bool {
	for _, tag := range tags {
		if containsFold(tag, term.text) {
			return true
		}
	}
	return false
}

func containsFold(haystack, needleLower string) bool {
	return strings.Contains(strings.ToLower(haystack), needleLower)
}

// TagCount is one tag offered as a completion, with the number of tickets
// selecting it would leave on screen.
type TagCount struct {
	Tag   string
	Count int
}

// TagCandidates returns the distinct tags among tickets containing prefix
// (case-insensitively), most matches first and alphabetical within a count.
// An empty prefix offers every tag, which is what a bare `#` asks for.
//
// Count is what accepting the completion actually yields, not how many tickets
// literally carry the tag. The two differ, because `#customer` also matches a
// card tagged "customers". PR #5 computed its counts one way and applied its
// filter another, so the picker promised two cards and delivered three; the
// number offered here is produced by running the query that accepting would
// produce, which is the only way it can't drift.
func TagCandidates(tickets []Ticket, prefix string) []TagCount {
	prefix = strings.ToLower(prefix)

	// Fold case when grouping but keep the first spelling seen, so a tag typed
	// as "Customer" once and "customer" twice offers a single candidate.
	display := map[string]string{}
	for _, t := range tickets {
		for _, tag := range t.Tags {
			key := strings.ToLower(tag)
			if key == "" || !strings.Contains(key, prefix) {
				continue
			}
			if _, ok := display[key]; !ok {
				display[key] = tag
			}
		}
	}

	out := make([]TagCount, 0, len(display))
	for _, tag := range display {
		q := Query{terms: []queryTerm{{text: strings.ToLower(tag), tagged: true}}}
		out = append(out, TagCount{Tag: tag, Count: len(q.MatchAll(tickets))})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Count != out[j].Count {
			return out[i].Count > out[j].Count
		}
		return strings.ToLower(out[i].Tag) < strings.ToLower(out[j].Tag)
	})
	return out
}
