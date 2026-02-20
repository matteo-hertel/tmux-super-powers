package cmd

import (
	"fmt"
	"io"
	"os"
	"os/exec"

	"github.com/charmbracelet/bubbles/list"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	tmuxpkg "github.com/matteo-hertel/tmux-super-powers/internal/tmux"
	"github.com/spf13/cobra"
)

var rmCmd = &cobra.Command{
	Use:   "rm",
	Short: "Remove tmux sessions (smart worktree cleanup)",
	Long: `Interactive multi-select removal of tmux sessions.

Sessions that correspond to git worktrees get full cleanup:
- Kill tmux session
- Remove worktree directory
- Delete git branch

Plain sessions just get killed.

Flags:
  --sessions-only  Skip worktree cleanup, just kill tmux sessions`,
	Run: func(cmd *cobra.Command, args []string) {
		sessionsOnly, _ := cmd.Flags().GetBool("sessions-only")

		sessions, err := getTmuxSessions()
		if err != nil || len(sessions) == 0 {
			fmt.Println("No tmux sessions found")
			return
		}

		// Build worktree map for smart detection
		var wtMap map[string]Worktree
		if !sessionsOnly && isGitRepo() {
			worktrees, _ := getWorktrees()
			wtMap = make(map[string]Worktree)
			repoName := getRepoName()
			for _, wt := range worktrees {
				sessName := tmuxpkg.SanitizeSessionName(fmt.Sprintf("%s-%s", repoName, wt.Branch))
				wtMap[sessName] = wt
			}
		}

		items := make([]list.Item, len(sessions))
		for i, session := range sessions {
			_, isWt := wtMap[session]
			items[i] = rmItem{name: session, isWorktree: isWt}
		}

		delegate := newRmDelegate()
		m := rmModel{
			list:  list.New(items, delegate, 0, 0),
			wtMap: wtMap,
		}
		m.list.Title = "Select sessions to remove (space to toggle, enter to confirm)"
		m.list.SetShowHelp(false)

		p := tea.NewProgram(m, tea.WithAltScreen())
		finalModel, err := p.Run()
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}

		if fm, ok := finalModel.(rmModel); ok && len(fm.toRemove) > 0 {
			for _, session := range fm.toRemove {
				if wt, isWt := fm.wtMap[session]; isWt && !sessionsOnly {
					fmt.Printf("Removing worktree session: %s\n", session)
					tmuxpkg.KillSession(session)
					os.RemoveAll(wt.Path)
					exec.Command("git", "worktree", "remove", wt.Path, "--force").Run()
					exec.Command("git", "branch", "-D", wt.Branch).Run()
					fmt.Printf("  Worktree, branch, and session removed.\n")
				} else {
					fmt.Printf("Killing session: %s\n", session)
					tmuxpkg.KillSession(session)
				}
			}
			fmt.Println("Done.")
		}
	},
}

func init() {
	rmCmd.Flags().Bool("sessions-only", false, "Only kill tmux sessions, skip worktree cleanup")
}

type rmItem struct {
	name       string
	selected   bool
	isWorktree bool
}

func (i rmItem) Title() string       { return i.name }
func (i rmItem) Description() string { return "" }
func (i rmItem) FilterValue() string { return i.name }

type rmModel struct {
	list     list.Model
	toRemove []string
	wtMap    map[string]Worktree
}

func (m rmModel) Init() tea.Cmd { return nil }

func (m rmModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		h, v := lipgloss.NewStyle().GetFrameSize()
		m.list.SetSize(msg.Width-h, msg.Height-v)
	case tea.KeyMsg:
		switch msg.String() {
		case "ctrl+c", "q":
			return m, tea.Quit
		case " ":
			if i, ok := m.list.SelectedItem().(rmItem); ok {
				idx := m.list.Index()
				i.selected = !i.selected
				m.list.SetItem(idx, i)
			}
		case "enter":
			for _, item := range m.list.Items() {
				if si, ok := item.(rmItem); ok && si.selected {
					m.toRemove = append(m.toRemove, si.name)
				}
			}
			return m, tea.Quit
		}
	}
	var cmd tea.Cmd
	m.list, cmd = m.list.Update(msg)
	return m, cmd
}

func (m rmModel) View() string {
	return m.list.View()
}

type rmDelegate struct {
	list.DefaultDelegate
}

func newRmDelegate() rmDelegate {
	d := list.NewDefaultDelegate()
	d.SetHeight(1)
	d.SetSpacing(0)
	d.ShowDescription = false
	return rmDelegate{DefaultDelegate: d}
}

func (d rmDelegate) Render(w io.Writer, m list.Model, index int, item list.Item) {
	if si, ok := item.(rmItem); ok {
		checkbox := "[ ] "
		if si.selected {
			checkbox = "[x] "
		}
		label := si.name
		if si.isWorktree {
			label += " (worktree)"
		}
		str := checkbox + label
		if index == m.Index() {
			str = d.Styles.SelectedTitle.Render(str)
		} else {
			str = d.Styles.NormalTitle.Render(str)
		}
		fmt.Fprint(w, str)
	}
}
