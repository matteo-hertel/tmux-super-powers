package cmd

import (
	"bytes"
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
	"github.com/matteo-hertel/tmux-super-powers/internal/pathutil"
	tmuxpkg "github.com/matteo-hertel/tmux-super-powers/internal/tmux"
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

		dirs, err := expandDirectories(cfg.Directories, cfg.IgnoreDirectories)
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
			list:          list.New(items, delegate, 0, 0),
			textInput:     textInput,
			allDirs:       dirs,
			selectedPaths: make(map[string]bool),
			focusMode:     0,
		}
		m.list.Title = "Select directories (space=toggle, enter=confirm)"
		m.list.SetShowHelp(false)
		m.list.SetFilteringEnabled(false)

		p := tea.NewProgram(m, tea.WithAltScreen())
		finalModel, err := p.Run()
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}

		if fm, ok := finalModel.(dirModel); ok {
			openSelectedDirs(fm)
		}
	},
}

type dirItem struct {
	path     string
	selected bool
}

func (i dirItem) Title() string {
	checkbox := "[ ] "
	if i.selected {
		checkbox = "[x] "
	}
	return checkbox + filepath.Base(i.path)
}
func (i dirItem) Description() string { return i.path }
func (i dirItem) FilterValue() string { return filepath.Base(i.path) + " " + i.path }

type dirModel struct {
	list          list.Model
	textInput     textinput.Model
	allDirs       []string
	selectedPaths map[string]bool
	confirmed     bool
	focusMode     int // 0 = filter input, 1 = list
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
		m.list.SetSize(msg.Width-h, msg.Height-v-4)

	case tea.KeyMsg:
		if m.focusMode == 0 {
			// Filter input mode: only intercept control keys, let everything else go to textInput
			switch msg.String() {
			case "ctrl+c":
				return m, tea.Quit
			case "tab", "down", "up":
				if len(m.list.Items()) > 0 {
					m.focusMode = 1
					m.textInput.Blur()
				}
				return m, nil
			case "enter":
				if len(m.selectedPaths) > 0 {
					m.confirmed = true
					return m, tea.Quit
				}
			}
		} else {
			// List navigation mode
			switch msg.String() {
			case "ctrl+c", "q":
				return m, tea.Quit
			case "tab", "esc":
				m.focusMode = 0
				m.textInput.Focus()
				cmds = append(cmds, textinput.Blink)
				return m, tea.Batch(cmds...)
			case " ":
				if i, ok := m.list.SelectedItem().(dirItem); ok {
					idx := m.list.Index()
					i.selected = !i.selected
					m.list.SetItem(idx, i)
					if i.selected {
						m.selectedPaths[i.path] = true
					} else {
						delete(m.selectedPaths, i.path)
					}
				}
				return m, nil
			case "enter":
				if len(m.selectedPaths) > 0 {
					m.confirmed = true
					return m, tea.Quit
				}
				// Nothing toggled: open the highlighted item directly
				if i, ok := m.list.SelectedItem().(dirItem); ok {
					m.selectedPaths[i.path] = true
					m.confirmed = true
					return m, tea.Quit
				}
			}
		}
	}

	// Update the appropriate component based on focus
	if m.focusMode == 0 {
		m.textInput, cmd = m.textInput.Update(msg)
		cmds = append(cmds, cmd)
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
	view := fmt.Sprintf("%s\n%s", filterView, listView)

	if len(m.selectedPaths) > 0 {
		var names []string
		for path := range m.selectedPaths {
			names = append(names, filepath.Base(path))
		}
		selectedStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("62")).Bold(true)
		view += "\n" + selectedStyle.Render(fmt.Sprintf("Selected (%d): %s", len(m.selectedPaths), strings.Join(names, ", ")))
	}

	return view
}

func (m *dirModel) filterList() {
	filterText := strings.ToLower(strings.TrimSpace(m.textInput.Value()))

	if filterText == "" {
		items := make([]list.Item, len(m.allDirs))
		for i, dir := range m.allDirs {
			items[i] = dirItem{path: dir, selected: m.selectedPaths[dir]}
		}
		m.list.SetItems(items)
		return
	}

	var filteredItems []list.Item
	for _, dir := range m.allDirs {
		dirName := strings.ToLower(filepath.Base(dir))
		dirPath := strings.ToLower(dir)

		if strings.Contains(dirName, filterText) || strings.Contains(dirPath, filterText) {
			filteredItems = append(filteredItems, dirItem{path: dir, selected: m.selectedPaths[dir]})
		}
	}

	m.list.SetItems(filteredItems)
}

func expandDirectories(patterns []string, ignoreDirectories []string) ([]string, error) {
	var dirs []string
	ignoreSet := buildIgnoreSet(ignoreDirectories)

	for _, pattern := range patterns {
		expandedPattern := pathutil.ExpandPath(pattern)

		// Check if pattern ends with ** for multi-level depth
		if strings.HasSuffix(expandedPattern, "**") {
			basePath := strings.TrimSuffix(expandedPattern, "**")
			basePath = strings.TrimSuffix(basePath, string(os.PathSeparator))

			// Walk the directory tree up to 2 levels deep
			err := walkDirectoryDepth(basePath, 2, ignoreSet, func(path string) {
				dirs = append(dirs, path)
			})
			if err != nil && !os.IsNotExist(err) {
				return nil, err
			}
		} else if strings.Contains(expandedPattern, "*") {
			matches, err := filepath.Glob(expandedPattern)
			if err != nil {
				return nil, err
			}

			for _, match := range matches {
				info, err := os.Stat(match)
				if err != nil || !info.IsDir() {
					continue
				}
				name := filepath.Base(match)
				if shouldIgnoreDir(name, ignoreSet) {
					continue
				}
				if isGitIgnored(match) {
					continue
				}
				dirs = append(dirs, match)
			}
		} else {
			if info, err := os.Stat(expandedPattern); err == nil && info.IsDir() {
				dirs = append(dirs, expandedPattern)
			}
		}
	}

	return dirs, nil
}

// buildIgnoreSet creates a lookup set from user-configured ignore patterns
func buildIgnoreSet(userIgnores []string) map[string]bool {
	set := make(map[string]bool)
	for _, name := range userIgnores {
		set[strings.ToLower(name)] = true
	}
	return set
}

// shouldIgnoreDir returns true if a directory name should be excluded
func shouldIgnoreDir(name string, userIgnores map[string]bool) bool {
	// Skip hidden directories (starting with .)
	if strings.HasPrefix(name, ".") {
		return true
	}
	// Skip user-configured ignores
	if userIgnores[strings.ToLower(name)] {
		return true
	}
	return false
}

// isGitRoot checks if a directory is a git repo or worktree (has .git file or directory)
func isGitRoot(path string) bool {
	_, err := os.Stat(filepath.Join(path, ".git"))
	return err == nil
}

// isGitIgnored checks if a path is ignored by git
func isGitIgnored(path string) bool {
	cmd := exec.Command("git", "check-ignore", "-q", path)
	cmd.Dir = filepath.Dir(path)
	err := cmd.Run()
	// exit 0 = ignored, exit 1 = not ignored, exit 128 = not a git repo
	return err == nil
}

// gitIgnoredSet returns a set of paths that are gitignored from a list of candidates
func gitIgnoredSet(dir string, paths []string) map[string]bool {
	ignored := make(map[string]bool)
	if len(paths) == 0 {
		return ignored
	}

	cmd := exec.Command("git", "check-ignore", "--stdin")
	cmd.Dir = dir
	cmd.Stdin = bytes.NewReader([]byte(strings.Join(paths, "\n")))
	out, err := cmd.Output()
	if err != nil {
		return ignored
	}

	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		if line != "" {
			ignored[line] = true
		}
	}
	return ignored
}

// walkDirectoryDepth walks a directory tree up to maxDepth levels
func walkDirectoryDepth(root string, maxDepth int, ignoreSet map[string]bool, fn func(string)) error {
	if maxDepth < 0 {
		return nil
	}

	// Check if root exists and is a directory
	info, err := os.Stat(root)
	if err != nil {
		return err
	}
	if !info.IsDir() {
		return nil
	}

	// Add the root directory itself
	fn(root)

	// Walk subdirectories
	return walkDirectoryDepthRecursive(root, 0, maxDepth, ignoreSet, fn)
}

func walkDirectoryDepthRecursive(dir string, currentDepth, maxDepth int, ignoreSet map[string]bool, fn func(string)) error {
	if currentDepth >= maxDepth {
		return nil
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		return err
	}

	// Collect candidate directory paths for batch gitignore check
	var candidates []string
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		if shouldIgnoreDir(entry.Name(), ignoreSet) {
			continue
		}
		candidates = append(candidates, filepath.Join(dir, entry.Name()))
	}

	// Batch check gitignored paths
	ignored := gitIgnoredSet(dir, candidates)

	for _, path := range candidates {
		if ignored[path] {
			continue
		}
		if isGitRoot(path) {
			// Git repo or worktree: include it, don't recurse into subdirs
			fn(path)
		} else {
			// Plain directory: skip it, recurse to find repos/worktrees inside
			if err := walkDirectoryDepthRecursive(path, currentDepth+1, maxDepth, ignoreSet, fn); err != nil {
				continue
			}
		}
	}

	return nil
}

func openSelectedDirs(fm dirModel) {
	if !fm.confirmed || len(fm.selectedPaths) == 0 {
		return
	}

	var paths []string
	for path := range fm.selectedPaths {
		paths = append(paths, path)
	}

	for _, path := range paths {
		sessionName := tmuxpkg.SanitizeSessionName(filepath.Base(path))
		if !tmuxpkg.SessionExists(sessionName) {
			createSession(sessionName, path)
		}
	}

	// Switch/attach to the first created session
	sessionName := tmuxpkg.SanitizeSessionName(filepath.Base(paths[0]))
	tmuxpkg.AttachOrSwitch(sessionName)
}

func createSession(sessionName, dir string) {
	tmuxpkg.CreateTwoPaneSession(sessionName, dir, "nvim", "")
}