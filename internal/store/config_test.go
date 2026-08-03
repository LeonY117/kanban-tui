package store

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/LeonY117/kanban-tui/internal/model"
)

// writeConfig points the store root at a temp dir and puts body in its
// config.json. An empty body writes no file at all.
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

func TestLoadConfigReadsColumnLabels(t *testing.T) {
	writeConfig(t, `{"columns":[{"status":"HOLD","label":"Waiting","short":"Wt"}]}`)

	cfg := LoadConfig()
	if got := cfg.Labels()[model.StatusHold]; got != "Waiting" {
		t.Errorf("HOLD label = %q, want Waiting", got)
	}
	if got := cfg.ShortLabels()[model.StatusHold]; got != "Wt" {
		t.Errorf("HOLD short = %q, want Wt", got)
	}
}

func TestLoadConfigWithNoFileIsEmptyNotAnError(t *testing.T) {
	writeConfig(t, "")
	if got := LoadConfig().Labels(); len(got) != 0 {
		t.Errorf("labels = %v, want none", got)
	}
}

// A typo in config.json costs you your preferences and nothing else — boards
// have to keep opening, the same bargain LoadPins strikes.
func TestLoadConfigFallsBackWhenTheFileIsCorrupt(t *testing.T) {
	writeConfig(t, `{"columns":[{"status":`)
	cfg := LoadConfig()
	if len(cfg.Labels()) != 0 || len(cfg.Keys) != 0 {
		t.Errorf("cfg = %+v, want a zero config", cfg)
	}
}

func TestLabelsDropUnknownStatusesAndBlanks(t *testing.T) {
	writeConfig(t, `{"columns":[
		{"status":"NOPE","label":"Nope"},
		{"status":"TODO","label":"  "},
		{"status":"DONE","label":"Shipped"}]}`)

	labels := LoadConfig().Labels()
	if len(labels) != 1 || labels[model.StatusDone] != "Shipped" {
		t.Errorf("labels = %v, want only DONE -> Shipped", labels)
	}
}

// A list has a defined order, so a file naming one status twice resolves the
// same way on every run. The map form this replaced resolved whichever way Go
// happened to iterate.
func TestDuplicateColumnEntriesResolveToTheLastOne(t *testing.T) {
	writeConfig(t, `{"columns":[
		{"status":"HOLD","label":"Parked"},
		{"status":"hold","label":"Waiting"}]}`)

	for i := 0; i < 50; i++ {
		if got := LoadConfig().Labels()[model.StatusHold]; got != "Waiting" {
			t.Fatalf("run %d: HOLD = %q, want the last entry, Waiting", i, got)
		}
	}
}

func TestSaveConfigRoundTrips(t *testing.T) {
	writeConfig(t, "")
	want := Config{
		Columns: []ColumnConfig{{Status: "HOLD", Label: "Waiting"}},
		Keys:    map[string]string{"card.add": "z"},
	}
	if err := SaveConfig(want); err != nil {
		t.Fatal(err)
	}

	got := LoadConfig()
	if got.Version != 1 {
		t.Errorf("version = %d, want 1", got.Version)
	}
	if got.Labels()[model.StatusHold] != "Waiting" {
		t.Errorf("labels = %v", got.Labels())
	}
	if got.Keys["card.add"] != "z" {
		t.Errorf("keys = %v", got.Keys)
	}
}

func TestSaveConfigLeavesNoTempFileBehind(t *testing.T) {
	writeConfig(t, "")
	if err := SaveConfig(Config{Keys: map[string]string{"card.add": "z"}}); err != nil {
		t.Fatal(err)
	}
	entries, err := os.ReadDir(defaultRoot())
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if filepath.Ext(e.Name()) == ".tmp" {
			t.Errorf("left %s behind", e.Name())
		}
	}
}
