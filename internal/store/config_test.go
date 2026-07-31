package store

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/LeonY117/kanban-tui/internal/model"
)

// writeConfig points the store root at a temp dir and puts body in its
// config.json.
func writeConfig(t *testing.T, body string) {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("KANBAN_FILE", filepath.Join(dir, "board.json"))
	if body == "" {
		return
	}
	if err := os.WriteFile(filepath.Join(dir, configFile), []byte(body), 0644); err != nil {
		t.Fatal(err)
	}
}

func TestLoadConfigReadsStatusLabels(t *testing.T) {
	writeConfig(t, `{"statusLabels": {"HOLD": "Waiting"}}`)

	got := LoadConfig().Labels()
	if got[model.StatusHold] != "Waiting" {
		t.Errorf("HOLD label = %q, want %q", got[model.StatusHold], "Waiting")
	}
}

func TestLoadConfigWithNoFileIsEmptyNotAnError(t *testing.T) {
	writeConfig(t, "")

	if got := LoadConfig().Labels(); len(got) != 0 {
		t.Errorf("labels = %v, want none", got)
	}
}

// A typo in config.json costs you your labels and nothing else. Boards have to
// keep opening, the same bargain LoadPins strikes.
func TestLoadConfigFallsBackToDefaultsWhenTheFileIsCorrupt(t *testing.T) {
	writeConfig(t, `{"statusLabels": {"HOLD": `)

	if got := LoadConfig().Labels(); len(got) != 0 {
		t.Errorf("labels = %v, want none", got)
	}
}

// Hand-edited config shouldn't need to shout, and a key that went through
// --status once should keep working here.
func TestLabelsAcceptLowercaseKeysAndStatusAliases(t *testing.T) {
	writeConfig(t, `{"statusLabels": {"hold": "Parked", "doing": "In progress"}}`)
	labels := LoadConfig().Labels()
	if labels[model.StatusHold] != "Parked" {
		t.Errorf("HOLD label = %q, want %q", labels[model.StatusHold], "Parked")
	}
	if labels[model.StatusDoing] != "In progress" {
		t.Errorf("DOING label = %q, want %q", labels[model.StatusDoing], "In progress")
	}

	writeConfig(t, `{"statusLabels": {"WAITING": "Waiting"}}`)
	if got := LoadConfig().Labels()[model.StatusHold]; got != "Waiting" {
		t.Errorf("WAITING key set HOLD label to %q, want %q", got, "Waiting")
	}
}

// An unknown column or a blank name is dropped on its own rather than taking
// the rest of the file down with it.
func TestLabelsDropUnknownStatusesAndBlankNames(t *testing.T) {
	writeConfig(t, `{"statusLabels": {"NOPE": "Nope", "TODO": "  ", "DONE": "Shipped"}}`)

	labels := LoadConfig().Labels()
	if len(labels) != 1 || labels[model.StatusDone] != "Shipped" {
		t.Errorf("labels = %v, want only DONE -> Shipped", labels)
	}
}
