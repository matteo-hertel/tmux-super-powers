package cmd

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	tmuxpkg "github.com/matteo-hertel/tmux-super-powers/internal/tmux"
	"github.com/spf13/cobra"
)

var peekCmd = &cobra.Command{
	Use:   "peek [session]",
	Short: "Live preview of tmux sessions",
	Long: `Interactive dashboard showing all tmux sessions with live pane preview.

Without arguments, opens an interactive TUI:
- Arrow keys to navigate sessions
- Tab to cycle panes within the previewed session
- Enter to jump to a session
- q/Esc to quit

With a session name, prints a one-shot capture and exits.`,
	Args: cobra.MaximumNArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		if len(args) == 1 {
			// Direct mode: capture and print
			content := capturePaneContent(args[0], 0)
			fmt.Print(content)
			return
		}

		if !tmuxpkg.IsInsideTmux() {
			fmt.Fprintf(os.Stderr, "Error: interactive peek must be run inside a tmux session\n")
			os.Exit(1)
		}

		sessions, err := getTmuxSessions()
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error getting sessions: %v\n", err)
			os.Exit(1)
		}

		if len(sessions) == 0 {
			fmt.Println("No tmux sessions found")
			return
		}

		m := peekModel{
			sessions: sessions,
			preview:  capturePaneContent(sessions[0], 0),
		}

		p := tea.NewProgram(m, tea.WithAltScreen())
		finalModel, err := p.Run()
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}

		if fm, ok := finalModel.(peekModel); ok && fm.selected != "" {
			tmuxpkg.AttachOrSwitch(fm.selected)
		}
	},
}

type tickMsg time.Time

type peekModel struct {
	sessions    []string
	cursor      int
	selected    string
	preview     string
	previewPane int
	width       int
	height      int
}

func (m peekModel) Init() tea.Cmd {
	return tickCmd()
}

func tickCmd() tea.Cmd {
	return tea.Tick(500*time.Millisecond, func(t time.Time) tea.Msg {
		return tickMsg(t)
	})
}

func (m peekModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		return m, nil

	case tickMsg:
		if len(m.sessions) > 0 {
			m.preview = capturePaneContent(m.sessions[m.cursor], m.previewPane)
		}
		return m, tickCmd()

	case tea.KeyMsg:
		switch msg.Type {
		case tea.KeyCtrlC, tea.KeyEsc:
			return m, tea.Quit
		case tea.KeyUp:
			if m.cursor > 0 {
				m.cursor--
			} else {
				m.cursor = len(m.sessions) - 1
			}
			m.previewPane = 0
			m.preview = capturePaneContent(m.sessions[m.cursor], m.previewPane)
			return m, nil
		case tea.KeyDown:
			if m.cursor < len(m.sessions)-1 {
				m.cursor++
			} else {
				m.cursor = 0
			}
			m.previewPane = 0
			m.preview = capturePaneContent(m.sessions[m.cursor], m.previewPane)
			return m, nil
		case tea.KeyEnter:
			if len(m.sessions) > 0 {
				m.selected = m.sessions[m.cursor]
			}
			return m, tea.Quit
		case tea.KeyTab:
			m.previewPane++
			m.preview = capturePaneContent(m.sessions[m.cursor], m.previewPane)
			return m, nil
		default:
			switch msg.String() {
			case "q":
				return m, tea.Quit
			case "j":
				if m.cursor < len(m.sessions)-1 {
					m.cursor++
				} else {
					m.cursor = 0
				}
				m.previewPane = 0
				m.preview = capturePaneContent(m.sessions[m.cursor], m.previewPane)
				return m, nil
			case "k":
				if m.cursor > 0 {
					m.cursor--
				} else {
					m.cursor = len(m.sessions) - 1
				}
				m.previewPane = 0
				m.preview = capturePaneContent(m.sessions[m.cursor], m.previewPane)
				return m, nil
			}
		}
	}

	return m, nil
}

func (m peekModel) View() string {
	if m.width == 0 || m.height == 0 {
		return "Loading..."
	}

	leftWidth := m.width * 30 / 100
	rightWidth := m.width - leftWidth - 3 // 3 for border/margin

	// Left panel: session list
	var sessionLines []string
	for i, s := range m.sessions {
		if i == m.cursor {
			style := lipgloss.NewStyle().
				Bold(true).
				Foreground(lipgloss.Color("212")).
				Background(lipgloss.Color("236"))
			sessionLines = append(sessionLines, style.Render(fmt.Sprintf(" > %s ", s)))
		} else {
			sessionLines = append(sessionLines, fmt.Sprintf("   %s", s))
		}
	}

	leftPanel := lipgloss.NewStyle().
		Width(leftWidth).
		Height(m.height - 4).
		BorderStyle(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("62")).
		Padding(1).
		Render(strings.Join(sessionLines, "\n"))

	// Right panel: pane preview
	previewContent := m.preview
	if previewContent == "" {
		previewContent = "No content"
	}

	// Truncate preview lines to fit
	lines := strings.Split(previewContent, "\n")
	maxLines := m.height - 6
	if maxLines > 0 && len(lines) > maxLines {
		lines = lines[:maxLines]
	}
	previewContent = strings.Join(lines, "\n")

	paneLabel := fmt.Sprintf(" pane %d ", m.previewPane)
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
		Render("  Peek — arrows/jk: navigate | tab: cycle panes | enter: jump | q: quit" + paneLabel)

	layout := lipgloss.JoinHorizontal(lipgloss.Top, leftPanel, rightPanel)

	return fmt.Sprintf("%s\n%s", title, layout)
}

func capturePaneContent(session string, pane int) string {
	target := fmt.Sprintf("%s:0.%d", session, pane)
	cmd := exec.Command("tmux", "capture-pane", "-t", target, "-p", "-e")
	output, err := cmd.Output()
	if err != nil {
		// If pane doesn't exist, reset to pane 0
		if pane > 0 {
			return capturePaneContent(session, 0)
		}
		return fmt.Sprintf("(unable to capture: %v)", err)
	}
	return string(output)
}
