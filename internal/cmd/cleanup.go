package cmd

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/charmbracelet/bubbles/list"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/matteo-hertel/tmux-super-powers/config"
	"github.com/matteo-hertel/tmux-super-powers/internal/pathutil"
	"github.com/matteo-hertel/tmux-super-powers/internal/service"
	"github.com/spf13/cobra"
)

// cleanupEntry represents a directory candidate for cleanup.
type cleanupEntry struct {
	path     string // full path
	name     string // directory name
	branch   string // git branch (if worktree)
	mainRepo string // main repo path (if worktree)
	kind     string // "worktree", "worktree (dirty)", "empty"
	selected bool
}

func (e cleanupEntry) Title() string       { return e.name }
func (e cleanupEntry) Description() string { return "" }
func (e cleanupEntry) FilterValue() string { return e.name }

var cleanupCmd = &cobra.Command{
	Use:   "cleanup",
	Short: "Interactive cleanup of worktree directories",
	Long: `Scans the spawn worktree base directory and presents an interactive
list of worktrees and empty directories that can be removed.

Select items with space, confirm with enter. Removal includes:
  - Git worktree removal (+ branch deletion + ref pruning)
  - Empty directory removal`,
	Run: func(cmd *cobra.Command, args []string) {
		cfg, err := config.Load()
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error loading config: %v\n", err)
			os.Exit(1)
		}

		base := pathutil.ExpandPath(cfg.Spawn.WorktreeBase)
		info, err := os.Stat(base)
		if err != nil || !info.IsDir() {
			fmt.Printf("Worktree base %s does not exist or is not a directory.\n", base)
			return
		}

		// Get active tmux sessions
		activeSessions := make(map[string]bool)
		sessions, _ := service.ListSessions()
		for _, s := range sessions {
			activeSessions[s] = true
		}

		dirEntries, err := os.ReadDir(base)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error reading %s: %v\n", base, err)
			os.Exit(1)
		}

		var candidates []cleanupEntry
		for _, de := range dirEntries {
			if !de.IsDir() {
				continue
			}
			dirPath := filepath.Join(base, de.Name())
			gitFile := filepath.Join(dirPath, ".git")

			fi, err := os.Lstat(gitFile)
			if err != nil {
				// No .git at all
				children, readErr := os.ReadDir(dirPath)
				if readErr == nil && len(children) == 0 {
					candidates = append(candidates, cleanupEntry{
						path: dirPath,
						name: de.Name(),
						kind: "empty",
					})
				} else {
					candidates = append(candidates, cleanupEntry{
						path: dirPath,
						name: de.Name(),
						kind: "directory",
					})
				}
				continue
			}

			if fi.IsDir() {
				// .git is a directory → regular repo
				entry := cleanupEntry{
					path:   dirPath,
					name:   de.Name(),
					branch: getWorktreeBranch(dirPath),
					kind:   "repo",
				}
				if isWorktreeDirty(dirPath) {
					entry.kind = "repo (dirty)"
				}
				candidates = append(candidates, entry)
				continue
			}

			// .git is a file → worktree
			entry := cleanupEntry{
				path:     dirPath,
				name:     de.Name(),
				mainRepo: resolveMainRepo(gitFile),
				branch:   getWorktreeBranch(dirPath),
				kind:     "worktree",
			}
			if isWorktreeDirty(dirPath) {
				entry.kind = "worktree (dirty)"
			}
			candidates = append(candidates, entry)
		}

		if len(candidates) == 0 {
			fmt.Println("Nothing to clean up.")
			return
		}

		items := make([]list.Item, len(candidates))
		for i := range candidates {
			items[i] = candidates[i]
		}

		delegate := newCleanupDelegate()
		m := cleanupModel{
			list:       list.New(items, delegate, 0, 0),
			candidates: candidates,
		}
		m.list.Title = "Select items to remove (space to toggle, enter to confirm)"
		m.list.SetShowHelp(false)

		p := tea.NewProgram(m, tea.WithAltScreen())
		finalModel, err := p.Run()
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}

		fm, ok := finalModel.(cleanupModel)
		if !ok || len(fm.toRemove) == 0 {
			return
		}

		reposToPrune := make(map[string]bool)
		for _, entry := range fm.toRemove {
			switch {
			case strings.HasPrefix(entry.kind, "worktree"):
				if entry.mainRepo != "" {
					rmCmd := exec.Command("git", "-C", entry.mainRepo, "worktree", "remove", entry.path, "--force")
					if err := rmCmd.Run(); err != nil {
						os.RemoveAll(entry.path)
					}
					reposToPrune[entry.mainRepo] = true
				} else {
					os.RemoveAll(entry.path)
				}
				if entry.branch != "" && entry.mainRepo != "" {
					exec.Command("git", "-C", entry.mainRepo, "branch", "-D", entry.branch).Run()
				}
				fmt.Printf("Removed worktree: %s (branch: %s)\n", entry.name, entry.branch)

			case entry.kind == "empty":
				os.Remove(entry.path)
				fmt.Printf("Removed empty dir: %s\n", entry.name)

			default: // "repo", "repo (dirty)", "directory"
				os.RemoveAll(entry.path)
				fmt.Printf("Removed: %s\n", entry.name)
			}
		}

		for repo := range reposToPrune {
			pruneCmd := exec.Command("git", "-C", repo, "worktree", "prune")
			if err := pruneCmd.Run(); err == nil {
				fmt.Printf("Pruned worktree refs in %s\n", repo)
			}
		}

		fmt.Println("Done.")
	},
}

// --- TUI model (same pattern as tsp rm) ---

type cleanupModel struct {
	list       list.Model
	candidates []cleanupEntry
	toRemove   []cleanupEntry
}

func (m cleanupModel) Init() tea.Cmd { return nil }

func (m cleanupModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		h, v := lipgloss.NewStyle().GetFrameSize()
		m.list.SetSize(msg.Width-h, msg.Height-v)
	case tea.KeyMsg:
		switch msg.String() {
		case "ctrl+c", "q":
			return m, tea.Quit
		case " ":
			if item, ok := m.list.SelectedItem().(cleanupEntry); ok {
				idx := m.list.Index()
				item.selected = !item.selected
				m.list.SetItem(idx, item)
			}
		case "enter":
			for _, item := range m.list.Items() {
				if e, ok := item.(cleanupEntry); ok && e.selected {
					m.toRemove = append(m.toRemove, e)
				}
			}
			return m, tea.Quit
		}
	}
	var cmd tea.Cmd
	m.list, cmd = m.list.Update(msg)
	return m, cmd
}

func (m cleanupModel) View() string {
	return m.list.View()
}

// --- delegate ---

type cleanupDelegate struct {
	list.DefaultDelegate
}

func newCleanupDelegate() cleanupDelegate {
	d := list.NewDefaultDelegate()
	d.SetHeight(1)
	d.SetSpacing(0)
	d.ShowDescription = false
	return cleanupDelegate{DefaultDelegate: d}
}

func (d cleanupDelegate) Render(w io.Writer, m list.Model, index int, item list.Item) {
	if e, ok := item.(cleanupEntry); ok {
		checkbox := "[ ] "
		if e.selected {
			checkbox = "[x] "
		}
		label := e.name + " (" + e.kind + ")"
		str := checkbox + label
		if index == m.Index() {
			str = d.Styles.SelectedTitle.Render(str)
		} else {
			str = d.Styles.NormalTitle.Render(str)
		}
		fmt.Fprint(w, str)
	}
}

// --- helpers ---

// resolveMainRepo reads a worktree's .git file to find the main repository path.
func resolveMainRepo(gitFilePath string) string {
	data, err := os.ReadFile(gitFilePath)
	if err != nil {
		return ""
	}
	line := strings.TrimSpace(string(data))
	if !strings.HasPrefix(line, "gitdir: ") {
		return ""
	}
	gitDir := strings.TrimPrefix(line, "gitdir: ")
	for {
		parent := filepath.Dir(gitDir)
		if parent == gitDir {
			return ""
		}
		if filepath.Base(gitDir) == ".git" {
			return filepath.Dir(gitDir)
		}
		gitDir = parent
	}
}

func isWorktreeDirty(dir string) bool {
	cmd := exec.Command("git", "-C", dir, "status", "--porcelain")
	out, err := cmd.Output()
	if err != nil {
		return true
	}
	return strings.TrimSpace(string(out)) != ""
}

func getWorktreeBranch(dir string) string {
	cmd := exec.Command("git", "-C", dir, "rev-parse", "--abbrev-ref", "HEAD")
	out, err := cmd.Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}
