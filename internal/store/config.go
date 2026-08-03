package store

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"syscall"

	"github.com/LeonY117/kanban-tui/internal/model"
)

const configFile = "config.json"
const configLock = ".config.lock"

// ColumnConfig is one column's local display settings. Status is the canonical
// name and is what identifies the row; Label and Short only change what's drawn.
//
// A list rather than a map keyed by status: order, visibility and eventually
// column count are all fields this grows later, instead of a second config
// concept sitting alongside a rename map. It also makes duplicate entries
// resolve deterministically — last one wins — where a map keyed by status
// name resolved whichever way Go happened to iterate.
type ColumnConfig struct {
	Status string `json:"status"`
	Label  string `json:"label,omitempty"`
	Short  string `json:"short,omitempty"`
}

// Config holds local display preferences, read from ~/.kanban/config.json.
//
// Nothing here changes what a board stores. Statuses persist under their
// canonical names however they are labelled on screen, so a board stays
// readable to someone whose labels differ and the CLI keeps one vocabulary for
// scripts and agents.
type Config struct {
	Version int            `json:"version"`
	Columns []ColumnConfig `json:"columns,omitempty"`

	// Keys maps an action id (see tui.bindActions) to the key that triggers it.
	// Only actions that differ from their default are stored, so a later change
	// to a default reaches anyone who never overrode it.
	Keys map[string]string `json:"keys,omitempty"`
}

func configPath() string { return filepath.Join(defaultRoot(), configFile) }

// ConfigPath is where preferences are read from and written to for the board
// this process is talking to. It moves with KANBAN_FILE, so callers must ask
// rather than assume ~/.kanban.
func ConfigPath() string { return configPath() }

// LoadConfig reads config.json, falling back to a zero Config when it is
// missing or unreadable. Like pins, these are display preferences and must
// never stand between the user and their boards, so a typo in the file costs
// you your preferences and nothing else.
func LoadConfig() Config {
	data, err := os.ReadFile(configPath())
	if err != nil {
		return Config{}
	}
	var cfg Config
	if err := json.Unmarshal(data, &cfg); err != nil {
		return Config{}
	}
	return cfg
}

func saveConfig(cfg Config) error {
	cfg.Version = 1
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')

	tmp := configPath() + ".tmp"
	if err := os.WriteFile(tmp, data, 0644); err != nil {
		return err
	}
	if err := os.Rename(tmp, configPath()); err != nil {
		os.Remove(tmp)
		return err
	}
	return nil
}

func editConfig(fn func() error) error {
	if err := os.MkdirAll(defaultRoot(), 0755); err != nil {
		return err
	}
	lock, err := os.OpenFile(filepath.Join(defaultRoot(), configLock),
		os.O_CREATE|os.O_RDWR, 0644)
	if err != nil {
		return err
	}
	defer lock.Close()
	if err := syscall.Flock(int(lock.Fd()), syscall.LOCK_EX); err != nil {
		return err
	}
	defer syscall.Flock(int(lock.Fd()), syscall.LOCK_UN)
	return fn()
}

// SaveConfig replaces config.json atomically, under a lock.
//
// The lock matters because every process shares one config.json.tmp: two TUIs
// writing settings at the same moment would otherwise write the same temp file
// concurrently and rename whichever won, installing a mixture. Callers doing
// a read-modify-write should use UpdateConfig so the read is covered too.
func SaveConfig(cfg Config) error {
	return editConfig(func() error { return saveConfig(cfg) })
}

// UpdateConfig applies fn to the latest config and saves it while holding the
// config lock for the entire read-modify-write. This keeps unrelated settings
// written by another process between a page opening and closing.
func UpdateConfig(fn func(*Config) error) error {
	return editConfig(func() error {
		cfg := LoadConfig()
		if err := fn(&cfg); err != nil {
			return err
		}
		return saveConfig(cfg)
	})
}

// Labels resolves configured column names onto canonical statuses. Entries
// naming a status this build doesn't know about, and blank labels, are dropped
// rather than failing the load.
func (c Config) Labels() map[model.Status]string {
	return c.columnField(func(cc ColumnConfig) string { return cc.Label })
}

// ShortLabels is Labels for the board picker's count strip.
func (c Config) ShortLabels() map[model.Status]string {
	return c.columnField(func(cc ColumnConfig) string { return cc.Short })
}

func (c Config) columnField(pick func(ColumnConfig) string) map[model.Status]string {
	if len(c.Columns) == 0 {
		return nil
	}
	out := map[model.Status]string{}
	for _, cc := range c.Columns {
		status, err := model.ParseStatus(cc.Status)
		if err != nil {
			continue
		}
		if v := strings.TrimSpace(pick(cc)); v != "" {
			out[status] = v
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}
