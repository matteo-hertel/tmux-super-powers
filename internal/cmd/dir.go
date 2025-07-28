package cmd

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/charmbracelet/bubbles/list"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/matteo-hertel/tmux-super-powers/config"
	"github.com/spf13/cobra"
)

var dirCmd = &cobra.Command{
	Use:   "dir",
	Short: "Select and open directory from config",
	Run: func(cmd *cobra.Command, args []string) {
		cfg, err := config.Load()
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error loading config: %v\n", err)
			os.Exit(1)
		}

		if len(cfg.Directories) == 0 {
			fmt.Println("No directories configured")
			return
		}

		dirs, err := expandDirectories(cfg.Directories)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error expanding directories: %v\n", err)
			os.Exit(1)
		}

		if len(dirs) == 0 {
			fmt.Println("No directories found")
			return
		}

		items := make([]list.Item, len(dirs))
		for i, dir := range dirs {
			items[i] = dirItem{path: dir}
		}

		delegate := list.NewDefaultDelegate()
		delegate.ShowDescription = true
		
		textInput := textinput.New()
		textInput.Placeholder = "Type to filter directories..."
		textInput.Focus()
		textInput.Width = 50
		
		m := dirModel{
			list:      list.New(items, delegate, 0, 0),
			textInput: textInput,
			allDirs:   dirs,
			focusMode: 0,
		}
		m.list.Title = "Select a directory"
		m.list.SetShowHelp(false)

		p := tea.NewProgram(m, tea.WithAltScreen())
		finalModel, err := p.Run()
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}

		if fm, ok := finalModel.(dirModel); ok && fm.selected != "" {
			openInTmux(fm.selected)
		}
	},
}

type dirItem struct {
	path string
}

func (i dirItem) Title() string       { return filepath.Base(i.path) }
func (i dirItem) Description() string { return i.path }
func (i dirItem) FilterValue() string { return filepath.Base(i.path) + " " + i.path }

type dirModel struct {
	list      list.Model
	textInput textinput.Model
	allDirs   []string
	selected  string
	focusMode int // 0 = filter input, 1 = list
}

func (m dirModel) Init() tea.Cmd {
	return textinput.Blink
}

func (m dirModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmd tea.Cmd
	var cmds []tea.Cmd

	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		h, v := lipgloss.NewStyle().GetFrameSize()
		m.list.SetSize(msg.Width-h, msg.Height-v-3) // Reserve space for filter input

	case tea.KeyMsg:
		switch msg.String() {
		case "ctrl+c", "q":
			return m, tea.Quit
		case "tab":
			if m.focusMode == 0 {
				m.focusMode = 1
				m.textInput.Blur()
			} else {
				m.focusMode = 0
				m.textInput.Focus()
				cmds = append(cmds, textinput.Blink)
			}
		case "enter":
			if m.focusMode == 1 {
				if i, ok := m.list.SelectedItem().(dirItem); ok {
					m.selected = i.path
					return m, tea.Quit
				}
			}
		case "up", "k":
			if m.focusMode == 0 && len(m.list.Items()) > 0 {
				m.focusMode = 1
				m.textInput.Blur()
			}
		case "down", "j":
			if m.focusMode == 0 && len(m.list.Items()) > 0 {
				m.focusMode = 1
				m.textInput.Blur()
			}
		}
	}

	// Update the appropriate component based on focus
	if m.focusMode == 0 {
		m.textInput, cmd = m.textInput.Update(msg)
		cmds = append(cmds, cmd)
		// Filter the list based on input
		m.filterList()
	} else {
		m.list, cmd = m.list.Update(msg)
		cmds = append(cmds, cmd)
	}

	return m, tea.Batch(cmds...)
}

func (m dirModel) View() string {
	filterStyle := lipgloss.NewStyle().MarginBottom(1)
	if m.focusMode == 0 {
		filterStyle = filterStyle.BorderStyle(lipgloss.RoundedBorder()).BorderForeground(lipgloss.Color("62"))
	}
	
	filterView := filterStyle.Render(m.textInput.View())
	listView := m.list.View()
	
	return fmt.Sprintf("%s\n%s", filterView, listView)
}

func (m *dirModel) filterList() {
	filterText := strings.ToLower(strings.TrimSpace(m.textInput.Value()))
	
	if filterText == "" {
		// Show all directories
		items := make([]list.Item, len(m.allDirs))
		for i, dir := range m.allDirs {
			items[i] = dirItem{path: dir}
		}
		m.list.SetItems(items)
		return
	}
	
	// Filter directories
	var filteredItems []list.Item
	for _, dir := range m.allDirs {
		dirName := strings.ToLower(filepath.Base(dir))
		dirPath := strings.ToLower(dir)
		
		if strings.Contains(dirName, filterText) || strings.Contains(dirPath, filterText) {
			filteredItems = append(filteredItems, dirItem{path: dir})
		}
	}
	
	m.list.SetItems(filteredItems)
}

func expandPath(path string) string {
	if path[:2] == "~/" {
		home, _ := os.UserHomeDir()
		path = filepath.Join(home, path[2:])
	}
	return path
}

func expandDirectories(patterns []string) ([]string, error) {
	var dirs []string
	
	for _, pattern := range patterns {
		expandedPattern := expandPath(pattern)
		
		if strings.Contains(expandedPattern, "*") {
			matches, err := filepath.Glob(expandedPattern)
			if err != nil {
				return nil, err
			}
			
			for _, match := range matches {
				if info, err := os.Stat(match); err == nil && info.IsDir() {
					dirs = append(dirs, match)
				}
			}
		} else {
			if info, err := os.Stat(expandedPattern); err == nil && info.IsDir() {
				dirs = append(dirs, expandedPattern)
			}
		}
	}
	
	return dirs, nil
}

func openInTmux(dir string) {
	sessionName := filepath.Base(dir)
	
	checkCmd := exec.Command("tmux", "has-session", "-t", sessionName)
	if checkCmd.Run() != nil {
		createSession(sessionName, dir)
	}
	
	if os.Getenv("TMUX") != "" {
		cmd := exec.Command("tmux", "switch-client", "-t", sessionName)
		cmd.Run()
	} else {
		cmd := exec.Command("tmux", "attach-session", "-t", sessionName)
		cmd.Stdin = os.Stdin
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		cmd.Run()
	}
}

func createSession(sessionName, dir string) {
	exec.Command("tmux", "new-session", "-d", "-s", sessionName).Run()
	
	exec.Command("tmux", "send-keys", "-t", sessionName+":0", "cd "+dir+";nvim", "C-m").Run()
	
	exec.Command("tmux", "split-window", "-t", sessionName+":0", "-h", "-l", "35").Run()
	
	exec.Command("tmux", "select-pane", "-t", sessionName+":0.0").Run()
	
	exec.Command("tmux", "send-keys", "-t", sessionName+":0.1", "cd "+dir, "C-m").Run()
}