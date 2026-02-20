package cmd

import (
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	tmuxpkg "github.com/matteo-hertel/tmux-super-powers/internal/tmux"
	"github.com/spf13/cobra"
)

var harvestCmd = &cobra.Command{
	Use:   "harvest [session-name...]",
	Short: "Review diffs from all worktrees and take action",
	Long: `Collect and review changes from all active worktrees.

Key bindings:
  j/k or arrows  Navigate worktrees
  enter           Jump to session
  p               Create PR for selected worktree
  m               Merge branch to base and cleanup
  x               Discard changes and remove worktree
  c               Send follow-up prompt to agent
  f               Fix CI — fetch failing logs, send to agent
  r               Address PR review comments
  o               Jump to session
  q/esc           Quit`,
	Run: func(cmd *cobra.Command, args []string) {
		if !isGitRepo() {
			fmt.Fprintf(os.Stderr, "Error: not a git repository\n")
			os.Exit(1)
		}

		worktrees, err := getWorktrees()
		if err != nil || len(worktrees) == 0 {
			fmt.Println("No worktrees found")
			return
		}

		repoName := getRepoName()

		// Filter by args if provided
		if len(args) > 0 {
			filter := make(map[string]bool)
			for _, a := range args {
				filter[a] = true
			}
			var filtered []Worktree
			for _, wt := range worktrees {
				sessName := tmuxpkg.SanitizeSessionName(fmt.Sprintf("%s-%s", repoName, wt.Branch))
				if filter[sessName] || filter[wt.Branch] {
					filtered = append(filtered, wt)
				}
			}
			worktrees = filtered
		}

		if len(worktrees) == 0 {
			fmt.Println("No matching worktrees found")
			return
		}

		// Collect info for each worktree
		var infos []worktreeInfo
		for _, wt := range worktrees {
			infos = append(infos, collectWorktreeInfo(wt, repoName))
		}

		m := harvestModel{
			worktrees: infos,
			repoName:  repoName,
		}

		p := tea.NewProgram(m, tea.WithAltScreen())
		finalModel, err := p.Run()
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}

		if fm, ok := finalModel.(harvestModel); ok && fm.jumpTo != "" {
			tmuxpkg.AttachOrSwitch(fm.jumpTo)
		}
	},
}

type harvestMode int

const (
	harvestBrowse harvestMode = iota
	harvestConfirmDiscard
	harvestContinuePrompt
	harvestStatusMessage
)

type harvestModel struct {
	worktrees    []worktreeInfo
	cursor       int
	jumpTo       string
	repoName     string
	scrollOffset int
	mode         harvestMode
	statusMsg    string
	textInput    textinput.Model
	width        int
	height       int
}

func (m harvestModel) Init() tea.Cmd {
	return nil
}

func (m harvestModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		return m, nil

	case tea.KeyMsg:
		// Handle modal states first
		switch m.mode {
		case harvestConfirmDiscard:
			switch msg.String() {
			case "y":
				m.discardWorktree()
				m.mode = harvestBrowse
			default:
				m.mode = harvestBrowse
			}
			return m, nil

		case harvestContinuePrompt:
			switch msg.Type {
			case tea.KeyEnter:
				prompt := strings.TrimSpace(m.textInput.Value())
				if prompt != "" && m.cursor < len(m.worktrees) {
					wt := m.worktrees[m.cursor]
					target := fmt.Sprintf("%s:0.1", tmuxpkg.SanitizeSessionName(wt.sessionName))
					tmuxpkg.SendKeys(target, prompt)
					m.statusMsg = "Prompt sent to agent"
				}
				m.mode = harvestStatusMessage
				return m, nil
			case tea.KeyEsc:
				m.mode = harvestBrowse
				return m, nil
			}
			var cmd tea.Cmd
			m.textInput, cmd = m.textInput.Update(msg)
			return m, cmd

		case harvestStatusMessage:
			m.mode = harvestBrowse
			m.statusMsg = ""
			return m, nil
		}

		// Browse mode
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
			if len(m.worktrees) > 0 {
				m.jumpTo = tmuxpkg.SanitizeSessionName(m.worktrees[m.cursor].sessionName)
			}
			return m, tea.Quit
		default:
			switch msg.String() {
			case "q":
				return m, tea.Quit
			case "j":
				m.moveCursor(1)
			case "k":
				m.moveCursor(-1)
			case "o":
				if m.cursor < len(m.worktrees) {
					m.jumpTo = tmuxpkg.SanitizeSessionName(m.worktrees[m.cursor].sessionName)
					return m, tea.Quit
				}
			case "p":
				m.createPR()
			case "m":
				m.mergeBranch()
			case "x":
				m.mode = harvestConfirmDiscard
			case "c":
				ti := textinput.New()
				ti.Placeholder = "Type follow-up prompt for the agent..."
				ti.Focus()
				ti.Width = m.width - 10
				m.textInput = ti
				m.mode = harvestContinuePrompt
			case "f":
				m.fixCI()
			case "r":
				m.addressReviewComments()
			}
		}
	}

	return m, nil
}

func (m *harvestModel) moveCursor(delta int) {
	if len(m.worktrees) == 0 {
		return
	}
	m.cursor += delta
	if m.cursor < 0 {
		m.cursor = len(m.worktrees) - 1
	} else if m.cursor >= len(m.worktrees) {
		m.cursor = 0
	}
	m.scrollOffset = 0
}

func (m *harvestModel) createPR() {
	if m.cursor >= len(m.worktrees) {
		return
	}
	wt := m.worktrees[m.cursor]

	// Push branch
	pushCmd := exec.Command("git", "-C", wt.worktreePath, "push", "-u", "origin", wt.branch)
	if err := pushCmd.Run(); err != nil {
		m.statusMsg = fmt.Sprintf("Push failed: %v", err)
		m.mode = harvestStatusMessage
		return
	}

	// Create PR
	prCmd := exec.Command("gh", "pr", "create",
		"--head", wt.branch,
		"--title", wt.branch,
		"--body", fmt.Sprintf("Auto-created from `tsp harvest`\n\nBranch: %s", wt.branch),
	)
	prCmd.Dir = wt.worktreePath
	out, err := prCmd.Output()
	if err != nil {
		m.statusMsg = fmt.Sprintf("PR creation failed: %v", err)
	} else {
		url := strings.TrimSpace(string(out))
		m.worktrees[m.cursor].prURL = url
		m.statusMsg = fmt.Sprintf("PR created: %s", url)
	}
	m.mode = harvestStatusMessage
}

func (m *harvestModel) mergeBranch() {
	if m.cursor >= len(m.worktrees) {
		return
	}
	wt := m.worktrees[m.cursor]

	repoRoot, err := getRepoRoot()
	if err != nil {
		m.statusMsg = fmt.Sprintf("Cannot find repo root: %v", err)
		m.mode = harvestStatusMessage
		return
	}

	// Merge
	mergeCmd := exec.Command("git", "-C", repoRoot, "merge", wt.branch)
	if err := mergeCmd.Run(); err != nil {
		m.statusMsg = fmt.Sprintf("Merge failed: %v", err)
		m.mode = harvestStatusMessage
		return
	}

	// Cleanup: kill session, remove worktree, delete branch
	sessName := tmuxpkg.SanitizeSessionName(wt.sessionName)
	tmuxpkg.KillSession(sessName)
	exec.Command("git", "worktree", "remove", wt.worktreePath, "--force").Run()
	exec.Command("git", "branch", "-D", wt.branch).Run()

	m.worktrees = append(m.worktrees[:m.cursor], m.worktrees[m.cursor+1:]...)
	if m.cursor >= len(m.worktrees) && m.cursor > 0 {
		m.cursor--
	}
	m.statusMsg = fmt.Sprintf("Merged and cleaned up %s", wt.branch)
	m.mode = harvestStatusMessage
}

func (m *harvestModel) discardWorktree() {
	if m.cursor >= len(m.worktrees) {
		return
	}
	wt := m.worktrees[m.cursor]

	sessName := tmuxpkg.SanitizeSessionName(wt.sessionName)
	tmuxpkg.KillSession(sessName)
	os.RemoveAll(wt.worktreePath)
	exec.Command("git", "worktree", "remove", wt.worktreePath, "--force").Run()
	exec.Command("git", "branch", "-D", wt.branch).Run()

	m.worktrees = append(m.worktrees[:m.cursor], m.worktrees[m.cursor+1:]...)
	if m.cursor >= len(m.worktrees) && m.cursor > 0 {
		m.cursor--
	}
	m.statusMsg = fmt.Sprintf("Discarded %s", wt.branch)
	m.mode = harvestStatusMessage
}

func (m *harvestModel) fixCI() {
	if m.cursor >= len(m.worktrees) {
		return
	}
	wt := m.worktrees[m.cursor]

	if wt.prNumber == 0 {
		m.statusMsg = "No PR found — create one first with [p]"
		m.mode = harvestStatusMessage
		return
	}

	logs, err := fetchFailingCILogs(wt.prNumber)
	if err != nil {
		m.statusMsg = fmt.Sprintf("No failing CI: %v", err)
		m.mode = harvestStatusMessage
		return
	}

	prompt := fmt.Sprintf("The CI pipeline failed. Here are the failing logs:\n\n%s\n\nPlease fix the issues and push.", logs)
	// Truncate if too long for send-keys
	if len(prompt) > 4000 {
		prompt = prompt[:4000] + "\n\n[truncated — check CI logs directly]"
	}

	target := fmt.Sprintf("%s:0.1", tmuxpkg.SanitizeSessionName(wt.sessionName))
	tmuxpkg.SendKeys(target, prompt)
	m.statusMsg = "CI failure logs sent to agent"
	m.mode = harvestStatusMessage
}

func (m *harvestModel) addressReviewComments() {
	if m.cursor >= len(m.worktrees) {
		return
	}
	wt := m.worktrees[m.cursor]

	if wt.prNumber == 0 {
		m.statusMsg = "No PR found — create one first with [p]"
		m.mode = harvestStatusMessage
		return
	}

	comments, err := fetchPRComments(wt.prNumber)
	if err != nil {
		m.statusMsg = fmt.Sprintf("Failed to fetch comments: %v", err)
		m.mode = harvestStatusMessage
		return
	}

	if len(comments) == 0 {
		m.statusMsg = "No review comments found"
		m.mode = harvestStatusMessage
		return
	}

	formatted := formatPRComments(comments)
	prompt := fmt.Sprintf("Please address these PR review comments:\n\n%s", formatted)

	target := fmt.Sprintf("%s:0.1", tmuxpkg.SanitizeSessionName(wt.sessionName))
	tmuxpkg.SendKeys(target, prompt)
	m.statusMsg = fmt.Sprintf("Review comments sent to agent (%d comments)", len(comments))
	m.mode = harvestStatusMessage
}

func (m harvestModel) View() string {
	if m.width == 0 || m.height == 0 {
		return "Loading..."
	}

	leftWidth := m.width * 35 / 100
	rightWidth := m.width - leftWidth - 3

	statusColor := func(status string) lipgloss.Color {
		switch status {
		case "ready":
			return lipgloss.Color("82")
		case "wip":
			return lipgloss.Color("226")
		case "clean":
			return lipgloss.Color("245")
		default:
			return lipgloss.Color("255")
		}
	}

	ciIcon := func(ci string) string {
		switch ci {
		case "pass":
			return " CI✓"
		case "fail":
			return " CI✗"
		case "pending":
			return " CI◌"
		default:
			return ""
		}
	}

	// Left panel: worktree list
	var lines []string
	for i, wt := range m.worktrees {
		stat := fmt.Sprintf("+%d/-%d", wt.insertions, wt.deletions)
		prInfo := ""
		if wt.prNumber > 0 {
			prInfo = fmt.Sprintf(" PR#%d%s", wt.prNumber, ciIcon(wt.ciStatus))
			if wt.reviewCount > 0 {
				prInfo += fmt.Sprintf(" %d💬", wt.reviewCount)
			}
		}

		line := fmt.Sprintf(" %-18s %8s %s%s", harvestTruncate(wt.branch, 18), stat, wt.status, prInfo)

		if i == m.cursor {
			style := lipgloss.NewStyle().
				Bold(true).
				Foreground(lipgloss.Color("212")).
				Background(lipgloss.Color("236"))
			lines = append(lines, style.Render(fmt.Sprintf("▸%s", line)))
		} else {
			style := lipgloss.NewStyle().Foreground(statusColor(wt.status))
			lines = append(lines, style.Render(fmt.Sprintf(" %s", line)))
		}
	}

	leftPanel := lipgloss.NewStyle().
		Width(leftWidth).
		Height(m.height - 4).
		BorderStyle(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("62")).
		Padding(1).
		Render(strings.Join(lines, "\n"))

	// Right panel: diff
	var diffContent string
	if m.cursor < len(m.worktrees) {
		diffContent = m.worktrees[m.cursor].diffOutput
	}
	if diffContent == "" {
		diffContent = "(no changes)"
	}
	diffLines := strings.Split(diffContent, "\n")
	maxLines := m.height - 6
	if maxLines > 0 && len(diffLines) > maxLines {
		diffLines = diffLines[:maxLines]
	}
	diffContent = strings.Join(diffLines, "\n")

	rightPanel := lipgloss.NewStyle().
		Width(rightWidth).
		Height(m.height - 4).
		BorderStyle(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("62")).
		Padding(0, 1).
		Render(diffContent)

	title := lipgloss.NewStyle().
		Bold(true).
		Foreground(lipgloss.Color("212")).
		Render("  Harvest — j/k: navigate | p: PR | m: merge | x: discard | c: continue | f: fix CI | r: reviews | q: quit")

	layout := lipgloss.JoinHorizontal(lipgloss.Top, leftPanel, rightPanel)
	result := fmt.Sprintf("%s\n%s", title, layout)

	// Modal overlays
	switch m.mode {
	case harvestConfirmDiscard:
		if m.cursor < len(m.worktrees) {
			result += "\n" + lipgloss.NewStyle().
				Foreground(lipgloss.Color("196")).Bold(true).
				Render(fmt.Sprintf("  Discard all changes in '%s'? (y/n)", m.worktrees[m.cursor].branch))
		}
	case harvestContinuePrompt:
		result += "\n  " + m.textInput.View()
	case harvestStatusMessage:
		result += "\n" + lipgloss.NewStyle().
			Foreground(lipgloss.Color("39")).Bold(true).
			Render(fmt.Sprintf("  %s (press any key)", m.statusMsg))
	}

	return result
}

// harvestTruncate truncates a string to max length with ellipsis.
func harvestTruncate(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max-1] + "…"
}
