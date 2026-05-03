package store

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/leon/kanban/internal/model"
)

const sprintsSubdir = "sprints"
const archivedSuffix = ".archived"

var sprintNameRe = regexp.MustCompile(`^[A-Za-z0-9_-]{1,64}$`)

type SprintInfo struct {
	Name         string
	TicketCount  int
	StatusCounts map[model.Status]int
	LastModified time.Time
	Archived     bool
}

func ValidateSprintName(name string) error {
	if !sprintNameRe.MatchString(name) {
		return fmt.Errorf("invalid sprint name %q: use letters, digits, '_' or '-' (1-64 chars)", name)
	}
	return nil
}

// sprintDir returns the canonical (active) on-disk path for a sprint with the
// given name. The directory may or may not exist; archived sprints live at
// sprintDir(name) + archivedSuffix.
func sprintDir(name string) string {
	return filepath.Join(defaultRoot(), sprintsSubdir, name)
}

// resolveSprintDir locates a sprint by logical name. Returns the actual
// on-disk path, whether it's archived, whether it exists at all, and any
// validation error. The unsuffixed (active) path wins if both exist —
// can't happen via the API since rename is atomic, but defensive against
// manual filesystem manipulation leaving both directories in place.
func resolveSprintDir(name string) (path string, archived bool, exists bool, err error) {
	if err := ValidateSprintName(name); err != nil {
		return "", false, false, err
	}
	active := sprintDir(name)
	if _, statErr := os.Stat(filepath.Join(active, boardFile)); statErr == nil {
		return active, false, true, nil
	}
	arch := active + archivedSuffix
	if _, statErr := os.Stat(filepath.Join(arch, boardFile)); statErr == nil {
		return arch, true, true, nil
	}
	return active, false, false, nil
}

// NewSprint returns a Store pointed at a sprint's directory — the archived
// path if archived, the active path otherwise. The sprint is not created here
// — use CreateSprint for that. For sprints that don't yet exist, the store
// points at the unsuffixed canonical path. Archived stores reject Save with
// ErrArchived so writes fail at the boundary regardless of caller.
func NewSprint(name string) (*Store, error) {
	path, archived, exists, err := resolveSprintDir(name)
	if err != nil {
		return nil, err
	}
	if !exists {
		return New(sprintDir(name)), nil
	}
	s := New(path)
	s.archived = archived
	return s, nil
}

// SprintExists reports whether a sprint exists, whether active or archived.
func SprintExists(name string) bool {
	_, _, exists, _ := resolveSprintDir(name)
	return exists
}

// IsSprintArchived reports whether a sprint is archived. Returns false for
// non-existent sprints.
func IsSprintArchived(name string) bool {
	_, archived, exists, _ := resolveSprintDir(name)
	return exists && archived
}

// CreateSprint creates a new empty sprint. Errors if the sprint already
// exists, even if archived.
func CreateSprint(name string) error {
	if err := ValidateSprintName(name); err != nil {
		return err
	}
	if _, archived, exists, _ := resolveSprintDir(name); exists {
		if archived {
			return fmt.Errorf("sprint %q already exists (archived); run `kanban sprints unarchive %s` to restore", name, name)
		}
		return fmt.Errorf("sprint %q already exists", name)
	}
	return New(sprintDir(name)).Save(&model.Board{Version: 1, Tickets: []model.Ticket{}})
}

// RemoveSprint deletes a sprint's entire directory, whether active or
// archived. Errors if it doesn't exist.
func RemoveSprint(name string) error {
	path, _, exists, err := resolveSprintDir(name)
	if err != nil {
		return err
	}
	if !exists {
		return fmt.Errorf("sprint %q doesn't exist", name)
	}
	return os.RemoveAll(path)
}

// ArchiveSprint renames a sprint's directory to add the archived suffix.
// Acquires the board lock first so in-flight operations finish before the
// rename. Errors if the sprint doesn't exist or is already archived.
func ArchiveSprint(name string) error {
	path, archived, exists, err := resolveSprintDir(name)
	if err != nil {
		return err
	}
	if !exists {
		return fmt.Errorf("sprint %q doesn't exist", name)
	}
	if archived {
		return fmt.Errorf("sprint %q is already archived", name)
	}
	s := New(path)
	return s.WithLock(func() error {
		return os.Rename(path, path+archivedSuffix)
	})
}

// UnarchiveSprint reverses ArchiveSprint. Errors if the sprint doesn't exist
// or isn't archived.
func UnarchiveSprint(name string) error {
	path, archived, exists, err := resolveSprintDir(name)
	if err != nil {
		return err
	}
	if !exists {
		return fmt.Errorf("sprint %q doesn't exist", name)
	}
	if !archived {
		return fmt.Errorf("sprint %q is not archived", name)
	}
	s := New(path)
	return s.WithLock(func() error {
		unsuffixed := strings.TrimSuffix(path, archivedSuffix)
		return os.Rename(path, unsuffixed)
	})
}

// ListSprints returns all sprints (active and archived). Active sprints come
// first, sub-sorted by board mtime (most recently edited first); archived
// sprints come after, sub-sorted the same way. A sprint is a directory under
// sprints/ containing a board.json — bare directories are skipped.
func ListSprints() ([]SprintInfo, error) {
	root := filepath.Join(defaultRoot(), sprintsSubdir)
	entries, err := os.ReadDir(root)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}

	var sprints []SprintInfo
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		dirName := entry.Name()
		archived := strings.HasSuffix(dirName, archivedSuffix)
		logicalName := strings.TrimSuffix(dirName, archivedSuffix)
		if ValidateSprintName(logicalName) != nil {
			continue
		}
		actualPath := filepath.Join(root, dirName)
		boardPath := filepath.Join(actualPath, boardFile)
		info, err := os.Stat(boardPath)
		if err != nil {
			continue
		}
		board, err := New(actualPath).Load()
		if err != nil {
			continue
		}
		sprints = append(sprints, SprintInfo{
			Name:         logicalName,
			TicketCount:  len(board.Tickets),
			StatusCounts: CountByStatus(board),
			LastModified: info.ModTime(),
			Archived:     archived,
		})
	}

	sort.Slice(sprints, func(i, j int) bool {
		if sprints[i].Archived != sprints[j].Archived {
			return !sprints[i].Archived // active first
		}
		if !sprints[i].LastModified.Equal(sprints[j].LastModified) {
			return sprints[i].LastModified.After(sprints[j].LastModified)
		}
		return sprints[i].Name < sprints[j].Name
	})
	return sprints, nil
}

func CountByStatus(board *model.Board) map[model.Status]int {
	counts := make(map[model.Status]int, len(model.AllStatuses))
	for _, t := range board.Tickets {
		counts[t.Status]++
	}
	return counts
}
