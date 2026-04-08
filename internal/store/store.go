package store

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/google/uuid"
	"github.com/leon/kanban/internal/model"
)

const defaultDir = ".kanban"
const boardFile = "board.json"
const lockFile = ".board.lock"

// Store manages reading and writing the board JSON file with file locking.
type Store struct {
	dir string
}

// New creates a store. If dir is empty, uses ~/.kanban.
func New(dir string) *Store {
	if dir == "" {
		if env := os.Getenv("KANBAN_FILE"); env != "" {
			return &Store{dir: filepath.Dir(env)}
		}
		home, _ := os.UserHomeDir()
		dir = filepath.Join(home, defaultDir)
	}
	return &Store{dir: dir}
}

func (s *Store) boardPath() string {
	if env := os.Getenv("KANBAN_FILE"); env != "" {
		return env
	}
	return filepath.Join(s.dir, boardFile)
}

func (s *Store) lockPath() string {
	return filepath.Join(s.dir, lockFile)
}

func (s *Store) ensureDir() error {
	return os.MkdirAll(s.dir, 0755)
}

// Load reads the board from disk. Returns an empty board if file doesn't exist.
func (s *Store) Load() (*model.Board, error) {
	data, err := os.ReadFile(s.boardPath())
	if err != nil {
		if os.IsNotExist(err) {
			return &model.Board{Version: 1, Tickets: []model.Ticket{}}, nil
		}
		return nil, fmt.Errorf("reading board: %w", err)
	}
	var board model.Board
	if err := json.Unmarshal(data, &board); err != nil {
		return nil, fmt.Errorf("parsing board: %w", err)
	}
	if board.Tickets == nil {
		board.Tickets = []model.Ticket{}
	}
	return &board, nil
}

// Save writes the board to disk atomically (write tmp + rename).
func (s *Store) Save(board *model.Board) error {
	if err := s.ensureDir(); err != nil {
		return fmt.Errorf("creating kanban dir: %w", err)
	}
	data, err := json.MarshalIndent(board, "", "  ")
	if err != nil {
		return fmt.Errorf("marshaling board: %w", err)
	}
	data = append(data, '\n')

	tmp := s.boardPath() + ".tmp"
	if err := os.WriteFile(tmp, data, 0644); err != nil {
		return fmt.Errorf("writing temp file: %w", err)
	}
	if err := os.Rename(tmp, s.boardPath()); err != nil {
		os.Remove(tmp)
		return fmt.Errorf("renaming temp file: %w", err)
	}
	return nil
}

// WithLock runs fn while holding an exclusive file lock.
func (s *Store) WithLock(fn func() error) error {
	if err := s.ensureDir(); err != nil {
		return err
	}
	f, err := os.OpenFile(s.lockPath(), os.O_CREATE|os.O_RDWR, 0644)
	if err != nil {
		return fmt.Errorf("opening lock file: %w", err)
	}
	defer f.Close()

	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX); err != nil {
		return fmt.Errorf("acquiring lock: %w", err)
	}
	defer syscall.Flock(int(f.Fd()), syscall.LOCK_UN)

	return fn()
}

// Add creates a new ticket and saves the board. Returns the created ticket.
func (s *Store) Add(title, description string, status model.Status, tags []string, assignedTo, createdBy string) (*model.Ticket, error) {
	var ticket *model.Ticket
	err := s.WithLock(func() error {
		board, err := s.Load()
		if err != nil {
			return err
		}

		id := uuid.New().String()
		shortID := s.uniqueShortID(board, id)
		now := time.Now()

		t := model.Ticket{
			ID:          id,
			ShortID:     shortID,
			Title:       title,
			Description: description,
			Status:      status,
			Tags:        tags,
			AssignedTo:  assignedTo,
			CreatedAt:   now,
			UpdatedAt:   now,
			CreatedBy:   createdBy,
		}
		board.Tickets = append(board.Tickets, t)
		ticket = &t
		return s.Save(board)
	})
	return ticket, err
}

// Update modifies an existing ticket. The apply function receives the ticket to mutate.
func (s *Store) Update(id string, apply func(*model.Ticket)) error {
	return s.WithLock(func() error {
		board, err := s.Load()
		if err != nil {
			return err
		}
		t, _ := board.FindByID(id)
		if t == nil {
			return fmt.Errorf("ticket not found: %s", id)
		}
		apply(t)
		t.UpdatedAt = time.Now()
		return s.Save(board)
	})
}

// uniqueShortID returns the shortest unique prefix of the UUID (min 6 chars).
func (s *Store) uniqueShortID(board *model.Board, fullID string) string {
	for length := 6; length <= len(fullID); length++ {
		candidate := fullID[:length]
		unique := true
		for _, t := range board.Tickets {
			if strings.HasPrefix(t.ID, candidate) {
				unique = false
				break
			}
		}
		if unique {
			return candidate
		}
	}
	return fullID
}
