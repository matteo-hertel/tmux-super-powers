package cmd

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"

	"github.com/charmbracelet/bubbles/list"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	tmuxpkg "github.com/matteo-hertel/tmux-super-powers/internal/tmux"
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
	return parseWorktreesPorcelain(string(output)), nil
}

// parseWorktreesPorcelain parses the output of `git worktree list --porcelain`.
// Skips the first entry (main worktree) by index.
func parseWorktreesPorcelain(output string) []Worktree {
	if output == "" {
		return nil
	}

	var worktrees []Worktree
	lines := strings.Split(strings.TrimSpace(output), "\n")

	var current Worktree
	entryIndex := 0

	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			if entryIndex > 0 && current.Path != "" && current.Branch != "" {
				worktrees = append(worktrees, current)
			}
			current = Worktree{}
			entryIndex++
			continue
		}

		if strings.HasPrefix(line, "worktree ") {
			current.Path = strings.TrimPrefix(line, "worktree ")
		} else if strings.HasPrefix(line, "branch ") {
			branch := strings.TrimPrefix(line, "branch ")
			branch = strings.TrimPrefix(branch, "refs/heads/")
			current.Branch = branch
		}
	}

	// Handle last entry (no trailing blank line)
	if entryIndex > 0 && current.Path != "" && current.Branch != "" {
		worktrees = append(worktrees, current)
	}

	return worktrees
}

func removeSelectedWorktrees(worktrees []Worktree, selected map[int]bool) {
	repoName := getRepoName()

	// Collect selected worktrees
	type selectedWorktree struct {
		idx int
		wt  Worktree
	}
	var toRemove []selectedWorktree
	for idx := range selected {
		if idx < len(worktrees) {
			toRemove = append(toRemove, selectedWorktree{idx: idx, wt: worktrees[idx]})
		}
	}

	// Phase 1: Parallel — kill tmux sessions and remove directories
	var wg sync.WaitGroup
	results := make([]string, len(worktrees))

	for _, sw := range toRemove {
		wg.Add(1)
		go func(idx int, wt Worktree) {
			defer wg.Done()
			sessionName := tmuxpkg.SanitizeSessionName(fmt.Sprintf("%s-%s", repoName, wt.Branch))
			var out strings.Builder

			out.WriteString(fmt.Sprintf("Removing worktree: %s (%s)\n", wt.Branch, wt.Path))

			if tmuxpkg.SessionExists(sessionName) {
				out.WriteString(fmt.Sprintf("  Killing tmux session '%s'...\n", sessionName))
				tmuxpkg.KillSession(sessionName)
			}

			if _, err := os.Stat(wt.Path); err == nil {
				out.WriteString(fmt.Sprintf("  Removing directory '%s'...\n", wt.Path))
				if err := os.RemoveAll(wt.Path); err != nil {
					out.WriteString(fmt.Sprintf("  Warning: Failed to remove directory: %v\n", err))
				} else {
					out.WriteString("  Directory removed successfully.\n")
				}
			}

			cleanupEmptyParentsCollect(wt.Path, &out)
			results[idx] = out.String()
		}(sw.idx, sw.wt)
	}

	wg.Wait()

	// Print parallel phase results
	for _, r := range results {
		if r != "" {
			fmt.Print(r)
		}
	}

	// Phase 2: Sequential — git operations (avoids lock contention)
	for _, sw := range toRemove {
		wt := sw.wt

		cmd := exec.Command("git", "worktree", "remove", wt.Path, "--force")
		if err := cmd.Run(); err != nil {
			fmt.Printf("  Warning: git worktree remove failed for %s: %v\n", wt.Branch, err)
		} else {
			fmt.Printf("  Worktree reference for '%s' removed.\n", wt.Branch)
		}

		cmd = exec.Command("git", "branch", "-D", wt.Branch)
		if err := cmd.Run(); err != nil {
			fmt.Printf("  Warning: Failed to delete branch '%s': %v\n", wt.Branch, err)
		} else {
			fmt.Printf("  Branch '%s' deleted.\n", wt.Branch)
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

// isDirEmpty checks if a directory is empty
func isDirEmpty(path string) (bool, error) {
	entries, err := os.ReadDir(path)
	if err != nil {
		return false, err
	}
	return len(entries) == 0, nil
}

// cleanupEmptyParentsCollect recursively removes empty parent directories, writing output to a builder
// for use in concurrent goroutines
func cleanupEmptyParentsCollect(path string, out *strings.Builder) {
	parent := filepath.Dir(path)

	homeDir, _ := os.UserHomeDir()
	if parent == homeDir || parent == "/" || parent == "." {
		return
	}

	if repoRoot, err := getRepoRoot(); err == nil {
		if parent == repoRoot {
			return
		}
	}

	if info, err := os.Stat(parent); err == nil && info.IsDir() {
		if isEmpty, err := isDirEmpty(parent); err == nil && isEmpty {
			out.WriteString(fmt.Sprintf("  Removing empty parent directory '%s'...\n", parent))
			if err := os.Remove(parent); err != nil {
				out.WriteString(fmt.Sprintf("  Warning: Failed to remove empty directory: %v\n", err))
			} else {
				out.WriteString("  Empty directory removed successfully.\n")
				cleanupEmptyParentsCollect(parent, out)
			}
		}
	}
}