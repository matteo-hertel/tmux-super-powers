package cmd

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/matteo-hertel/tmux-super-powers/config"
	tmuxpkg "github.com/matteo-hertel/tmux-super-powers/internal/tmux"
	"github.com/spf13/cobra"
)

var dashCmd = &cobra.Command{
	Use:   "dash",
	Short: "Mission control — monitor, review, and manage all sessions",
	Long: `Unified dashboard for all your tmux sessions.

Live preview with activity detection, diff viewer, PR/CI actions, and cleanup.
Press ? inside the dashboard for the full key binding reference.`,
	Run: func(cmd *cobra.Command, args []string) {
		if !tmuxpkg.IsInsideTmux() {
			fmt.Fprintf(os.Stderr, "Error: dash must be run inside a tmux session\n")
			os.Exit(1)
		}

		sessions, err := getTmuxSessions()
		if err != nil || len(sessions) == 0 {
			fmt.Println("No tmux sessions found")
			return
		}

		cfg, _ := config.Load()

		// Build worktree lookup
		var wtMap map[string]Worktree
		if isGitRepo() {
			worktrees, _ := getWorktrees()
			wtMap = make(map[string]Worktree)
			repoName := getRepoName()
			for _, wt := range worktrees {
				sessName := tmuxpkg.SanitizeSessionName(fmt.Sprintf("%s-%s", repoName, wt.Branch))
				wtMap[sessName] = wt
			}
		}

		m := dashModel{
			sessions:      make([]dashSession, len(sessions)),
			cfg:           cfg,
			lastRefreshed: time.Now(),
		}
		for i, s := range sessions {
			content := capturePaneContent(s, 0)
			ds := dashSession{
				name:        s,
				status:      "active",
				lastChanged: time.Now(),
				prevContent: "",
				paneContent: content,
			}
			// Check worktree first
			if wt, ok := wtMap[s]; ok {
				ds.isWorktree = true
				ds.branch = wt.Branch
				ds.worktreePath = wt.Path
				ds.isGitRepo = true
				ds.gitPath = wt.Path
			} else {
				// For non-worktree sessions, detect git info from pane cwd
				gitPath, branch := detectSessionGitInfo(s)
				if gitPath != "" {
					ds.isGitRepo = true
					ds.gitPath = gitPath
					ds.branch = branch
					// Check if this session is actually inside a git worktree
					cwd := tmuxpkg.GetPaneCwd(s)
					if cwd != "" {
						gitDirOut, err1 := exec.Command("git", "-C", cwd, "rev-parse", "--git-dir").Output()
						commonDirOut, err2 := exec.Command("git", "-C", cwd, "rev-parse", "--git-common-dir").Output()
						if err1 == nil && err2 == nil {
							gd := strings.TrimSpace(string(gitDirOut))
							cd := strings.TrimSpace(string(commonDirOut))
							if !filepath.IsAbs(gd) {
								gd = filepath.Join(cwd, gd)
							}
							if !filepath.IsAbs(cd) {
								cd = filepath.Join(cwd, cd)
							}
							if filepath.Clean(gd) != filepath.Clean(cd) {
								ds.isWorktree = true
								ds.worktreePath = gitPath
								ds.gitPath = filepath.Dir(filepath.Clean(cd))
							}
						}
					}
				}
			}
			m.sessions[i] = ds
		}

		p := tea.NewProgram(m, tea.WithAltScreen())
		finalModel, err := p.Run()
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}

		if fm, ok := finalModel.(dashModel); ok && fm.jumpTo != "" {
			tmuxpkg.AttachOrSwitch(fm.jumpTo)
		}
	},
}

// dashSession merges live monitoring data with worktree/diff/PR data.
type dashSession struct {
	// Identity
	name         string
	isWorktree   bool
	branch       string
	worktreePath string
	isGitRepo    bool   // true if session cwd is in any git repo
	gitPath      string // git repo root path (works for worktrees AND regular repos)

	// Live monitoring
	status      string // active, idle, done, error
	lastChanged time.Time
	prevContent string
	paneContent string

	// Diff data (loaded lazily on first 'd' press)
	filesChanged int
	insertions   int
	deletions    int
	diffOutput   string
	diffLoaded   bool

	// PR data (loaded lazily on first p/f/r press)
	prNumber    int
	prURL       string
	ciStatus    string
	reviewCount int
}

type dashView int

const (
	dashViewLive dashView = iota // live pane preview
	dashViewDiff                 // git diff
	dashViewHelp                 // ? help overlay
)

type dashMode int

const (
	dashBrowse dashMode = iota
	dashConfirmKill
	dashConfirmDiscard
	dashContinuePrompt
	dashStatusMessage
)

type dashModel struct {
	sessions      []dashSession
	cursor        int
	jumpTo        string
	previewPane   int
	width         int
	height        int
	cfg           *config.Config
	lastRefreshed time.Time
	view          dashView
	mode          dashMode
	statusMsg     string
	textInput     textinput.Model
}

type dashTickMsg time.Time

func dashTickCmd(refreshMs int) tea.Cmd {
	d := time.Duration(refreshMs) * time.Millisecond
	return tea.Tick(d, func(t time.Time) tea.Msg {
		return dashTickMsg(t)
	})
}

func (m dashModel) Init() tea.Cmd {
	return dashTickCmd(m.cfg.Dash.RefreshMs)
}

func (m dashModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		return m, nil

	case dashTickMsg:
		// Only refresh pane content in live view
		if m.view == dashViewLive {
			now := time.Now()
			for i := range m.sessions {
				s := &m.sessions[i]
				pane := 0
				if i == m.cursor {
					pane = m.previewPane
				}
				newContent := capturePaneContent(s.name, pane)
				s.prevContent = s.paneContent
				if newContent != s.paneContent {
					s.lastChanged = now
				}
				s.paneContent = newContent
				s.status = inferStatus(
					s.prevContent, s.paneContent, s.lastChanged, now,
					m.cfg.Dash.ErrorPatterns, m.cfg.Dash.PromptPattern,
				)
			}
			m.lastRefreshed = now
		}
		return m, dashTickCmd(m.cfg.Dash.RefreshMs)

	case tea.KeyMsg:
		// Help overlay dismisses on any key
		if m.view == dashViewHelp {
			m.view = dashViewLive
			return m, nil
		}

		// Handle modal states
		switch m.mode {
		case dashConfirmKill:
			if msg.String() == "y" {
				if m.cursor < len(m.sessions) {
					name := m.sessions[m.cursor].name
					tmuxpkg.KillSession(name)
					m.sessions = append(m.sessions[:m.cursor], m.sessions[m.cursor+1:]...)
					if m.cursor >= len(m.sessions) && m.cursor > 0 {
						m.cursor--
					}
				}
			}
			m.mode = dashBrowse
			return m, nil

		case dashConfirmDiscard:
			if msg.String() == "y" {
				m.discardWorktree()
			}
			m.mode = dashBrowse
			return m, nil

		case dashContinuePrompt:
			switch msg.Type {
			case tea.KeyEnter:
				prompt := strings.TrimSpace(m.textInput.Value())
				if prompt != "" && m.cursor < len(m.sessions) {
					target := fmt.Sprintf("%s:0.1", m.sessions[m.cursor].name)
					tmuxpkg.SendKeys(target, prompt)
					m.statusMsg = "Prompt sent to agent"
					m.mode = dashStatusMessage
				} else {
					m.mode = dashBrowse
				}
				return m, nil
			case tea.KeyEsc:
				m.mode = dashBrowse
				return m, nil
			}
			var cmd tea.Cmd
			m.textInput, cmd = m.textInput.Update(msg)
			return m, cmd

		case dashStatusMessage:
			m.mode = dashBrowse
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
			if len(m.sessions) > 0 {
				m.jumpTo = m.sessions[m.cursor].name
			}
			return m, tea.Quit
		case tea.KeyTab:
			if m.view == dashViewLive {
				m.previewPane++
			}
			return m, nil
		default:
			switch msg.String() {
			case "q":
				return m, tea.Quit
			case "j":
				m.moveCursor(1)
				return m, nil
			case "k":
				m.moveCursor(-1)
				return m, nil
			case "?":
				m.view = dashViewHelp
				return m, nil
			case "d":
				// Toggle between live and diff view
				if m.view == dashViewLive {
					m.loadDiffIfNeeded()
					m.view = dashViewDiff
				} else {
					m.view = dashViewLive
				}
				return m, nil
			case "x":
				if m.cursor < len(m.sessions) {
					if m.sessions[m.cursor].isWorktree {
						m.mode = dashConfirmDiscard
					} else {
						m.mode = dashConfirmKill
					}
				}
				return m, nil
			case "c":
				ti := textinput.New()
				ti.Placeholder = "Type follow-up prompt for the agent..."
				ti.Focus()
				ti.Width = m.width - 10
				m.textInput = ti
				m.mode = dashContinuePrompt
				return m, nil
			case "p":
				m.createPR()
				return m, nil
			case "f":
				m.fixCI()
				return m, nil
			case "r":
				m.addressReviewComments()
				return m, nil
			case "m":
				m.mergeBranch()
				return m, nil
			case "W":
				m.cleanupMerged()
				return m, nil
			}
		}
	}

	return m, nil
}

func (m *dashModel) moveCursor(delta int) {
	if len(m.sessions) == 0 {
		return
	}
	m.cursor += delta
	if m.cursor < 0 {
		m.cursor = len(m.sessions) - 1
	} else if m.cursor >= len(m.sessions) {
		m.cursor = 0
	}
	m.previewPane = 0
}

func (m *dashModel) loadDiffIfNeeded() {
	if m.cursor >= len(m.sessions) {
		return
	}
	s := &m.sessions[m.cursor]
	if s.diffLoaded || !s.isGitRepo {
		return
	}
	statCmd := exec.Command("git", "-C", s.gitPath, "diff", "--stat")
	if out, err := statCmd.Output(); err == nil {
		s.filesChanged, s.insertions, s.deletions = parseDiffStat(string(out))
	}
	diffCmd := exec.Command("git", "-C", s.gitPath, "diff")
	if out, err := diffCmd.Output(); err == nil {
		s.diffOutput = string(out)
	}
	s.diffLoaded = true
}

func (m *dashModel) createPR() {
	if m.cursor >= len(m.sessions) {
		return
	}
	s := &m.sessions[m.cursor]
	if !s.isGitRepo {
		m.statusMsg = "Not a git repo"
		m.mode = dashStatusMessage
		return
	}
	pushCmd := exec.Command("git", "-C", s.gitPath, "push", "-u", "origin", s.branch)
	if err := pushCmd.Run(); err != nil {
		m.statusMsg = fmt.Sprintf("Push failed: %v", err)
		m.mode = dashStatusMessage
		return
	}
	prCmd := exec.Command("gh", "pr", "create",
		"--head", s.branch,
		"--title", s.branch,
		"--body", fmt.Sprintf("Auto-created from `tsp dash`\n\nBranch: %s", s.branch),
	)
	prCmd.Dir = s.gitPath
	out, err := prCmd.Output()
	if err != nil {
		m.statusMsg = fmt.Sprintf("PR creation failed: %v", err)
	} else {
		url := strings.TrimSpace(string(out))
		s.prURL = url
		m.statusMsg = fmt.Sprintf("PR created: %s", url)
	}
	m.mode = dashStatusMessage
}

func (m *dashModel) mergeBranch() {
	if m.cursor >= len(m.sessions) {
		return
	}
	s := m.sessions[m.cursor]
	if !s.isGitRepo {
		m.statusMsg = "Not a git repo"
		m.mode = dashStatusMessage
		return
	}
	if s.isWorktree {
		repoRoot, err := getRepoRoot()
		if err != nil {
			m.statusMsg = fmt.Sprintf("Cannot find repo root: %v", err)
			m.mode = dashStatusMessage
			return
		}
		mergeCmd := exec.Command("git", "-C", repoRoot, "merge", s.branch)
		if err := mergeCmd.Run(); err != nil {
			m.statusMsg = fmt.Sprintf("Merge failed: %v", err)
			m.mode = dashStatusMessage
			return
		}
		tmuxpkg.KillSession(s.name)
		if err := exec.Command("git", "-C", s.gitPath, "worktree", "remove", s.worktreePath, "--force").Run(); err != nil {
			exec.Command("git", "-C", s.gitPath, "worktree", "prune").Run()
		}
		exec.Command("git", "-C", s.gitPath, "branch", "-D", s.branch).Run()
		m.removeSession(m.cursor)
		m.statusMsg = fmt.Sprintf("Merged and cleaned up %s", s.branch)
	} else {
		// Regular git repo — merge current branch into main/master
		base := "main"
		checkCmd := exec.Command("git", "-C", s.gitPath, "rev-parse", "--verify", "main")
		if checkCmd.Run() != nil {
			base = "master"
		}
		checkoutCmd := exec.Command("git", "-C", s.gitPath, "checkout", base)
		if err := checkoutCmd.Run(); err != nil {
			m.statusMsg = fmt.Sprintf("Checkout %s failed: %v", base, err)
			m.mode = dashStatusMessage
			return
		}
		mergeCmd := exec.Command("git", "-C", s.gitPath, "merge", s.branch)
		if err := mergeCmd.Run(); err != nil {
			m.statusMsg = fmt.Sprintf("Merge failed: %v", err)
			m.mode = dashStatusMessage
			return
		}
		exec.Command("git", "-C", s.gitPath, "branch", "-D", s.branch).Run()
		m.statusMsg = fmt.Sprintf("Merged %s into %s", s.branch, base)
	}
	m.mode = dashStatusMessage
}

func (m *dashModel) discardWorktree() {
	if m.cursor >= len(m.sessions) {
		return
	}
	s := m.sessions[m.cursor]
	tmuxpkg.KillSession(s.name)
	if s.isWorktree && s.worktreePath != "" {
		if err := exec.Command("git", "-C", s.gitPath, "worktree", "remove", s.worktreePath, "--force").Run(); err != nil {
			// If git worktree remove fails (e.g. dir already gone), fall back to prune
			exec.Command("git", "-C", s.gitPath, "worktree", "prune").Run()
		}
		if err := exec.Command("git", "-C", s.gitPath, "branch", "-D", s.branch).Run(); err != nil {
			m.statusMsg = fmt.Sprintf("Removed %s (branch delete failed: %v)", s.name, err)
			m.mode = dashStatusMessage
			m.removeSession(m.cursor)
			return
		}
	}
	m.removeSession(m.cursor)
	m.statusMsg = fmt.Sprintf("Removed %s", s.name)
	m.mode = dashStatusMessage
}

func (m *dashModel) cleanupMerged() {
	if m.cursor >= len(m.sessions) {
		return
	}
	s := m.sessions[m.cursor]
	if !s.isGitRepo {
		m.statusMsg = "Not a git repo"
		m.mode = dashStatusMessage
		return
	}
	// Check if branch is merged into main/master
	merged := false
	for _, base := range []string{"main", "master"} {
		cmd := exec.Command("git", "-C", s.gitPath, "branch", "--merged", base)
		if out, err := cmd.Output(); err == nil {
			for _, line := range strings.Split(string(out), "\n") {
				if strings.TrimSpace(line) == s.branch {
					merged = true
					break
				}
			}
		}
		if merged {
			break
		}
	}
	if !merged {
		m.statusMsg = fmt.Sprintf("Branch '%s' is not merged yet", s.branch)
		m.mode = dashStatusMessage
		return
	}
	if s.isWorktree {
		tmuxpkg.KillSession(s.name)
		os.RemoveAll(s.worktreePath)
		exec.Command("git", "worktree", "remove", s.worktreePath, "--force").Run()
		exec.Command("git", "branch", "-D", s.branch).Run()
		m.removeSession(m.cursor)
		m.statusMsg = fmt.Sprintf("Cleaned up merged worktree %s", s.branch)
	} else {
		exec.Command("git", "-C", s.gitPath, "branch", "-D", s.branch).Run()
		m.statusMsg = fmt.Sprintf("Deleted merged branch %s", s.branch)
	}
	m.mode = dashStatusMessage
}

func (m *dashModel) fixCI() {
	if m.cursor >= len(m.sessions) {
		return
	}
	s := &m.sessions[m.cursor]
	if !s.isGitRepo {
		m.statusMsg = "Not a git repo"
		m.mode = dashStatusMessage
		return
	}
	enrichWithPRData2(s)
	if s.prNumber == 0 {
		m.statusMsg = "No PR found — create one first with [p]"
		m.mode = dashStatusMessage
		return
	}
	logs, err := fetchFailingCILogs(s.prNumber)
	if err != nil {
		m.statusMsg = fmt.Sprintf("No failing CI: %v", err)
		m.mode = dashStatusMessage
		return
	}
	prompt := fmt.Sprintf("The CI pipeline failed. Here are the failing logs:\n\n%s\n\nPlease fix the issues and push.", logs)
	if len(prompt) > 4000 {
		prompt = prompt[:4000] + "\n\n[truncated]"
	}
	target := fmt.Sprintf("%s:0.1", s.name)
	tmuxpkg.SendKeys(target, prompt)
	m.statusMsg = "CI failure logs sent to agent"
	m.mode = dashStatusMessage
}

func (m *dashModel) addressReviewComments() {
	if m.cursor >= len(m.sessions) {
		return
	}
	s := &m.sessions[m.cursor]
	if !s.isGitRepo {
		m.statusMsg = "Not a git repo"
		m.mode = dashStatusMessage
		return
	}
	enrichWithPRData2(s)
	if s.prNumber == 0 {
		m.statusMsg = "No PR found — create one first with [p]"
		m.mode = dashStatusMessage
		return
	}
	comments, err := fetchPRComments(s.prNumber)
	if err != nil {
		m.statusMsg = fmt.Sprintf("Failed to fetch comments: %v", err)
		m.mode = dashStatusMessage
		return
	}
	if len(comments) == 0 {
		m.statusMsg = "No review comments found"
		m.mode = dashStatusMessage
		return
	}
	formatted := formatPRComments(comments)
	prompt := fmt.Sprintf("Please address these PR review comments:\n\n%s", formatted)
	target := fmt.Sprintf("%s:0.1", s.name)
	tmuxpkg.SendKeys(target, prompt)
	m.statusMsg = fmt.Sprintf("Review comments sent to agent (%d comments)", len(comments))
	m.mode = dashStatusMessage
}

func (m *dashModel) removeSession(idx int) {
	m.sessions = append(m.sessions[:idx], m.sessions[idx+1:]...)
	if m.cursor >= len(m.sessions) && m.cursor > 0 {
		m.cursor--
	}
}

// enrichWithPRData2 fetches PR/CI info for a dashSession lazily.
func enrichWithPRData2(s *dashSession) {
	if s.prNumber > 0 {
		return
	}
	s.prNumber, s.prURL = findPRForBranch(s.branch)
	if s.prNumber > 0 {
		s.ciStatus = getCIStatus(s.prNumber)
		s.reviewCount = getReviewCommentCount(s.prNumber)
	}
}

// ── View ────────────────────────────────────────────────────────

func (m dashModel) View() string {
	if m.width == 0 || m.height == 0 {
		return "Loading..."
	}

	// Help overlay
	if m.view == dashViewHelp {
		return m.viewHelp()
	}

	leftWidth := m.width * 40 / 100
	rightWidth := m.width - leftWidth - 3

	// Title bar
	title := lipgloss.NewStyle().
		Bold(true).
		Foreground(lipgloss.Color("212")).
		Render("  tsp dash — Mission Control")

	// Left panel: session list
	now := time.Now()
	var sessionLines []string
	for i, s := range m.sessions {
		icon := statusIcon(s.status)
		timeSince := formatTimeSince(s.lastChanged, now)

		nameWidth := leftWidth - 16
		if nameWidth < 10 {
			nameWidth = 10
		}
		label := truncate(s.name, nameWidth)

		extra := timeSince
		if s.isGitRepo && s.diffLoaded && s.filesChanged > 0 {
			extra = fmt.Sprintf("+%d/-%d %s", s.insertions, s.deletions, timeSince)
		}
		if s.prNumber > 0 {
			ciIcon := "…"
			switch s.ciStatus {
			case "pass":
				ciIcon = "✓"
			case "fail":
				ciIcon = "✗"
			}
			extra = fmt.Sprintf("#%d %s %s", s.prNumber, ciIcon, timeSince)
		}

		line := fmt.Sprintf(" %s %-*s %s", icon, nameWidth, label, extra)

		if i == m.cursor {
			style := lipgloss.NewStyle().
				Bold(true).
				Foreground(lipgloss.Color("212")).
				Background(lipgloss.Color("236"))
			sessionLines = append(sessionLines, style.Render(fmt.Sprintf("▸%s", line)))
		} else {
			sessionLines = append(sessionLines, lipgloss.NewStyle().
				Foreground(dashStatusColor(s.status)).
				Render(fmt.Sprintf(" %s", line)))
		}
	}

	panelHeight := m.height - 6 // room for title + legend + modal
	leftPanel := lipgloss.NewStyle().
		Width(leftWidth).
		Height(panelHeight).
		BorderStyle(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("62")).
		Padding(0, 1).
		Render(strings.Join(sessionLines, "\n"))

	// Right panel: session detail + actions
	rightContent := m.viewDetailPanel(rightWidth, panelHeight)
	rightPanel := lipgloss.NewStyle().
		Width(rightWidth).
		Height(panelHeight).
		BorderStyle(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("62")).
		Padding(0, 1).
		Render(rightContent)

	layout := lipgloss.JoinHorizontal(lipgloss.Top, leftPanel, rightPanel)

	// Legend bar
	dim := lipgloss.NewStyle().Foreground(lipgloss.Color("245"))
	key := lipgloss.NewStyle().Foreground(lipgloss.Color("212")).Bold(true)
	sep := dim.Render(" │ ")

	legend := "  " +
		lipgloss.NewStyle().Foreground(lipgloss.Color("82")).Render("●") + " active " +
		lipgloss.NewStyle().Foreground(lipgloss.Color("245")).Render("◌") + " idle " +
		lipgloss.NewStyle().Foreground(lipgloss.Color("226")).Render("✓") + " done " +
		lipgloss.NewStyle().Foreground(lipgloss.Color("196")).Render("✗") + " error " +
		lipgloss.NewStyle().Foreground(lipgloss.Color("39")).Render("?") + " waiting" +
		sep +
		key.Render("↑↓") + " nav " +
		key.Render("⏎") + " attach " +
		key.Render("d") + " diff " +
		key.Render("x") + " kill " +
		key.Render("p") + " pr " +
		key.Render("f") + " fix-ci " +
		key.Render("r") + " reviews " +
		key.Render("m") + " merge " +
		key.Render("?") + " help"

	result := fmt.Sprintf("%s\n%s\n%s", title, layout, legend)

	// Modal overlays
	switch m.mode {
	case dashConfirmKill:
		if m.cursor < len(m.sessions) {
			result += "\n" + lipgloss.NewStyle().
				Foreground(lipgloss.Color("196")).Bold(true).
				Render(fmt.Sprintf("  Kill session '%s'? (y/n)", m.sessions[m.cursor].name))
		}
	case dashConfirmDiscard:
		if m.cursor < len(m.sessions) {
			result += "\n" + lipgloss.NewStyle().
				Foreground(lipgloss.Color("196")).Bold(true).
				Render(fmt.Sprintf("  Discard worktree '%s'? This deletes the branch and directory. (y/n)", m.sessions[m.cursor].name))
		}
	case dashContinuePrompt:
		result += "\n  " + m.textInput.View()
	case dashStatusMessage:
		result += "\n" + lipgloss.NewStyle().
			Foreground(lipgloss.Color("39")).Bold(true).
			Render(fmt.Sprintf("  %s (press any key)", m.statusMsg))
	}

	return result
}

func (m dashModel) viewDetailPanel(width, height int) string {
	if len(m.sessions) == 0 || m.cursor >= len(m.sessions) {
		return "No sessions"
	}
	s := m.sessions[m.cursor]

	bold := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("212"))
	dim := lipgloss.NewStyle().Foreground(lipgloss.Color("245"))
	val := lipgloss.NewStyle().Foreground(lipgloss.Color("255"))

	var lines []string

	// Session header
	lines = append(lines, bold.Render(s.name))
	lines = append(lines, "")

	// Status
	statusColor := dashStatusColor(s.status)
	lines = append(lines, fmt.Sprintf("  %s %s  %s",
		dim.Render("Status:"),
		lipgloss.NewStyle().Foreground(statusColor).Bold(true).Render(statusIcon(s.status)+" "+s.status),
		dim.Render(formatTimeSince(s.lastChanged, time.Now())),
	))

	// Git info
	if s.isGitRepo {
		lines = append(lines, fmt.Sprintf("  %s %s", dim.Render("Branch:"), val.Render(s.branch)))
		if s.isWorktree {
			lines = append(lines, fmt.Sprintf("  %s %s", dim.Render("  Type:"), val.Render("worktree")))
		}
		if s.diffLoaded && s.filesChanged > 0 {
			lines = append(lines, fmt.Sprintf("  %s %s",
				dim.Render("  Diff:"),
				val.Render(fmt.Sprintf("%d files, +%d/-%d", s.filesChanged, s.insertions, s.deletions)),
			))
		}
	} else {
		lines = append(lines, fmt.Sprintf("  %s %s", dim.Render("   Git:"), dim.Render("not a git repo")))
	}

	// PR info
	if s.prNumber > 0 {
		lines = append(lines, "")
		lines = append(lines, fmt.Sprintf("  %s #%d", dim.Render("    PR:"), s.prNumber))
		ciLabel := s.ciStatus
		ciColor := lipgloss.Color("245")
		switch s.ciStatus {
		case "pass":
			ciLabel = "✓ passing"
			ciColor = lipgloss.Color("82")
		case "fail":
			ciLabel = "✗ failing"
			ciColor = lipgloss.Color("196")
		case "pending":
			ciLabel = "… pending"
			ciColor = lipgloss.Color("226")
		}
		lines = append(lines, fmt.Sprintf("  %s %s",
			dim.Render("    CI:"),
			lipgloss.NewStyle().Foreground(ciColor).Render(ciLabel),
		))
		if s.reviewCount > 0 {
			lines = append(lines, fmt.Sprintf("  %s %s",
				dim.Render("Reviews:"),
				val.Render(fmt.Sprintf("%d comments", s.reviewCount)),
			))
		}
	}

	// Loading indicator
	if m.statusMsg != "" && m.mode == dashStatusMessage {
		lines = append(lines, "")
		lines = append(lines, lipgloss.NewStyle().Foreground(lipgloss.Color("39")).Render(fmt.Sprintf("  ⟳ %s", m.statusMsg)))
	}

	// Actions section
	lines = append(lines, "")
	lines = append(lines, bold.Render("Actions"))

	actionDim := lipgloss.NewStyle().Foreground(lipgloss.Color("245"))
	actionKey := lipgloss.NewStyle().Foreground(lipgloss.Color("212")).Bold(true)

	lines = append(lines, fmt.Sprintf("  %s attach   %s send prompt", actionKey.Render("⏎"), actionKey.Render("c")))
	lines = append(lines, fmt.Sprintf("  %s kill     %s toggle diff", actionKey.Render("x"), actionKey.Render("d")))
	if s.isGitRepo {
		lines = append(lines, fmt.Sprintf("  %s create PR", actionKey.Render("p")))
		if s.prNumber > 0 {
			lines = append(lines, fmt.Sprintf("  %s fix CI   %s address reviews", actionKey.Render("f"), actionKey.Render("r")))
			lines = append(lines, fmt.Sprintf("  %s merge    %s cleanup merged", actionKey.Render("m"), actionKey.Render("W")))
		} else {
			lines = append(lines, actionDim.Render("  (create PR to unlock CI/review/merge actions)"))
		}
	}

	// If diff view is active, show diff content below actions
	if m.view == dashViewDiff {
		lines = append(lines, "")
		lines = append(lines, bold.Render("Diff"))
		if !s.isGitRepo {
			lines = append(lines, dim.Render("  (not a git repo)"))
		} else if !s.diffLoaded {
			lines = append(lines, dim.Render("  (loading...)"))
		} else if s.diffOutput == "" {
			lines = append(lines, dim.Render("  (no changes)"))
		} else {
			diffLines := strings.Split(s.diffOutput, "\n")
			maxDiff := height - len(lines) - 2
			if maxDiff > 0 && len(diffLines) > maxDiff {
				diffLines = diffLines[:maxDiff]
			}
			for _, dl := range diffLines {
				if strings.HasPrefix(dl, "+") {
					lines = append(lines, lipgloss.NewStyle().Foreground(lipgloss.Color("82")).Render("  "+dl))
				} else if strings.HasPrefix(dl, "-") {
					lines = append(lines, lipgloss.NewStyle().Foreground(lipgloss.Color("196")).Render("  "+dl))
				} else {
					lines = append(lines, dim.Render("  "+dl))
				}
			}
		}
	} else {
		// Live pane preview (compact, last N lines)
		lines = append(lines, "")
		lines = append(lines, bold.Render("Live"))
		content := s.paneContent
		if content == "" {
			lines = append(lines, dim.Render("  (no content)"))
		} else {
			paneLines := strings.Split(content, "\n")
			maxPane := height - len(lines) - 2
			if maxPane > 0 && len(paneLines) > maxPane {
				paneLines = paneLines[len(paneLines)-maxPane:]
			}
			lines = append(lines, paneLines...)
		}
	}

	return strings.Join(lines, "\n")
}

func (m dashModel) viewHelp() string {
	help := lipgloss.NewStyle().
		Width(m.width - 4).
		Padding(2, 4).
		BorderStyle(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("212")).
		Render(strings.Join([]string{
			lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("212")).Render("Dashboard — Key Bindings"),
			"",
			lipgloss.NewStyle().Bold(true).Render("Navigation"),
			"  j/k or arrows   Navigate sessions",
			"  tab              Cycle panes (live view)",
			"  enter            Jump to selected session",
			"  d                Toggle live preview / diff view",
			"  q / esc          Quit",
			"",
			lipgloss.NewStyle().Bold(true).Render("Actions (all sessions)"),
			"  x                Kill session (or discard worktree)",
			"  c                Send follow-up prompt to agent",
			"",
			lipgloss.NewStyle().Bold(true).Render("Actions (git repo sessions)"),
			"  p                Create PR (push + gh pr create)",
			"  m                Merge branch to base (worktree: full cleanup, regular: merge + delete)",
			"  f                Fix CI — fetch failing logs, send to agent",
			"  r                Review — fetch PR comments, send to agent",
			"  W                Clean up merged branch (worktree: full cleanup, regular: delete branch)",
			"",
			lipgloss.NewStyle().Foreground(lipgloss.Color("245")).Render("Press any key to close this help"),
		}, "\n"))

	// Center vertically
	padding := (m.height - strings.Count(help, "\n") - 2) / 2
	if padding < 0 {
		padding = 0
	}
	return strings.Repeat("\n", padding) + help
}

func dashStatusColor(status string) lipgloss.Color {
	switch status {
	case "active":
		return lipgloss.Color("82") // green
	case "idle":
		return lipgloss.Color("245") // gray
	case "done":
		return lipgloss.Color("226") // yellow
	case "error":
		return lipgloss.Color("196") // red
	default:
		return lipgloss.Color("255")
	}
}

func truncate(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max-1] + "…"
}
