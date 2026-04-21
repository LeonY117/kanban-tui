package store

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"

	"github.com/leon/kanban/internal/model"
)

const sprintsSubdir = "sprints"

var sprintNameRe = regexp.MustCompile(`^[A-Za-z0-9_-]{1,64}$`)

type SprintInfo struct {
	Name        string
	TicketCount int
}

func ValidateSprintName(name string) error {
	if !sprintNameRe.MatchString(name) {
		return fmt.Errorf("invalid sprint name %q: use letters, digits, '_' or '-' (1-64 chars)", name)
	}
	return nil
}

func sprintDir(name string) string {
	return filepath.Join(defaultRoot(), sprintsSubdir, name)
}

// NewSprint returns a Store pointed at a sprint's directory. The sprint is not
// created here — use CreateSprint for that.
func NewSprint(name string) (*Store, error) {
	if err := ValidateSprintName(name); err != nil {
		return nil, err
	}
	return New(sprintDir(name)), nil
}

// SprintExists reports whether a sprint's board.json exists. Caller is
// responsible for validating the name first.
func SprintExists(name string) bool {
	_, err := os.Stat(filepath.Join(sprintDir(name), boardFile))
	return err == nil
}

// CreateSprint creates a new empty sprint. Errors if the sprint already exists.
func CreateSprint(name string) error {
	if err := ValidateSprintName(name); err != nil {
		return err
	}
	if SprintExists(name) {
		return fmt.Errorf("sprint %q already exists", name)
	}
	return New(sprintDir(name)).Save(&model.Board{Version: 1, Tickets: []model.Ticket{}})
}

// RemoveSprint deletes a sprint's entire directory. Errors if it doesn't exist.
func RemoveSprint(name string) error {
	if err := ValidateSprintName(name); err != nil {
		return err
	}
	if !SprintExists(name) {
		return fmt.Errorf("sprint %q doesn't exist", name)
	}
	return os.RemoveAll(sprintDir(name))
}

// ListSprints returns sprints sorted by name. A sprint is a directory under
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
		name := entry.Name()
		if ValidateSprintName(name) != nil || !SprintExists(name) {
			continue
		}
		board, err := New(sprintDir(name)).Load()
		if err != nil {
			continue
		}
		sprints = append(sprints, SprintInfo{
			Name:        name,
			TicketCount: len(board.Tickets),
		})
	}

	sort.Slice(sprints, func(i, j int) bool { return sprints[i].Name < sprints[j].Name })
	return sprints, nil
}
