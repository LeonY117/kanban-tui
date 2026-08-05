package store

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/LeonY117/kanban-tui/internal/model"
	"github.com/google/uuid"
)

const defaultDir = ".kanban"
const boardFile = "board.json"
const archiveFile = "archive.json"
const lockFile = ".board.lock"

// Store manages reading and writing the board JSON file with file locking.
type Store struct {
	dir       string
	boardName string // filename within dir (usually "board.json")
	archived  bool   // when true, Save returns ErrArchived; reads still work
}

// ErrArchived is returned by Save when the underlying sprint is archived.
// Mutations should be rejected; the caller can display a hint pointing at
// `kanban sprints unarchive`.
var ErrArchived = fmt.Errorf("board is archived (read-only); run `kanban sprints unarchive <name>` to restore")

// defaultRoot returns the directory that holds the main board, archive, lock,
// and any sprints/ subdirectory. Honors KANBAN_FILE (using its parent dir).
func defaultRoot() string {
	if env := os.Getenv("KANBAN_FILE"); env != "" {
		return filepath.Dir(env)
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, defaultDir)
}

// New creates a store. If dir is empty, uses the default root (or KANBAN_FILE).
// Once constructed, the store's paths are fixed — later env-var changes don't affect it.
func New(dir string) *Store {
	if dir == "" {
		if env := os.Getenv("KANBAN_FILE"); env != "" {
			return &Store{dir: filepath.Dir(env), boardName: filepath.Base(env)}
		}
		home, _ := os.UserHomeDir()
		dir = filepath.Join(home, defaultDir)
	}
	return &Store{dir: dir, boardName: boardFile}
}

// BoardPath returns the path to the board JSON file.
func (s *Store) BoardPath() string {
	return filepath.Join(s.dir, s.boardName)
}

func (s *Store) boardPath() string {
	return filepath.Join(s.dir, s.boardName)
}

func (s *Store) archivePath() string {
	return filepath.Join(s.dir, archiveFile)
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

// WithLock runs fn while holding an exclusive file lock. Returns ErrArchived
// before acquiring the lock if the store points at an archived sprint —
// callers (Add, Update, Archive, Unarchive, ArchiveByID) cover all mutating
// paths, so this is the single chokepoint that blocks writes on archived
// sprints. ArchiveSprint/UnarchiveSprint use stores constructed via New()
// (not NewSprint), so their internal lock-then-rename isn't blocked here.
func (s *Store) WithLock(fn func() error) error {
	if s.archived {
		return ErrArchived
	}
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

// BoardName is the board's logical name: "" for the main board, otherwise the
// sprint name (archived or not).
func (s *Store) BoardName() string {
	if s.dir == defaultRoot() {
		return ""
	}
	return strings.TrimSuffix(filepath.Base(s.dir), archivedSuffix)
}

// ensurePrefix returns the ticket-id prefix for this board, assigning one from
// the board name the first time a ticket is created on a sprint that predates
// prefixes. The main board keeps the empty prefix, so its ids are bare numbers.
func (s *Store) ensurePrefix(board *model.Board) string {
	prefix := EffectivePrefix(board, s.BoardName())
	board.Prefix = prefix // persisted by the caller's Save
	return prefix
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
		shortID, err := NextTicketID(s.ensurePrefix(board))
		if err != nil {
			return err
		}
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

// upsertArchivedTicket refreshes a copy left by an interrupted archive instead
// of creating duplicate UUIDs. The newer board copy wins because it stayed
// editable.
func upsertArchivedTicket(archive *model.Board, ticket model.Ticket) {
	if existing, _ := archive.FindByUUID(ticket.ID); existing != nil {
		*existing = ticket
		return
	}
	archive.Tickets = append(archive.Tickets, ticket)
}

// ArchiveByID moves a single ticket to archive.json regardless of status.
func (s *Store) ArchiveByID(id string) error {
	return s.WithLock(func() error {
		board, err := s.Load()
		if err != nil {
			return err
		}
		t, idx := board.FindByID(id)
		if t == nil {
			return fmt.Errorf("ticket not found: %s", id)
		}

		archive, err := s.loadArchive()
		if err != nil {
			return err
		}

		now := time.Now()
		archived := *t
		archived.ArchivedAt = &now
		upsertArchivedTicket(archive, archived)
		board.Tickets = append(board.Tickets[:idx], board.Tickets[idx+1:]...)

		// Write the gaining file first: without a transaction, a failed second
		// write duplicates the ticket instead of dropping it from both files, and
		// there is no backup to get it back from. Retrying collapses the
		// duplicate; deleting or moving the board copy strands the archive copy.
		if err := s.saveArchive(archive); err != nil {
			return err
		}
		return s.Save(board)
	})
}

// LoadArchive returns the archived tickets.
func (s *Store) LoadArchive() (*model.Board, error) {
	return s.loadArchive()
}

// Unarchive moves a ticket out of archive.json back to the board with its
// original status. Clears ArchivedAt and bumps UpdatedAt. A retry after an
// interrupted attempt keeps the copy already sitting on the board — status and
// edits included — rather than the archive's older one, so the status it lands
// on is the restored copy's own once it has been changed.
func (s *Store) Unarchive(id string) error {
	return s.WithLock(func() error {
		archive, err := s.loadArchive()
		if err != nil {
			return err
		}
		t, idx := archive.FindByID(id)
		if t == nil {
			return fmt.Errorf("archived ticket not found: %s", id)
		}
		board, err := s.Load()
		if err != nil {
			return err
		}
		restored := *t
		restored.ArchivedAt = nil
		restored.UpdatedAt = time.Now()
		// The board may already hold it from an interrupted earlier attempt. Keep
		// that copy rather than the archive's — it is the one that stayed editable.
		if existing, _ := board.FindByUUID(restored.ID); existing == nil {
			board.Tickets = append(board.Tickets, restored)
		}
		archive.Tickets = append(archive.Tickets[:idx], archive.Tickets[idx+1:]...)

		// Write the gaining file first so a failed second write duplicates the
		// ticket instead of dropping it.
		if err := s.Save(board); err != nil {
			return err
		}
		return s.saveArchive(archive)
	})
}

// Archive moves DONE tickets to archive.json. If before is non-nil, only archives
// tickets updated before that time.
func (s *Store) Archive(before *time.Time) (int, error) {
	var count int
	err := s.WithLock(func() error {
		board, err := s.Load()
		if err != nil {
			return err
		}

		// Load existing archive
		archive, err := s.loadArchive()
		if err != nil {
			return err
		}

		// Split tickets into keep and archive
		var keep []model.Ticket
		now := time.Now()
		// A retry only reconciles copies that still match. Changing the status or
		// updating past the cutoff leaves it in both files until archived by ID.
		for _, t := range board.Tickets {
			if t.Status == model.StatusDone {
				if before == nil || t.UpdatedAt.Before(*before) {
					t.ArchivedAt = &now
					upsertArchivedTicket(archive, t)
					count++
					continue
				}
			}
			keep = append(keep, t)
		}

		if count == 0 {
			return nil
		}

		if keep == nil {
			keep = []model.Ticket{}
		}
		board.Tickets = keep

		// Write the gaining file first; see ArchiveByID.
		if err := s.saveArchive(archive); err != nil {
			return err
		}
		return s.Save(board)
	})
	return count, err
}

func (s *Store) loadArchive() (*model.Board, error) {
	data, err := os.ReadFile(s.archivePath())
	if err != nil {
		if os.IsNotExist(err) {
			return &model.Board{Version: 1, Tickets: []model.Ticket{}}, nil
		}
		return nil, fmt.Errorf("reading archive: %w", err)
	}
	var board model.Board
	if err := json.Unmarshal(data, &board); err != nil {
		return nil, fmt.Errorf("parsing archive: %w", err)
	}
	if board.Tickets == nil {
		board.Tickets = []model.Ticket{}
	}
	return &board, nil
}

func (s *Store) saveArchive(board *model.Board) error {
	if err := s.ensureDir(); err != nil {
		return err
	}
	data, err := json.MarshalIndent(board, "", "  ")
	if err != nil {
		return fmt.Errorf("marshaling archive: %w", err)
	}
	data = append(data, '\n')
	tmp := s.archivePath() + ".tmp"
	if err := os.WriteFile(tmp, data, 0644); err != nil {
		return fmt.Errorf("writing archive temp: %w", err)
	}
	if err := os.Rename(tmp, s.archivePath()); err != nil {
		os.Remove(tmp)
		return fmt.Errorf("renaming archive temp: %w", err)
	}
	return nil
}
