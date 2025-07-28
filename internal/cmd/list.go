package cmd

import (
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/charmbracelet/bubbles/list"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/spf13/cobra"
)

var listCmd = &cobra.Command{
	Use:   "txl",
	Short: "List and select tmux sessions",
	Run: func(cmd *cobra.Command, args []string) {
		sessions, err := getTmuxSessions()
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error getting tmux sessions: %v\n", err)
			os.Exit(1)
		}

		if len(sessions) == 0 {
			fmt.Println("No tmux sessions found")
			return
		}

		items := make([]list.Item, len(sessions))
		for i, session := range sessions {
			items[i] = sessionItem{name: session}
		}

		delegate := list.NewDefaultDelegate()
		delegate.SetHeight(1)
		delegate.SetSpacing(0)
		delegate.ShowDescription = false
		
		m := sessionModel{
			list: list.New(items, delegate, 0, 0),
		}
		m.list.Title = "Select a tmux session"
		m.list.SetShowHelp(false)

		p := tea.NewProgram(m, tea.WithAltScreen())
		finalModel, err := p.Run()
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}

		if fm, ok := finalModel.(sessionModel); ok && fm.selected != "" {
			attachToSession(fm.selected)
		}
	},
}

type sessionItem struct {
	name string
}

func (i sessionItem) Title() string       { return i.name }
func (i sessionItem) Description() string { return "" }
func (i sessionItem) FilterValue() string { return i.name }

type sessionModel struct {
	list     list.Model
	selected string
}

func (m sessionModel) Init() tea.Cmd {
	return nil
}

func (m sessionModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		h, v := lipgloss.NewStyle().GetFrameSize()
		m.list.SetSize(msg.Width-h, msg.Height-v)

	case tea.KeyMsg:
		switch msg.String() {
		case "ctrl+c", "q":
			return m, tea.Quit
		case "enter":
			if i, ok := m.list.SelectedItem().(sessionItem); ok {
				m.selected = i.name
				return m, tea.Quit
			}
		}
	}

	var cmd tea.Cmd
	m.list, cmd = m.list.Update(msg)
	return m, cmd
}

func (m sessionModel) View() string {
	return m.list.View()
}

func getTmuxSessions() ([]string, error) {
	cmd := exec.Command("tmux", "list-sessions", "-F", "#{session_name}")
	output, err := cmd.Output()
	if err != nil {
		if strings.Contains(err.Error(), "no server running") {
			return []string{}, nil
		}
		return nil, err
	}

	sessions := strings.Split(strings.TrimSpace(string(output)), "\n")
	return sessions, nil
}

func attachToSession(session string) {
	if os.Getenv("TMUX") != "" {
		cmd := exec.Command("tmux", "switch-client", "-t", session)
		cmd.Run()
	} else {
		cmd := exec.Command("tmux", "attach-session", "-t", session)
		cmd.Stdin = os.Stdin
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		cmd.Run()
	}
}