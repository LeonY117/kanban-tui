package store

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"syscall"
	"unicode"

	"github.com/LeonY117/kanban-tui/internal/model"
)

const countersFile = "counters.json"
const countersLock = ".counters.lock"

var prefixRe = regexp.MustCompile(`^[A-Z]{1,4}$`)

// ValidatePrefix accepts 1-4 letters. Prefixes are deliberately allowed to
// collide: two boards sharing one just share its number line, so an id is
// never reused even when a prefix is.
func ValidatePrefix(p string) error {
	if !prefixRe.MatchString(strings.ToUpper(p)) {
		return fmt.Errorf("invalid prefix %q: use 1-4 letters", p)
	}
	return nil
}

// DerivePrefix is deliberately dumb — the first two letters of the board name,
// uppercased. Anything smarter belongs in the human's hands at creation time
// via `kanban sprints new <name> --prefix XY`.
func DerivePrefix(name string) string {
	var letters []rune
	for _, r := range name {
		if unicode.IsLetter(r) {
			letters = append(letters, unicode.ToUpper(r))
			if len(letters) == 2 {
				break
			}
		}
	}
	if len(letters) == 0 {
		return "X"
	}
	return string(letters)
}

// EffectivePrefix is the prefix a board's next ticket will carry: the stored
// one, or — for a board that predates prefixes — the one its name derives.
// The main board has none; its ids are bare numbers.
func EffectivePrefix(board *model.Board, boardName string) string {
	if board != nil && board.Prefix != "" {
		return board.Prefix
	}
	if boardName == "" {
		return ""
	}
	return DerivePrefix(boardName)
}

// countersPath is the shared per-prefix counter file, alongside the main board.
func countersPath() string {
	return filepath.Join(defaultRoot(), countersFile)
}

func loadCounters() (map[string]int, error) {
	data, err := os.ReadFile(countersPath())
	if err != nil {
		if os.IsNotExist(err) {
			return map[string]int{}, nil
		}
		return nil, err
	}
	counters := map[string]int{}
	if err := json.Unmarshal(data, &counters); err != nil {
		// A corrupt counter file must not block ticket creation: fall back to
		// an empty map, which reseeds from the boards on disk below.
		return map[string]int{}, nil
	}
	if counters == nil {
		// `null` is valid JSON and unmarshals into a nil map, which panics on
		// the first assignment rather than erroring.
		return map[string]int{}, nil
	}
	return counters, nil
}

func saveCounters(counters map[string]int) error {
	data, err := json.MarshalIndent(counters, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	if err := os.MkdirAll(defaultRoot(), 0755); err != nil {
		return err
	}
	tmp := countersPath() + ".tmp"
	if err := os.WriteFile(tmp, data, 0644); err != nil {
		return err
	}
	if err := os.Rename(tmp, countersPath()); err != nil {
		os.Remove(tmp)
		return err
	}
	return nil
}

// NextTicketID reserves the next id for a prefix and returns it, e.g. "KB7".
// An empty prefix (the main board) yields bare numbers.
//
// Counters are keyed by prefix rather than by board, so boards that share a
// prefix continue one another's numbering: archive `kanban` at K12, start a
// new board on K, and its first ticket is K13. The counter never goes
// backwards, so an id is never handed out twice even across archived boards.
func NextTicketID(prefix string) (string, error) {
	var id string
	err := withCountersLock(func() error {
		counters, err := loadCounters()
		if err != nil {
			return err
		}
		n, ok := counters[prefix]
		if !ok {
			// First use of this prefix (or a lost counter file): pick up from
			// the highest id already issued anywhere on disk.
			seed, err := highestIssued(prefix)
			if err != nil {
				return err
			}
			n = seed
		}
		n++
		counters[prefix] = n
		id = prefix + strconv.Itoa(n)
		return saveCounters(counters)
	})
	return id, err
}

func withCountersLock(fn func() error) error {
	if err := os.MkdirAll(defaultRoot(), 0755); err != nil {
		return err
	}
	f, err := os.OpenFile(filepath.Join(defaultRoot(), countersLock), os.O_CREATE|os.O_RDWR, 0644)
	if err != nil {
		return err
	}
	defer f.Close()
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX); err != nil {
		return err
	}
	defer syscall.Flock(int(f.Fd()), syscall.LOCK_UN)
	return fn()
}

// highestIssued scans every board and archive on disk for ids carrying this
// prefix and returns the largest number found. Only runs when the counter file
// has no entry for the prefix, so the cost lands once per prefix.
//
// A board it cannot read is an error, not a zero: skipping one silently would
// hand out an id that board already uses, and duplicate ids are precisely what
// the counter exists to prevent. Better to refuse to create the ticket and say
// why.
func highestIssued(prefix string) (int, error) {
	max := 0
	consider := func(board *model.Board) {
		if board == nil {
			return
		}
		for _, t := range board.Tickets {
			if isLegacyShortID(t) {
				continue
			}
			if n, ok := parseTicketNumber(t.ShortID, prefix); ok && n > max {
				max = n
			}
		}
	}

	scan := func(s *Store) error {
		b, err := s.Load()
		if err != nil {
			return fmt.Errorf("cannot read %s to seed the %q counter: %w", s.BoardPath(), prefix, err)
		}
		consider(b)
		a, err := s.LoadArchive()
		if err != nil {
			return fmt.Errorf("cannot read the archive beside %s to seed the %q counter: %w", s.BoardPath(), prefix, err)
		}
		consider(a)
		return nil
	}

	if err := eachStore(scan); err != nil {
		return 0, err
	}
	return max, nil
}

// raiseCounter lifts a prefix's counter to at least n, so ids handed out after a
// retag can't collide with the ones it just rewrote. A missing counter needs no
// help: NextTicketID seeds one from the highest id on disk, which by then
// includes the retagged ids.
func raiseCounter(prefix string, n int) error {
	if n <= 0 {
		return nil
	}
	return withCountersLock(func() error {
		counters, err := loadCounters()
		if err != nil {
			return err
		}
		if cur, ok := counters[prefix]; !ok || cur >= n {
			return nil
		}
		counters[prefix] = n
		return saveCounters(counters)
	})
}

// isLegacyShortID reports whether a ticket carries a pre-prefix id — six or
// more hex digits taken from the head of its own UUID. Most of those contain
// a-f and can't be mistaken for a counter value, but an all-digit one like
// "993899" would otherwise be read as the 993899th ticket on the main board
// and send its numbering into the millions. Being a prefix of the UUID is what
// settles it: ids issued by the counter never are.
func isLegacyShortID(t model.Ticket) bool {
	if len(t.ShortID) < 6 || !strings.HasPrefix(t.ID, strings.ToLower(t.ShortID)) {
		return false
	}
	for _, r := range strings.ToLower(t.ShortID) {
		if !((r >= '0' && r <= '9') || (r >= 'a' && r <= 'f')) {
			return false
		}
	}
	return true
}

// parseTicketNumber splits an id like "KB7" into its prefix and number, and
// reports whether the prefix is the one asked for. highestIssued filters legacy
// ids before calling this, including the all-digit ones that would parse here.
func parseTicketNumber(shortID, prefix string) (int, bool) {
	if !strings.HasPrefix(shortID, prefix) {
		return 0, false
	}
	rest := shortID[len(prefix):]
	if rest == "" {
		return 0, false
	}
	n, err := strconv.Atoi(rest)
	if err != nil || n < 0 {
		return 0, false
	}
	return n, true
}

// removeCountersFile drops the counter file. Used by tests to prove the
// counters reseed from the boards on disk rather than restarting at 1.
func removeCountersFile() error {
	if err := os.Remove(countersPath()); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}
