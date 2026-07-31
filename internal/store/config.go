package store

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"

	"github.com/LeonY117/kanban-tui/internal/model"
)

const configFile = "config.json"

// Config holds local display preferences, read from ~/.kanban/config.json.
//
// Nothing here changes what a board stores. Statuses persist under their
// canonical names (BACKLOG, TODO, DOING, DONE, HOLD) however they are labelled
// on screen, so a board stays readable to someone whose labels differ and the
// CLI keeps one vocabulary for scripts and agents.
type Config struct {
	// StatusLabels renames a column in the TUI, keyed by canonical status.
	// Statuses left out keep their built-in label. Keys are forgiving about
	// case, and accept the same aliases as --status.
	StatusLabels map[string]string `json:"statusLabels,omitempty"`

	// StatusLabelsShort overrides the compact label in the board picker's count
	// strip. Rarely needed: a renamed column takes the first character of its
	// new label unless this says otherwise.
	StatusLabelsShort map[string]string `json:"statusLabelsShort,omitempty"`
}

func configPath() string {
	return filepath.Join(defaultRoot(), configFile)
}

// LoadConfig reads config.json, falling back to a zero Config when it is
// missing or unreadable. Like pins, labels are a display preference and must
// never stand between the user and their boards, so a typo in the file costs
// you your labels and nothing else.
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

// Labels resolves the configured column names onto canonical statuses. Keys
// that aren't a status this build knows about, and blank labels, are dropped
// rather than failing the load.
func (c Config) Labels() map[model.Status]string {
	return resolveStatusKeys(c.StatusLabels)
}

// ShortLabels is Labels for the count-strip labels.
func (c Config) ShortLabels() map[model.Status]string {
	return resolveStatusKeys(c.StatusLabelsShort)
}

func resolveStatusKeys(in map[string]string) map[model.Status]string {
	if len(in) == 0 {
		return nil
	}
	out := make(map[model.Status]string, len(in))
	for key, label := range in {
		status, err := model.ParseStatus(key)
		if err != nil {
			continue
		}
		if label = strings.TrimSpace(label); label == "" {
			continue
		}
		out[status] = label
	}
	return out
}
