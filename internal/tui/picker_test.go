package tui

import (
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/LeonY117/kanban-tui/internal/store"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// pickerModel returns a model sitting on the main board with the named sprints
// created and the board picker open. Board mtimes are stamped so that with
// nothing pinned the picker lists the sprints in the order given — otherwise
// the unpinned ordering depends on clock granularity during the test, and a
// pin that changes nothing would look like a pass.
func pickerModel(t *testing.T, sprints ...string) *Model {
	t.Helper()
	m := testModel(t, "a ticket")
	for i, name := range sprints {
		if err := store.CreateSprint(name, ""); err != nil {
			t.Fatalf("create sprint %q: %v", name, err)
		}
		s, err := store.NewSprint(name)
		if err != nil {
			t.Fatal(err)
		}
		stamp := time.Now().Add(time.Duration(-i) * time.Hour)
		if err := os.Chtimes(s.BoardPath(), stamp, stamp); err != nil {
			t.Fatal(err)
		}
	}
	m.enterPicker()
	if m.view != pickerView {
		t.Fatalf("view = %v, want pickerView", m.view)
	}
	return m
}

func pickerNames(m *Model) []string {
	names := make([]string, 0, len(m.pickerBoards))
	for _, e := range m.pickerBoards {
		names = append(names, boardDisplayName(e.name))
	}
	return names
}

// selectBoard parks the picker cursor on a board by name.
func selectBoard(t *testing.T, m *Model, name string) {
	t.Helper()
	for i, e := range m.pickerBoards {
		if e.name == name {
			m.pickerIdx = i
			return
		}
	}
	t.Fatalf("board %q not in the picker: %v", name, pickerNames(m))
}

func TestPickerPinMovesBoardAboveTheRest(t *testing.T) {
	m := pickerModel(t, "alpha", "beta")
	selectBoard(t, m, "beta")

	// Drive it through the real key path — the p binding has to be wired, not
	// just the handler present.
	m.updatePicker(keyPress("p"))

	if got, want := pickerNames(m), []string{"main", "beta", "alpha"}; strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("order = %v, want %v", got, want)
	}
	if !m.pickerBoards[1].pinned {
		t.Error("beta is not marked pinned")
	}
	if m.pickerIdx != 1 {
		t.Errorf("cursor at %d, want 1 — it should follow the board it pinned", m.pickerIdx)
	}
	if !store.IsPinned("beta") {
		t.Error("pin did not persist to disk")
	}

	// Unpin puts it back below the divider.
	m.updatePicker(keyPress("p"))
	if m.pickerBoards[1].pinned {
		t.Error("beta is still pinned after a second p")
	}
	if store.IsPinned("beta") {
		t.Error("unpin did not persist to disk")
	}
}

func TestPickerRefusesToPinMain(t *testing.T) {
	m := pickerModel(t, "alpha")
	selectBoard(t, m, "")

	m.pickerTogglePin()

	if m.notice == "" {
		t.Error("no notice explaining that main can't be unpinned")
	}
	if !m.pickerBoards[0].pinned {
		t.Error("main stopped reporting as pinned")
	}
	if pins, err := store.LoadPins(); err != nil || len(pins) != 0 {
		t.Errorf("pins = %v (err %v), want empty — main is pinned implicitly", pins, err)
	}
}

func TestPickerReordersPinnedBoards(t *testing.T) {
	m := pickerModel(t, "alpha", "beta")
	for _, name := range []string{"alpha", "beta"} {
		selectBoard(t, m, name)
		m.pickerTogglePin()
	}
	if got, want := pickerNames(m), []string{"main", "alpha", "beta"}; strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("pinned order = %v, want %v", got, want)
	}

	selectBoard(t, m, "beta")
	m.updatePicker(keyPress("K")) // reorder up

	if got, want := pickerNames(m), []string{"main", "beta", "alpha"}; strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("after K, order = %v, want %v", got, want)
	}
	if m.pickerIdx != 1 {
		t.Errorf("cursor at %d, want 1 — it should ride along with the moved board", m.pickerIdx)
	}

	// Main owns the top slot: another K is a no-op.
	m.updatePicker(keyPress("K"))
	if got, want := pickerNames(m), []string{"main", "beta", "alpha"}; strings.Join(got, ",") != strings.Join(want, ",") {
		t.Errorf("beta pushed past main: %v, want %v", got, want)
	}
}

// J on the last pinned board must not silently unpin it by pushing it across
// the divider.
func TestPickerReorderStopsAtTheDivider(t *testing.T) {
	m := pickerModel(t, "alpha", "beta")
	selectBoard(t, m, "alpha")
	m.pickerTogglePin()

	selectBoard(t, m, "alpha")
	m.pickerReorderPin(1)

	if !store.IsPinned("alpha") {
		t.Error("alpha was unpinned by a downward reorder")
	}
	if got, want := pickerNames(m), []string{"main", "alpha", "beta"}; strings.Join(got, ",") != strings.Join(want, ",") {
		t.Errorf("order = %v, want %v", got, want)
	}
}

func TestPickerRefusesToReorderUnpinnedBoard(t *testing.T) {
	m := pickerModel(t, "alpha", "beta")
	selectBoard(t, m, "beta")

	m.pickerReorderPin(-1)

	if m.notice == "" {
		t.Error("no notice explaining that only pinned boards reorder")
	}
	if got, want := pickerNames(m), []string{"main", "alpha", "beta"}; strings.Join(got, ",") != strings.Join(want, ",") {
		t.Errorf("order = %v, want %v — an unpinned board moved", got, want)
	}
}

func TestPickerRefusesToArchivePinnedBoard(t *testing.T) {
	m := pickerModel(t, "alpha")
	selectBoard(t, m, "alpha")
	m.pickerTogglePin()

	selectBoard(t, m, "alpha")
	m.startPickerArchive()

	if m.confirmArchive != "" {
		t.Errorf("confirm prompt opened for a pinned sprint (%q)", m.confirmArchive)
	}
	if !strings.Contains(m.notice, "unpin") {
		t.Errorf("notice %q doesn't point at unpinning", m.notice)
	}
	if store.IsSprintArchived("alpha") {
		t.Error("sprint was archived")
	}
}

// The divider only earns its line when there is a pinned sprint above it and
// something below.
func TestPickerDividerAppearsOnlyBetweenTwoNonEmptySections(t *testing.T) {
	m := pickerModel(t, "alpha", "beta")

	lines := m.pickerLines()
	if len(lines) != len(m.pickerBoards) {
		t.Errorf("got %d lines for %d boards with nothing pinned, want no divider", len(lines), len(m.pickerBoards))
	}

	selectBoard(t, m, "alpha")
	m.pickerTogglePin()

	lines = m.pickerLines()
	if len(lines) != len(m.pickerBoards)+1 {
		t.Fatalf("got %d lines for %d boards, want one divider", len(lines), len(m.pickerBoards))
	}
	if lines[2].boardIdx != -1 {
		t.Errorf("divider at line %d, want it after main + alpha", dividerLine(lines))
	}
	for i, l := range lines {
		if i != 2 && l.boardIdx < 0 {
			t.Errorf("a second divider at line %d", i)
		}
	}

	// Pin everything and the divider has nothing left to divide.
	selectBoard(t, m, "beta")
	m.pickerTogglePin()
	if lines := m.pickerLines(); len(lines) != len(m.pickerBoards) {
		t.Errorf("got %d lines with everything pinned, want no divider", len(lines))
	}
}

func dividerLine(lines []pickerLine) int {
	for i, l := range lines {
		if l.boardIdx < 0 {
			return i
		}
	}
	return -1
}

// The divider is decoration: clicking it must not switch boards, and the rows
// under it must still map to their own boards rather than shifting by one.
func TestPickerZonesSkipTheDivider(t *testing.T) {
	m := pickerModel(t, "alpha", "beta")
	selectBoard(t, m, "alpha")
	m.pickerTogglePin()

	m.resetZones()
	origin := point{x: 4, y: 3}
	const height = 10
	panel := m.renderPickerPopup(50, height, origin)

	if h := len(strings.Split(panel, "\n")); h != height {
		t.Fatalf("popup rendered %d rows, want %d", h, height)
	}

	lines := m.pickerLines()
	dividerY := origin.y + 1 + dividerLine(lines)
	var rows []hitZone
	for _, z := range m.zones {
		if z.kind == zonePickerRow {
			rows = append(rows, z)
		}
	}
	if len(rows) != len(m.pickerBoards) {
		t.Fatalf("%d clickable rows for %d boards", len(rows), len(m.pickerBoards))
	}
	for _, z := range rows {
		if z.y == dividerY {
			t.Errorf("the divider row at y=%d is clickable", z.y)
		}
	}
	// Zones are registered top to bottom, so the last board's zone must sit
	// one line below the divider's own line, not on it.
	last := rows[len(rows)-1]
	if last.idx != len(m.pickerBoards)-1 {
		t.Errorf("bottom zone points at board %d, want %d", last.idx, len(m.pickerBoards)-1)
	}
	if last.y <= dividerY {
		t.Errorf("bottom zone at y=%d, want below the divider at y=%d", last.y, dividerY)
	}
}

// MovePin clamps silently at both ends, so the TUI has to say which edge was hit
// — otherwise the keypress just looks broken.
func TestPickerReorderExplainsBothEdges(t *testing.T) {
	m := pickerModel(t, "alpha", "beta")
	for _, name := range []string{"alpha", "beta"} {
		selectBoard(t, m, name)
		m.pickerTogglePin()
	}

	selectBoard(t, m, "alpha") // top of the pinned block, under main
	m.notice = ""
	m.pickerReorderPin(-1)
	if !strings.Contains(m.notice, "main") {
		t.Errorf("notice at the top edge = %q, want it to mention main", m.notice)
	}

	selectBoard(t, m, "beta") // bottom of the pinned block
	m.notice = ""
	m.pickerReorderPin(1)
	if m.notice == "" {
		t.Error("no notice at the bottom edge of the pinned block")
	}
	if !store.IsPinned("beta") {
		t.Error("the clamped move unpinned beta")
	}
}

// The terminal truncates an over-long footer with no ellipsis, and the picker's
// footer is the only place its keys are documented — so the hint telling you how
// to get out has to survive at any width.
func TestFooterKeepsTheEscapeHintWhenItCannotFit(t *testing.T) {
	m := pickerModel(t, "alpha")
	full := m.helpText()
	if !strings.HasSuffix(full, "esc/tab close") {
		t.Fatalf("picker help no longer ends with the escape hint: %q", full)
	}

	for _, width := range []int{200, 120, 100, 80, 60, 40, 20} {
		got := fitHints(full, hintSep, width)
		if lipgloss.Width(got) > width {
			t.Errorf("at width %d the footer is %d wide: %q", width, lipgloss.Width(got), got)
		}
		if !strings.Contains(got, "esc/tab close") {
			t.Errorf("at width %d the escape hint was dropped: %q", width, got)
		}
		if width >= lipgloss.Width(full) && got != full {
			t.Errorf("at width %d the footer was trimmed despite fitting: %q", width, got)
		}
	}

	// The whole footer, badge included, has to fit a normal terminal.
	m.width = 100
	if w := lipgloss.Width(m.footerLine()); w > m.width {
		t.Errorf("footer renders %d columns wide on a %d-column terminal", w, m.width)
	}
	m.pickerShowArchived = true
	m.reloadPickerEntries()
	if w := lipgloss.Width(m.footerLine()); w > m.width {
		t.Errorf("archived-mode footer renders %d columns wide on a %d-column terminal", w, m.width)
	}
}

// A pinned board the user rarely touches is the whole point: reopening the
// picker must not re-sort it back down by mtime.
func TestPinSurvivesReopeningThePicker(t *testing.T) {
	m := pickerModel(t, "alpha", "beta")
	selectBoard(t, m, "beta")
	m.pickerTogglePin()

	m.restorePopupView(pickerView)
	m.enterPicker()

	if got, want := pickerNames(m), []string{"main", "beta", "alpha"}; strings.Join(got, ",") != strings.Join(want, ",") {
		t.Errorf("order after reopening = %v, want %v", got, want)
	}
}

// titleOf runs a command and reports the window title it announces, or "" if it
// announces none. bubbletea keeps the message type unexported, so the match is
// by type name. Never hand this a command that may carry a tick — running one
// sleeps for the tick interval.
func titleOf(cmd tea.Cmd) string {
	if cmd == nil {
		return ""
	}
	msg := cmd()
	if batch, ok := msg.(tea.BatchMsg); ok {
		for _, sub := range batch {
			if title := titleOf(sub); title != "" {
				return title
			}
		}
		return ""
	}
	if fmt.Sprintf("%T", msg) != "tea.setWindowTitleMsg" {
		return ""
	}
	return fmt.Sprint(msg)
}

// The terminal title tracks the board in view, so a tab running kanban reads as
// the sprint rather than as the tool.
func TestWindowTitleFollowsTheBoard(t *testing.T) {
	m := pickerModel(t, "demo")

	// Init's batch carries a tick, so read the title off the model rather than
	// running it.
	if m.Init(); m.windowTitle != "main" {
		t.Errorf("title at launch = %q, want main", m.windowTitle)
	}

	if _, cmd := m.Update(keyPress("j")); titleOf(cmd) != "" {
		t.Errorf("a keystroke re-announced the unchanged title as %q", titleOf(cmd))
	}

	if err := m.switchBoard("demo"); err != nil {
		t.Fatal(err)
	}
	_, cmd := m.Update(keyPress("j"))
	if got := titleOf(cmd); got != "demo" {
		t.Errorf("title after switching to demo = %q, want demo", got)
	}
}
