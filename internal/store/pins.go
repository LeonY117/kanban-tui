package store

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"syscall"
)

const pinsFile = "pins.json"
const pinsLock = ".pins.lock"

// pinState is the on-disk shape of pins.json. Pinned holds sprint names in the
// order they should appear; the main board is never listed — it is implicitly
// pinned and always sits at the top of the pinned block.
type pinState struct {
	Version int      `json:"version"`
	Pinned  []string `json:"pinned"`
}

func pinsPath() string {
	return filepath.Join(defaultRoot(), pinsFile)
}

// LoadPins returns the pinned sprint names in pin order.
//
// A pin for a sprint that no longer exists is harmless: callers list boards
// from disk and only consult the pin order for the ones they found. Removing a
// sprint through RemoveSprint drops its pin, so ghosts only appear if the
// sprints/ directory is edited by hand.
func LoadPins() ([]string, error) {
	data, err := os.ReadFile(pinsPath())
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var state pinState
	if err := json.Unmarshal(data, &state); err != nil {
		// Pins are a display preference. A corrupt file must never stand
		// between the user and their boards — fall back to "nothing pinned",
		// which the next pin/unpin rewrites cleanly.
		return nil, nil
	}
	return state.Pinned, nil
}

func savePins(pinned []string) error {
	if pinned == nil {
		pinned = []string{}
	}
	data, err := json.MarshalIndent(pinState{Version: 1, Pinned: pinned}, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	if err := os.MkdirAll(defaultRoot(), 0755); err != nil {
		return err
	}
	tmp := pinsPath() + ".tmp"
	if err := os.WriteFile(tmp, data, 0644); err != nil {
		return err
	}
	if err := os.Rename(tmp, pinsPath()); err != nil {
		os.Remove(tmp)
		return err
	}
	return nil
}

func editPins(fn func([]string) error) error {
	if err := os.MkdirAll(defaultRoot(), 0755); err != nil {
		return err
	}
	f, err := os.OpenFile(filepath.Join(defaultRoot(), pinsLock), os.O_CREATE|os.O_RDWR, 0644)
	if err != nil {
		return err
	}
	defer f.Close()
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX); err != nil {
		return err
	}
	defer syscall.Flock(int(f.Fd()), syscall.LOCK_UN)
	pinned, err := LoadPins()
	if err != nil {
		return err
	}
	return fn(pinned)
}

// IsPinned reports whether a sprint is pinned. The main board ("") is always
// pinned and reports true.
func IsPinned(name string) bool {
	if name == "" {
		return true
	}
	pinned, err := LoadPins()
	if err != nil {
		return false
	}
	return indexOf(pinned, name) >= 0
}

// Pin adds a sprint to the end of the pinned list. Pinning an already-pinned
// sprint is a no-op, not an error. The main board can't be pinned explicitly —
// it always is.
func Pin(name string) error {
	if err := ValidateSprintName(name); err != nil {
		return err
	}
	_, archived, exists, err := resolveSprintDir(name)
	if err != nil {
		return err
	}
	if !exists {
		return fmt.Errorf("sprint %q doesn't exist", name)
	}
	if archived {
		return fmt.Errorf("sprint %q is archived; unarchive it before pinning", name)
	}
	return editPins(func(pinned []string) error {
		if indexOf(pinned, name) >= 0 {
			return nil
		}
		return savePins(append(pinned, name))
	})
}

// Unpin removes a sprint from the pinned list. Unpinning something that isn't
// pinned is a no-op.
func Unpin(name string) error {
	if err := ValidateSprintName(name); err != nil {
		return err
	}
	return editPins(func(pinned []string) error {
		i := indexOf(pinned, name)
		if i < 0 {
			return nil
		}
		return savePins(append(pinned[:i:i], pinned[i+1:]...))
	})
}

// TogglePin flips a sprint's pinned state and reports the new one.
func TogglePin(name string) (bool, error) {
	if IsPinned(name) {
		return false, Unpin(name)
	}
	return true, Pin(name)
}

// MovePin shifts a pinned sprint by dir (-1 up, +1 down) within the pinned
// list. Moving past either end is a no-op — the pinned block's boundaries are
// the main board above and the unpinned boards below, and crossing the lower
// one is an unpin, not a move.
func MovePin(name string, dir int) error {
	if err := ValidateSprintName(name); err != nil {
		return err
	}
	return editPins(func(pinned []string) error {
		i := indexOf(pinned, name)
		if i < 0 {
			return fmt.Errorf("sprint %q isn't pinned", name)
		}
		j := i + dir
		if j < 0 || j >= len(pinned) {
			return nil
		}
		reordered := make([]string, len(pinned))
		copy(reordered, pinned)
		reordered[i], reordered[j] = reordered[j], reordered[i]
		return savePins(reordered)
	})
}

func indexOf(names []string, name string) int {
	for i, n := range names {
		if n == name {
			return i
		}
	}
	return -1
}
