package store

import (
	"os"
	"strings"
	"testing"
	"time"
)

func mustCreateSprint(t *testing.T, name string) {
	t.Helper()
	if err := CreateSprint(name, ""); err != nil {
		t.Fatalf("create sprint %q: %v", name, err)
	}
}

func pinNames(t *testing.T) []string {
	t.Helper()
	pinned, err := LoadPins()
	if err != nil {
		t.Fatalf("load pins: %v", err)
	}
	return pinned
}

func TestPinKeepsInsertionOrder(t *testing.T) {
	sandboxRoot(t)
	for _, name := range []string{"alpha", "beta", "gamma"} {
		mustCreateSprint(t, name)
	}

	// Pinned in a deliberately non-alphabetical order — the pinned list is a
	// hand-arranged sequence, not a sort.
	for _, name := range []string{"gamma", "alpha"} {
		if err := Pin(name); err != nil {
			t.Fatalf("pin %q: %v", name, err)
		}
	}
	if got, want := pinNames(t), []string{"gamma", "alpha"}; !equalStrings(got, want) {
		t.Errorf("pins = %v, want %v", got, want)
	}

	// Pinning twice is a no-op, not a duplicate row.
	if err := Pin("gamma"); err != nil {
		t.Fatalf("re-pin: %v", err)
	}
	if got, want := pinNames(t), []string{"gamma", "alpha"}; !equalStrings(got, want) {
		t.Errorf("after re-pin, pins = %v, want %v", got, want)
	}

	if err := Unpin("gamma"); err != nil {
		t.Fatalf("unpin: %v", err)
	}
	if got, want := pinNames(t), []string{"alpha"}; !equalStrings(got, want) {
		t.Errorf("after unpin, pins = %v, want %v", got, want)
	}
	// Unpinning something that isn't pinned is quiet.
	if err := Unpin("beta"); err != nil {
		t.Errorf("unpin of an unpinned sprint errored: %v", err)
	}
}

func TestTogglePinReportsNewState(t *testing.T) {
	sandboxRoot(t)
	mustCreateSprint(t, "demo")

	pinned, err := TogglePin("demo")
	if err != nil || !pinned {
		t.Fatalf("first toggle = (%v, %v), want (true, nil)", pinned, err)
	}
	pinned, err = TogglePin("demo")
	if err != nil || pinned {
		t.Fatalf("second toggle = (%v, %v), want (false, nil)", pinned, err)
	}
}

func TestMainIsAlwaysPinned(t *testing.T) {
	sandboxRoot(t)
	if !IsPinned("") {
		t.Error("main board reports unpinned")
	}
}

func TestMovePinReordersAndClampsAtTheEnds(t *testing.T) {
	sandboxRoot(t)
	for _, name := range []string{"one", "two", "three"} {
		mustCreateSprint(t, name)
		if err := Pin(name); err != nil {
			t.Fatalf("pin %q: %v", name, err)
		}
	}

	if err := MovePin("three", -1); err != nil {
		t.Fatalf("move up: %v", err)
	}
	if got, want := pinNames(t), []string{"one", "three", "two"}; !equalStrings(got, want) {
		t.Fatalf("pins = %v, want %v", got, want)
	}

	if err := MovePin("three", -1); err != nil {
		t.Fatalf("move up again: %v", err)
	}
	if got, want := pinNames(t), []string{"three", "one", "two"}; !equalStrings(got, want) {
		t.Fatalf("pins = %v, want %v", got, want)
	}

	// Already at the top / bottom: no move, no error.
	if err := MovePin("three", -1); err != nil {
		t.Fatalf("move past the top: %v", err)
	}
	if err := MovePin("two", 1); err != nil {
		t.Fatalf("move past the bottom: %v", err)
	}
	if got, want := pinNames(t), []string{"three", "one", "two"}; !equalStrings(got, want) {
		t.Errorf("pins = %v, want %v — clamped moves rewrote the order", got, want)
	}
}

func TestMovePinRejectsUnpinnedSprint(t *testing.T) {
	sandboxRoot(t)
	mustCreateSprint(t, "loose")
	if err := MovePin("loose", 1); err == nil {
		t.Error("moving an unpinned sprint succeeded, want an error")
	}
}

func TestPinRejectsMissingAndArchivedSprints(t *testing.T) {
	sandboxRoot(t)
	if err := Pin("ghost"); err == nil {
		t.Error("pinning a non-existent sprint succeeded")
	}
	mustCreateSprint(t, "old")
	if err := ArchiveSprint("old"); err != nil {
		t.Fatalf("archive: %v", err)
	}
	if err := Pin("old"); err == nil {
		t.Error("pinning an archived sprint succeeded")
	}
}

// A pin is a "keep this in front of me" marker, so archiving one is treated as
// a mistake rather than silently dropping the pin.
func TestArchiveSprintRefusesWhilePinned(t *testing.T) {
	sandboxRoot(t)
	mustCreateSprint(t, "demo")
	if err := Pin("demo"); err != nil {
		t.Fatalf("pin: %v", err)
	}

	err := ArchiveSprint("demo")
	if err == nil {
		t.Fatal("archived a pinned sprint, want a refusal")
	}
	if !strings.Contains(err.Error(), "unpin") {
		t.Errorf("refusal %q doesn't point at unpinning", err)
	}
	if IsSprintArchived("demo") {
		t.Error("sprint was archived despite the refusal")
	}

	if err := Unpin("demo"); err != nil {
		t.Fatalf("unpin: %v", err)
	}
	if err := ArchiveSprint("demo"); err != nil {
		t.Fatalf("archive after unpin: %v", err)
	}
}

func TestRemoveSprintDropsItsPin(t *testing.T) {
	sandboxRoot(t)
	mustCreateSprint(t, "doomed")
	if err := Pin("doomed"); err != nil {
		t.Fatalf("pin: %v", err)
	}
	if err := RemoveSprint("doomed"); err != nil {
		t.Fatalf("remove: %v", err)
	}
	if got := pinNames(t); len(got) != 0 {
		t.Errorf("pins = %v, want empty — a removed sprint left a ghost pin", got)
	}
}

// Pin order beats mtime order, which is what makes a pin worth having: the
// board you rarely touch stays at the top.
func TestListSprintsSortsPinnedFirst(t *testing.T) {
	sandboxRoot(t)
	for _, name := range []string{"fresh", "stale"} {
		mustCreateSprint(t, name)
	}
	// Make "stale" the least recently edited, so mtime alone would rank it last.
	s, err := NewSprint("stale")
	if err != nil {
		t.Fatal(err)
	}
	old := time.Now().Add(-48 * time.Hour)
	if err := os.Chtimes(s.BoardPath(), old, old); err != nil {
		t.Fatal(err)
	}

	sprints, err := ListSprints()
	if err != nil {
		t.Fatal(err)
	}
	if sprints[0].Name != "fresh" {
		t.Fatalf("unpinned order = %s first, want fresh (most recently edited)", sprints[0].Name)
	}

	if err := Pin("stale"); err != nil {
		t.Fatalf("pin: %v", err)
	}
	sprints, err = ListSprints()
	if err != nil {
		t.Fatal(err)
	}
	if sprints[0].Name != "stale" || !sprints[0].Pinned {
		t.Errorf("pinned order = %+v first, want stale with Pinned set", sprints[0])
	}
	if sprints[1].Name != "fresh" || sprints[1].Pinned {
		t.Errorf("second row = %+v, want unpinned fresh", sprints[1])
	}
}

// Archived sprints stay in the archived block even if pins.json names them —
// only reachable by hand-editing the file, but it must not float one to the top.
func TestListSprintsKeepsHandPinnedArchivedSprintsLast(t *testing.T) {
	sandboxRoot(t)
	mustCreateSprint(t, "active")
	mustCreateSprint(t, "old")
	if err := ArchiveSprint("old"); err != nil {
		t.Fatalf("archive: %v", err)
	}
	if err := savePins([]string{"old"}); err != nil {
		t.Fatalf("hand-write pins: %v", err)
	}

	sprints, err := ListSprints()
	if err != nil {
		t.Fatal(err)
	}
	if sprints[0].Name != "active" {
		t.Errorf("first row = %s, want active", sprints[0].Name)
	}
	if sprints[1].Name != "old" || sprints[1].Pinned {
		t.Errorf("archived row = %+v, want old reported unpinned", sprints[1])
	}
}

// Pins are a display preference: a corrupt file must never stand between the
// user and their boards.
func TestCorruptPinsFileReadsAsNothingPinned(t *testing.T) {
	sandboxRoot(t)
	mustCreateSprint(t, "demo")
	if err := os.MkdirAll(defaultRoot(), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(pinsPath(), []byte("{not json"), 0644); err != nil {
		t.Fatal(err)
	}

	if got := pinNames(t); len(got) != 0 {
		t.Errorf("pins = %v, want empty", got)
	}
	if _, err := ListSprints(); err != nil {
		t.Errorf("ListSprints failed on a corrupt pins file: %v", err)
	}
	// The next write repairs the file.
	if err := Pin("demo"); err != nil {
		t.Fatalf("pin over a corrupt file: %v", err)
	}
	if got, want := pinNames(t), []string{"demo"}; !equalStrings(got, want) {
		t.Errorf("pins = %v, want %v", got, want)
	}
}

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
