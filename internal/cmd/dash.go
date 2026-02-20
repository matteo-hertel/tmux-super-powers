package cmd

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/matteo-hertel/tmux-super-powers/config"
	tmuxpkg "github.com/matteo-hertel/tmux-super-powers/internal/tmux"
	"github.com/spf13/cobra"
)

var dashCmd = &cobra.Command{
	Use:   "dash",
	Short: "Real-time dashboard of all tmux sessions",
	Long: `Mission control for all your tmux sessions.

Shows live pane preview, activity status, and quick actions.

Key bindings:
  j/k or arrows  Navigate sessions
  tab             Cycle panes in selected session
  enter           Jump to session
  x               Kill session (with confirmation)
  q/esc           Quit`,
	Run: func(cmd *cobra.Command, args []string) {
		if !tmuxpkg.IsInsideTmux() {
			fmt.Fprintf(os.Stderr, "Error: dash must be run inside a tmux session\n")
			os.Exit(1)
		}

		sessions, err := getTmuxSessions()
		if err != nil || len(sessions) == 0 {
			fmt.Println("No tmux sessions found")
			return
		}

		cfg, _ := config.Load()

		m := dashModel{
			sessions:      make([]sessionInfo, len(sessions)),
			cfg:           cfg,
			lastRefreshed: time.Now(),
		}
		for i, s := range sessions {
			content := capturePaneContent(s, 0)
			m.sessions[i] = sessionInfo{
				name:        s,
				status:      "active",
				lastChanged: time.Now(),
				prevContent: "",
				paneContent: content,
			}
		}

		p := tea.NewProgram(m, tea.WithAltScreen())
		finalModel, err := p.Run()
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}

		if fm, ok := finalModel.(dashModel); ok && fm.jumpTo != "" {
			tmuxpkg.AttachOrSwitch(fm.jumpTo)
		}
	},
}

type dashModel struct {
	sessions      []sessionInfo
	cursor        int
	jumpTo        string
	previewPane   int
	width         int
	height        int
	cfg           *config.Config
	lastRefreshed time.Time
	confirmKill   bool // true when awaiting kill confirmation
}

type dashTickMsg time.Time

func dashTickCmd(refreshMs int) tea.Cmd {
	d := time.Duration(refreshMs) * time.Millisecond
	return tea.Tick(d, func(t time.Time) tea.Msg {
		return dashTickMsg(t)
	})
}

func (m dashModel) Init() tea.Cmd {
	return dashTickCmd(m.cfg.Dash.RefreshMs)
}

func (m dashModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		return m, nil

	case dashTickMsg:
		now := time.Now()
		for i := range m.sessions {
			s := &m.sessions[i]
			newContent := capturePaneContent(s.name, m.previewPaneFor(i))
			s.prevContent = s.paneContent
			if newContent != s.paneContent {
				s.lastChanged = now
			}
			s.paneContent = newContent
			s.status = inferStatus(
				s.prevContent, s.paneContent, s.lastChanged, now,
				m.cfg.Dash.ErrorPatterns, m.cfg.Dash.PromptPattern,
			)
		}
		m.lastRefreshed = now
		return m, dashTickCmd(m.cfg.Dash.RefreshMs)

	case tea.KeyMsg:
		if m.confirmKill {
			switch msg.String() {
			case "y":
				if m.cursor < len(m.sessions) {
					name := m.sessions[m.cursor].name
					tmuxpkg.KillSession(name)
					m.sessions = append(m.sessions[:m.cursor], m.sessions[m.cursor+1:]...)
					if m.cursor >= len(m.sessions) && m.cursor > 0 {
						m.cursor--
					}
				}
				m.confirmKill = false
				return m, nil
			default:
				m.confirmKill = false
				return m, nil
			}
		}

		switch msg.Type {
		case tea.KeyCtrlC, tea.KeyEsc:
			return m, tea.Quit
		case tea.KeyUp:
			m.moveCursor(-1)
			return m, nil
		case tea.KeyDown:
			m.moveCursor(1)
			return m, nil
		case tea.KeyEnter:
			if len(m.sessions) > 0 {
				m.jumpTo = m.sessions[m.cursor].name
			}
			return m, tea.Quit
		case tea.KeyTab:
			m.previewPane++
			return m, nil
		default:
			switch msg.String() {
			case "q":
				return m, tea.Quit
			case "j":
				m.moveCursor(1)
				return m, nil
			case "k":
				m.moveCursor(-1)
				return m, nil
			case "x":
				if len(m.sessions) > 0 {
					m.confirmKill = true
				}
				return m, nil
			}
		}
	}

	return m, nil
}

func (m *dashModel) moveCursor(delta int) {
	if len(m.sessions) == 0 {
		return
	}
	m.cursor += delta
	if m.cursor < 0 {
		m.cursor = len(m.sessions) - 1
	} else if m.cursor >= len(m.sessions) {
		m.cursor = 0
	}
	m.previewPane = 0
}

func (m dashModel) previewPaneFor(i int) int {
	if i == m.cursor {
		return m.previewPane
	}
	return 0
}

func (m dashModel) View() string {
	if m.width == 0 || m.height == 0 {
		return "Loading..."
	}

	leftWidth := m.width * 35 / 100
	rightWidth := m.width - leftWidth - 3

	statusColor := func(status string) lipgloss.Color {
		switch status {
		case "active":
			return lipgloss.Color("82") // green
		case "idle":
			return lipgloss.Color("245") // gray
		case "done":
			return lipgloss.Color("226") // yellow
		case "error":
			return lipgloss.Color("196") // red
		default:
			return lipgloss.Color("255")
		}
	}

	now := time.Now()
	var sessionLines []string
	for i, s := range m.sessions {
		icon := statusIcon(s.status)
		timeSince := formatTimeSince(s.lastChanged, now)

		line := fmt.Sprintf(" %s %-20s %s", icon, truncate(s.name, 20), timeSince)

		if i == m.cursor {
			style := lipgloss.NewStyle().
				Bold(true).
				Foreground(lipgloss.Color("212")).
				Background(lipgloss.Color("236"))
			sessionLines = append(sessionLines, style.Render(fmt.Sprintf("▸%s", line)))
		} else {
			style := lipgloss.NewStyle().Foreground(statusColor(s.status))
			sessionLines = append(sessionLines, style.Render(fmt.Sprintf(" %s", line)))
		}
	}

	leftPanel := lipgloss.NewStyle().
		Width(leftWidth).
		Height(m.height - 4).
		BorderStyle(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("62")).
		Padding(1).
		Render(strings.Join(sessionLines, "\n"))

	// Right panel: live preview
	var previewContent string
	if len(m.sessions) > 0 && m.cursor < len(m.sessions) {
		previewContent = m.sessions[m.cursor].paneContent
	}
	if previewContent == "" {
		previewContent = "No content"
	}
	lines := strings.Split(previewContent, "\n")
	maxLines := m.height - 6
	if maxLines > 0 && len(lines) > maxLines {
		lines = lines[len(lines)-maxLines:]
	}
	previewContent = strings.Join(lines, "\n")

	rightPanel := lipgloss.NewStyle().
		Width(rightWidth).
		Height(m.height - 4).
		BorderStyle(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("62")).
		Padding(0, 1).
		Render(previewContent)

	title := lipgloss.NewStyle().
		Bold(true).
		Foreground(lipgloss.Color("212")).
		Render("  Dashboard — j/k: navigate | tab: pane | enter: jump | x: kill | q: quit")

	statusBar := ""
	if m.confirmKill && m.cursor < len(m.sessions) {
		statusBar = lipgloss.NewStyle().
			Foreground(lipgloss.Color("196")).
			Bold(true).
			Render(fmt.Sprintf("  Kill session '%s'? (y/n)", m.sessions[m.cursor].name))
	}

	layout := lipgloss.JoinHorizontal(lipgloss.Top, leftPanel, rightPanel)
	result := fmt.Sprintf("%s\n%s", title, layout)
	if statusBar != "" {
		result += "\n" + statusBar
	}

	return result
}

func truncate(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max-1] + "…"
}

// getWorktreeMap returns a map of session-name → worktree-branch
// for enriching dash display with worktree info.
func getWorktreeMap() map[string]string {
	cmd := exec.Command("git", "worktree", "list", "--porcelain")
	output, err := cmd.Output()
	if err != nil {
		return nil
	}
	result := make(map[string]string)
	worktrees := parseWorktreesPorcelain(string(output))
	for _, wt := range worktrees {
		result[wt.Branch] = wt.Path
	}
	return result
}
