package cmd

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/charmbracelet/bubbles/list"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/spf13/cobra"
)

var wtxRmCmd = &cobra.Command{
	Use:   "wtx-rm",
	Short: "Interactive worktree removal",
	Long: `Present an interactive list of all worktrees and allows you to select which ones to remove.

For each selected worktree:
1. Kills associated tmux session if it exists
2. Removes the worktree
3. Deletes the associated branch
4. Removes the directory`,
	Run: func(cmd *cobra.Command, args []string) {
		if !isGitRepo() {
			fmt.Fprintf(os.Stderr, "Error: Not a git repository\n")
			os.Exit(1)
		}

		worktrees, err := getWorktrees()
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error getting worktrees: %v\n", err)
			os.Exit(1)
		}

		if len(worktrees) == 0 {
			fmt.Println("No worktrees found to remove.")
			return
		}

		items := make([]list.Item, len(worktrees))
		for i, wt := range worktrees {
			items[i] = worktreeItem{
				path:   wt.Path,
				branch: wt.Branch,
			}
		}

		delegate := list.NewDefaultDelegate()
		delegate.SetHeight(2)
		delegate.SetSpacing(0)

		m := worktreeModel{
			list:      list.New(items, delegate, 0, 0),
			selected:  make(map[int]bool),
			worktrees: worktrees,
		}
		m.list.Title = "Select worktrees to remove (space to toggle, enter to confirm)"
		m.list.SetShowHelp(false)

		p := tea.NewProgram(m, tea.WithAltScreen())
		finalModel, err := p.Run()
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}

		if fm, ok := finalModel.(worktreeModel); ok {
			removeSelectedWorktrees(fm.worktrees, fm.selected)
		}
	},
}

type Worktree struct {
	Path   string
	Branch string
}

type worktreeItem struct {
	path   string
	branch string
}

func (i worktreeItem) Title() string       { return i.branch }
func (i worktreeItem) Description() string { return i.path }
func (i worktreeItem) FilterValue() string { return i.branch + " " + i.path }

type worktreeModel struct {
	list      list.Model
	selected  map[int]bool
	worktrees []Worktree
	confirmed bool
}

func (m worktreeModel) Init() tea.Cmd {
	return nil
}

func (m worktreeModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		h, v := lipgloss.NewStyle().GetFrameSize()
		m.list.SetSize(msg.Width-h, msg.Height-v)

	case tea.KeyMsg:
		switch msg.String() {
		case "ctrl+c", "q":
			return m, tea.Quit
		case " ":
			idx := m.list.Index()
			if m.selected[idx] {
				delete(m.selected, idx)
			} else {
				m.selected[idx] = true
			}
			return m, nil
		case "enter":
			if len(m.selected) > 0 {
				m.confirmed = true
				return m, tea.Quit
			}
		}
	}

	var cmd tea.Cmd
	m.list, cmd = m.list.Update(msg)
	return m, cmd
}

func (m worktreeModel) View() string {
	view := m.list.View()
	
	var selectedItems []string
	for idx := range m.selected {
		if idx < len(m.worktrees) {
			selectedItems = append(selectedItems, m.worktrees[idx].Branch)
		}
	}
	
	if len(selectedItems) > 0 {
		view += fmt.Sprintf("\nSelected: %s", strings.Join(selectedItems, ", "))
	}
	
	return view
}

func getWorktrees() ([]Worktree, error) {
	cmd := exec.Command("git", "worktree", "list", "--porcelain")
	output, err := cmd.Output()
	if err != nil {
		return nil, err
	}

	var worktrees []Worktree
	lines := strings.Split(strings.TrimSpace(string(output)), "\n")
	
	var currentWorktree Worktree
	isMainWorktree := true
	
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			if !isMainWorktree && currentWorktree.Path != "" && currentWorktree.Branch != "" {
				worktrees = append(worktrees, currentWorktree)
			}
			currentWorktree = Worktree{}
			isMainWorktree = true
			continue
		}
		
		if strings.HasPrefix(line, "worktree ") {
			currentWorktree.Path = strings.TrimPrefix(line, "worktree ")
			if len(worktrees) > 0 || strings.Contains(line, "-") {
				isMainWorktree = false
			}
		} else if strings.HasPrefix(line, "branch ") {
			branch := strings.TrimPrefix(line, "branch ")
			branch = strings.TrimPrefix(branch, "refs/heads/")
			currentWorktree.Branch = branch
		}
	}
	
	if !isMainWorktree && currentWorktree.Path != "" && currentWorktree.Branch != "" {
		worktrees = append(worktrees, currentWorktree)
	}

	return worktrees, nil
}

func removeSelectedWorktrees(worktrees []Worktree, selected map[int]bool) {
	repoName := getRepoName()

	for idx := range selected {
		if idx >= len(worktrees) {
			continue
		}

		wt := worktrees[idx]
		sessionName := fmt.Sprintf("%s-%s", repoName, wt.Branch)

		fmt.Printf("Removing worktree: %s (%s)\n", wt.Branch, wt.Path)

		// Kill tmux session first
		if tmuxSessionExists(sessionName) {
			fmt.Printf("  Killing tmux session '%s'...\n", sessionName)
			exec.Command("tmux", "kill-session", "-t", sessionName).Run()
		}

		// Remove the worktree reference
		cmd := exec.Command("git", "worktree", "remove", wt.Path, "--force")
		if err := cmd.Run(); err != nil {
			fmt.Printf("  Error: Failed to remove worktree: %v\n", err)
			// Continue anyway to try to clean up the directory
		} else {
			fmt.Println("  Worktree removed successfully.")
		}

		// Explicitly delete the directory to ensure complete cleanup
		if _, err := os.Stat(wt.Path); err == nil {
			fmt.Printf("  Removing directory '%s'...\n", wt.Path)
			if err := os.RemoveAll(wt.Path); err != nil {
				fmt.Printf("  Warning: Failed to remove directory: %v\n", err)
			} else {
				fmt.Println("  Directory removed successfully.")
			}
		}

		// Clean up empty parent directories (whether git removed the dir or we did)
		cleanupEmptyParents(wt.Path)

		// Delete the branch
		cmd = exec.Command("git", "branch", "-D", wt.Branch)
		if err := cmd.Run(); err != nil {
			fmt.Printf("  Warning: Failed to delete branch '%s': %v\n", wt.Branch, err)
		} else {
			fmt.Printf("  Branch '%s' deleted successfully.\n", wt.Branch)
		}
	}

	fmt.Println("Worktree removal completed.")
}

func getRepoName() string {
	if repoRoot, err := getRepoRoot(); err == nil {
		return filepath.Base(repoRoot)
	}
	return "unknown"
}

func tmuxSessionExists(sessionName string) bool {
	cmd := exec.Command("tmux", "has-session", "-t", sessionName)
	return cmd.Run() == nil
}

// isDirEmpty checks if a directory is empty
func isDirEmpty(path string) (bool, error) {
	entries, err := os.ReadDir(path)
	if err != nil {
		return false, err
	}
	return len(entries) == 0, nil
}

// cleanupEmptyParents recursively removes empty parent directories
// Stops at the home directory or repository root to avoid removing important directories
func cleanupEmptyParents(path string) {
	parent := filepath.Dir(path)

	// Safety checks: don't remove home directory or root
	homeDir, _ := os.UserHomeDir()
	if parent == homeDir || parent == "/" || parent == "." {
		return
	}

	// Don't remove the repository root
	if repoRoot, err := getRepoRoot(); err == nil {
		if parent == repoRoot {
			return
		}
	}

	// Check if parent directory exists and is empty
	if info, err := os.Stat(parent); err == nil && info.IsDir() {
		if isEmpty, err := isDirEmpty(parent); err == nil && isEmpty {
			fmt.Printf("  Removing empty parent directory '%s'...\n", parent)
			if err := os.Remove(parent); err != nil {
				fmt.Printf("  Warning: Failed to remove empty directory: %v\n", err)
			} else {
				fmt.Println("  Empty directory removed successfully.")
				// Recursively check the parent's parent
				cleanupEmptyParents(parent)
			}
		}
	}
}