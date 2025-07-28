package cmd

import (
	"fmt"
	"io"
	"os"
	"os/exec"

	"github.com/charmbracelet/bubbles/list"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/spf13/cobra"
)

var txrmCmd = &cobra.Command{
	Use:   "txrm",
	Short: "Multi-select and remove tmux sessions",
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
			items[i] = removableSessionItem{name: session, selected: false}
		}

		delegate := newRemovalDelegate()
		
		m := removalModel{
			list: list.New(items, delegate, 0, 0),
		}
		m.list.Title = "Select sessions to remove (Space to toggle, Enter to confirm)"
		m.list.SetShowHelp(false)

		p := tea.NewProgram(m, tea.WithAltScreen())
		finalModel, err := p.Run()
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}

		if fm, ok := finalModel.(removalModel); ok && len(fm.selectedSessions) > 0 {
			for _, session := range fm.selectedSessions {
				fmt.Printf("Killing session: %s\n", session)
				killSession(session)
			}
		}
	},
}

type removableSessionItem struct {
	name     string
	selected bool
}

func (i removableSessionItem) Title() string       { return i.name }
func (i removableSessionItem) Description() string { return "" }
func (i removableSessionItem) FilterValue() string { return i.name }

type removalModel struct {
	list             list.Model
	selectedSessions []string
}

func (m removalModel) Init() tea.Cmd {
	return nil
}

func (m removalModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		h, v := lipgloss.NewStyle().GetFrameSize()
		m.list.SetSize(msg.Width-h, msg.Height-v)

	case tea.KeyMsg:
		switch msg.String() {
		case "ctrl+c", "q":
			return m, tea.Quit
		case " ":
			if i, ok := m.list.SelectedItem().(removableSessionItem); ok {
				idx := m.list.Index()
				i.selected = !i.selected
				m.list.SetItem(idx, i)
			}
		case "enter":
			// Collect all selected sessions
			m.selectedSessions = []string{}
			for _, item := range m.list.Items() {
				if si, ok := item.(removableSessionItem); ok && si.selected {
					m.selectedSessions = append(m.selectedSessions, si.name)
				}
			}
			return m, tea.Quit
		}
	}

	var cmd tea.Cmd
	m.list, cmd = m.list.Update(msg)
	return m, cmd
}

func (m removalModel) View() string {
	return m.list.View()
}

type removalDelegate struct {
	list.DefaultDelegate
}

func newRemovalDelegate() removalDelegate {
	d := list.NewDefaultDelegate()
	d.SetHeight(1)
	d.SetSpacing(0)
	d.ShowDescription = false

	d.Styles.SelectedTitle = d.Styles.SelectedTitle.Copy().
		Border(lipgloss.NormalBorder(), false, false, false, true).
		BorderForeground(lipgloss.AdaptiveColor{Light: "#F793FF", Dark: "#AD58B4"}).
		Foreground(lipgloss.AdaptiveColor{Light: "#EE6FF8", Dark: "#EE6FF8"})

	return removalDelegate{DefaultDelegate: d}
}

func (d removalDelegate) Render(w io.Writer, m list.Model, index int, item list.Item) {
	if si, ok := item.(removableSessionItem); ok {
		var checkbox string
		if si.selected {
			checkbox = "[x] "
		} else {
			checkbox = "[ ] "
		}

		str := checkbox + si.name

		if index == m.Index() {
			str = d.Styles.SelectedTitle.Render(str)
		} else {
			str = d.Styles.NormalTitle.Render(str)
		}

		fmt.Fprint(w, str)
	}
}

func killSession(session string) {
	cmd := exec.Command("tmux", "kill-session", "-t", session)
	cmd.Run()
}