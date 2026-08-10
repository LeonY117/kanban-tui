package store

import (
	"fmt"
	"strings"
	"unicode/utf8"
)

// MaxDescriptionLen caps a board's description. Deliberately small: the field
// is parsed on every command that loads the board, and one big enough to hold
// a document invites it to become one — inside board.json, which has no
// history and which a --set overwrites whole. Long-form context belongs in the
// project's own repo, with the description pointing at it.
const MaxDescriptionLen = 2000

// ValidateDescription rejects an over-long description rather than truncating
// it, so a caller that writes too much finds out instead of silently losing
// the tail.
func ValidateDescription(desc string) error {
	if n := utf8.RuneCountInString(desc); n > MaxDescriptionLen {
		return fmt.Errorf("description is %d characters, max %d — link a doc in the repo rather than inlining it", n, MaxDescriptionLen)
	}
	return nil
}

// DescriptionHeader is the description's first line. Surfaces that list every
// board at once show this instead of the whole field, so a survey of fifteen
// boards doesn't cost fifteen descriptions.
func DescriptionHeader(desc string) string {
	first, _, _ := strings.Cut(desc, "\n")
	return strings.TrimSpace(first)
}

// SetDescription replaces the board's description. Routed through WithLock, so
// an archived sprint refuses it like every other mutation.
func (s *Store) SetDescription(desc string) error {
	desc = strings.TrimSpace(desc)
	if err := ValidateDescription(desc); err != nil {
		return err
	}
	return s.WithLock(func() error {
		board, err := s.Load()
		if err != nil {
			return err
		}
		board.Description = desc
		return s.Save(board)
	})
}
