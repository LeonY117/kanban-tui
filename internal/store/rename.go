package store

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/LeonY117/kanban-tui/internal/model"
)

// UpdateSprint renames a sprint and/or changes the prefix its ticket ids carry.
// newName equal to oldName leaves the name alone; an empty newPrefix leaves the
// prefix alone.
//
// Changing the prefix rewrites the short id of every ticket on the board and in
// its archive, keeping the number — KA7 becomes KB7 — so ids stay stable modulo
// the prefix. That only works while every target id is free, so the whole change
// is refused if another board already issued one of them.
//
// Everything is validated before anything is written, so a refusal leaves the
// sprint exactly as it was.
func UpdateSprint(oldName, newName, newPrefix string) error {
	if err := ValidateSprintName(oldName); err != nil {
		return err
	}
	if newName == "" {
		newName = oldName
	}
	if err := ValidateSprintName(newName); err != nil {
		return err
	}
	if newPrefix != "" {
		if err := ValidatePrefix(newPrefix); err != nil {
			return err
		}
		newPrefix = strings.ToUpper(newPrefix)
	}

	path, archived, exists, err := resolveSprintDir(oldName)
	if err != nil {
		return err
	}
	if !exists {
		return fmt.Errorf("sprint %q doesn't exist", oldName)
	}
	if archived {
		return fmt.Errorf("sprint %q is archived (read-only); run `kanban sprints unarchive %s` first", oldName, oldName)
	}
	if newName != oldName {
		if err := checkNameFree(path, newName); err != nil {
			return err
		}
	}

	s := New(path)
	board, err := s.Load()
	if err != nil {
		return err
	}
	archive, err := s.LoadArchive()
	if err != nil {
		return err
	}

	oldPrefix := EffectivePrefix(board, oldName)
	if newPrefix == "" {
		newPrefix = oldPrefix
	}

	boardIDs := retaggedIDs(board, oldPrefix, newPrefix)
	archiveIDs := retaggedIDs(archive, oldPrefix, newPrefix)
	if newPrefix != oldPrefix {
		if err := checkNoSelfClash(board, archive, boardIDs, archiveIDs); err != nil {
			return err
		}
		if err := checkIDsFree(path, newPrefix, boardIDs, archiveIDs); err != nil {
			return err
		}
	}

	// A sprint that predates prefixes has none stored and derives one from its
	// name, so renaming it would silently move its future ids to a different
	// prefix. Writing the effective prefix down as part of the rename is what
	// stops that.
	if board.Prefix != newPrefix || len(boardIDs) > 0 || len(archiveIDs) > 0 {
		if err := s.WithLock(func() error {
			board, err := s.Load()
			if err != nil {
				return err
			}
			archive, err := s.loadArchive()
			if err != nil {
				return err
			}
			applyIDs(board, boardIDs)
			applyIDs(archive, archiveIDs)
			board.Prefix = newPrefix
			if err := s.Save(board); err != nil {
				return err
			}
			return s.saveArchive(archive)
		}); err != nil {
			return err
		}
	}

	if newPrefix != oldPrefix {
		if err := raiseCounter(newPrefix, highestNumber(boardIDs, archiveIDs)); err != nil {
			return err
		}
	}

	if newName != oldName {
		if err := s.WithLock(func() error {
			return os.Rename(path, sprintDir(newName))
		}); err != nil {
			return err
		}
		if err := renamePin(oldName, newName); err != nil {
			return err
		}
	}
	return nil
}

// checkNameFree reports whether newName is available for the sprint currently at
// oldPath.
//
// Two cases need more than "already exists". On a case-insensitive filesystem a
// case-only rename resolves to the sprint's own directory, so refusing it would
// make capitalisation permanently unfixable — os.Rename handles that in place, so
// allow it. And an archived sprint is invisible in the default picker, so a bare
// "already exists" names a board the user cannot see; say it's archived, the way
// CreateSprint does.
func checkNameFree(oldPath, newName string) error {
	target, archived, exists, err := resolveSprintDir(newName)
	if err != nil {
		return err
	}
	if !exists {
		return nil
	}
	if !archived {
		if same, err := sameDir(oldPath, target); err == nil && same {
			return nil // case-only rename of this very sprint
		}
		return fmt.Errorf("sprint %q already exists", newName)
	}
	return fmt.Errorf("sprint %q already exists (archived); run `kanban sprints unarchive %s` to restore or pick another name", newName, newName)
}

func sameDir(a, b string) (bool, error) {
	fa, err := os.Stat(a)
	if err != nil {
		return false, err
	}
	fb, err := os.Stat(b)
	if err != nil {
		return false, err
	}
	return os.SameFile(fa, fb), nil
}

// retaggedID is one planned short-id rewrite.
type retaggedID struct {
	ticketID string // uuid, so the rewrite survives a concurrent reorder
	number   int
	newID    string
}

// retaggedIDs plans the rewrite for one board. Ids that don't parse under the
// old prefix are left alone: pre-prefix ids taken from a ticket's own uuid have
// no number to carry over, and anything else was hand-edited.
func retaggedIDs(board *model.Board, oldPrefix, newPrefix string) []retaggedID {
	if board == nil || oldPrefix == newPrefix {
		return nil
	}
	var out []retaggedID
	for _, t := range board.Tickets {
		if isLegacyShortID(t) {
			continue
		}
		n, ok := parseTicketNumber(t.ShortID, oldPrefix)
		if !ok {
			continue
		}
		out = append(out, retaggedID{ticketID: t.ID, number: n, newID: newPrefix + strconv.Itoa(n)})
	}
	return out
}

func applyIDs(board *model.Board, ids []retaggedID) {
	if board == nil {
		return
	}
	byID := make(map[string]string, len(ids))
	for _, r := range ids {
		byID[r.ticketID] = r.newID
	}
	for i := range board.Tickets {
		if newID, ok := byID[board.Tickets[i].ID]; ok {
			board.Tickets[i].ShortID = newID
		}
	}
}

func highestNumber(sets ...[]retaggedID) int {
	max := 0
	for _, set := range sets {
		for _, r := range set {
			if r.number > max {
				max = r.number
			}
		}
	}
	return max
}

// checkNoSelfClash refuses the retag if an id it would mint is already held by a
// ticket on this same board.
//
// A cross-board move keeps the ticket's short id, so a board can hold ids from a
// prefix that isn't its own — sprint `kanban` holding an `AL1` that arrived from
// `alpha`. Those ids don't parse under the old prefix, so retaggedIDs leaves them
// alone, and checkIDsFree can't see them because it skips this board's own
// directory. Retagging `kanban` to AL would then rewrite KA1 → AL1 and leave two
// AL1s on one board, where FindByID returns the first and the other ticket
// becomes unreachable by id.
func checkNoSelfClash(board, archive *model.Board, boardIDs, archiveIDs []retaggedID) error {
	retagged := map[string]bool{}
	for _, set := range [][]retaggedID{boardIDs, archiveIDs} {
		for _, r := range set {
			retagged[r.ticketID] = true
		}
	}
	// Ids that survive the rewrite untouched, and so still occupy their slot.
	kept := map[string]bool{}
	for _, b := range []*model.Board{board, archive} {
		if b == nil {
			continue
		}
		for _, t := range b.Tickets {
			if !retagged[t.ID] {
				kept[strings.ToUpper(t.ShortID)] = true
			}
		}
	}

	var clashes []string
	seen := map[string]bool{}
	for _, set := range [][]retaggedID{boardIDs, archiveIDs} {
		for _, r := range set {
			key := strings.ToUpper(r.newID)
			if kept[key] && !seen[key] {
				seen[key] = true
				clashes = append(clashes, r.newID)
			}
		}
	}
	if len(clashes) == 0 {
		return nil
	}
	sort.Strings(clashes)
	return fmt.Errorf("this board already holds %s — a ticket moved in from another board carries that id; move it out or renumber it first",
		strings.Join(clashes, ", "))
}

// checkIDsFree refuses the retag if any id it would mint is already issued on
// another board. Two boards may deliberately share a prefix and interleave their
// numbers, so the check is per-id rather than per-prefix.
func checkIDsFree(skipDir, prefix string, sets ...[]retaggedID) error {
	inUse, err := idsInUse(prefix, skipDir)
	if err != nil {
		return err
	}
	if len(inUse) == 0 {
		return nil
	}
	var clashes []string
	seen := map[int]bool{}
	for _, set := range sets {
		for _, r := range set {
			owner, taken := inUse[r.number]
			if !taken || seen[r.number] {
				continue
			}
			seen[r.number] = true
			clashes = append(clashes, fmt.Sprintf("%s (on %s)", r.newID, owner))
		}
	}
	if len(clashes) == 0 {
		return nil
	}
	sort.Strings(clashes)
	shown := clashes
	suffix := ""
	if len(shown) > 3 {
		shown, suffix = shown[:3], fmt.Sprintf(" and %d more", len(clashes)-3)
	}
	return fmt.Errorf("prefix %q would reuse ids already issued: %s%s", prefix, strings.Join(shown, ", "), suffix)
}

// idsInUse maps every ticket number issued under a prefix to the board holding
// it, skipping one directory. Covers archives too — an archived KB7 still owns
// that id.
func idsInUse(prefix, skipDir string) (map[int]string, error) {
	inUse := map[int]string{}
	err := eachStore(func(s *Store) error {
		if s.dir == skipDir {
			return nil
		}
		name := s.BoardName()
		if name == "" {
			name = "main"
		}
		for _, load := range []func() (*model.Board, error){s.Load, s.loadArchive} {
			b, err := load()
			if err != nil {
				return fmt.Errorf("cannot read %s to check for id clashes: %w", s.dir, err)
			}
			for _, t := range b.Tickets {
				if isLegacyShortID(t) {
					continue
				}
				if n, ok := parseTicketNumber(t.ShortID, prefix); ok {
					inUse[n] = name
				}
			}
		}
		return nil
	})
	return inUse, err
}

// eachStore calls fn for every board on disk: the main board, then every sprint
// directory including archived ones.
func eachStore(fn func(*Store) error) error {
	if err := fn(New("")); err != nil {
		return err
	}
	root := filepath.Join(defaultRoot(), sprintsSubdir)
	entries, err := os.ReadDir(root)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		if err := fn(New(filepath.Join(root, e.Name()))); err != nil {
			return err
		}
	}
	return nil
}

// renamePin follows a sprint rename, keeping its slot in the pinned order.
func renamePin(oldName, newName string) error {
	return withPinsLock(func() error {
		pinned, err := LoadPins()
		if err != nil {
			return err
		}
		i := indexOf(pinned, oldName)
		if i < 0 {
			return nil
		}
		reordered := make([]string, len(pinned))
		copy(reordered, pinned)
		reordered[i] = newName
		return savePins(reordered)
	})
}
