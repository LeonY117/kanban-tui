package tui

import (
	"strings"

	"github.com/LeonY117/kanban-tui/internal/store"
	"github.com/LeonY117/kanban-tui/internal/termwidth"
	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// The first-run terminal-width question.
//
// Nothing can measure this for us: the terminal answers a width query with a
// number lipgloss already disagrees with, and the disagreement is the thing we
// need to know. So the user is asked once — but asked by being shown, not by
// being told. The frame redraws under whichever row the cursor is on, and the
// sample box either lines up or steps.
//
// "Have we asked?" is recorded as the presence of terminalWidth in config.json,
// which is why an answer is always written, including the default one. A user
// who answers "grapheme" has answered; a config with the field missing has
// never been in front of anyone. There is deliberately no way to dismiss the
// question into a default — a defaulted answer is indistinguishable from a
// chosen one afterwards, and the box on screen settles it in a glance.

// onboardState is the cursor over widthProfiles. There is no separate chosen
// value: until enter, the cursor is the choice, which is also what makes the
// live preview honest.
type onboardState struct {
	idx int
}

// AskTerminalWidth opens the first-run question. It is called from cmd, where
// config.json is read, rather than inferred inside the model — the TUI has no
// concept of a first run, it just has a view that asks a question.
func (m *Model) AskTerminalWidth() {
	m.view = onboardView
	m.onboard.idx = 0
	for i, p := range widthProfiles {
		if p.profile == widthProfile {
			m.onboard.idx = i
		}
	}
}

// onboardProfile is the profile under the cursor, which is what the whole
// frame renders under while this view is open.
func (m *Model) onboardProfile() termwidth.Profile {
	if m.onboard.idx < 0 || m.onboard.idx >= len(widthProfiles) {
		return widthProfile
	}
	return widthProfiles[m.onboard.idx].profile
}

func (m *Model) updateOnboard(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch {
	case key.Matches(msg, keys.Quit):
		// Quitting is not an answer. The question comes back next launch,
		// which is better than recording a preference nobody expressed.
		return m, tea.Quit

	case key.Matches(msg, keys.Up):
		if m.onboard.idx > 0 {
			m.onboard.idx--
		}
	case key.Matches(msg, keys.Down):
		if m.onboard.idx < len(widthProfiles)-1 {
			m.onboard.idx++
		}

	case key.Matches(msg, keys.Enter):
		return m.answerTerminalWidth(m.onboardProfile())
	}
	// Esc deliberately does nothing. A defaulted answer is indistinguishable
	// from a chosen one once it is in config.json, and the whole value of this
	// question is that the box in front of you settles it — so it is asked
	// until it is answered. Quitting remains the way out that records nothing.
	return m, nil
}

// answerTerminalWidth records the choice and drops onto the board.
func (m *Model) answerTerminalWidth(p termwidth.Profile) (tea.Model, tea.Cmd) {
	widthProfile = p
	err := store.UpdateConfig(func(cfg *store.Config) error {
		cfg.TerminalWidth = p.String()
		return nil
	})
	m.view = boardView
	m.applyWidthProfile()
	if err != nil {
		// The answer holds for this session either way. What was lost is only
		// that the question comes back, so say so rather than failing quietly.
		m.notice = "couldn't save that to config.json — " + err.Error()
	}
	return m, nil
}

// ─── Render ──────────────────────────────────────────────────────────

// widthSample is the proof. Two rows laid out to the same width, one of them
// carrying an emoji: under the profile this terminal actually uses they line
// up, under the wrong one the emoji row's border steps. Shared with the
// Display settings section so there is one definition of "lines up".
func widthSample() []string {
	return []string{
		"┌──────────────────┐",
		"│ 🐛 a ticket      │",
		"│ plain text       │",
		"└──────────────────┘",
	}
}

const onboardWidth = 60

// viewOnboard puts the panel on an empty screen rather than over the board.
// The board behind would be the stronger preview — its column borders step
// under the wrong profile, where the sample box is only twenty cells wide —
// but this is the first thing a new install shows, and an empty screen reads
// as a question being asked rather than as a popup over work in progress.
func (m *Model) viewOnboard() string {
	width := onboardWidth
	if width > m.width-4 {
		width = m.width - 4
	}
	rows := m.onboardRows(width - 4)

	content := lipgloss.NewStyle().PaddingLeft(1).Render(strings.Join(rows, "\n"))
	panel := renderPanel("Emoji width", content, width, len(rows)+2, green, true)
	return lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center, panel)
}

func (m *Model) onboardRows(w int) []string {
	dim := lipgloss.NewStyle().Foreground(dimGray)
	wrap := lipgloss.NewStyle().Foreground(softWhite).Width(w)

	rows := strings.Split(wrap.Render(
		"Choose the option below that renders the box correctly."), "\n")
	rows = append(rows, "")

	for i, p := range widthProfiles {
		marker := "  "
		label := p.label
		if i == m.onboard.idx {
			marker = selectedMarker.Render("* ")
			label = lipgloss.NewStyle().Foreground(green).Bold(true).Render(label)
		}
		pad := strings.Repeat(" ", max(0, 12-lipgloss.Width(p.label)))
		rows = append(rows, " "+marker+label+pad+dim.Render(p.note))
	}

	rows = append(rows, "")
	for _, line := range widthSample() {
		rows = append(rows, "   "+line)
	}
	return append(rows,
		"",
		dim.Render(" change it later under ? → Display"),
		dim.Render(" j/k choose · enter confirm"),
	)
}
