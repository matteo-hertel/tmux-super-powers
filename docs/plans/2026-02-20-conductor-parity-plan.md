# Conductor-Parity Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Add 3 new commands (dash, spawn, harvest) plus consolidate existing commands to achieve conductor.build parity.

**Architecture:** Each command follows the existing bubbletea pattern. Pure logic is extracted into testable functions. New tmux helpers go in `internal/tmux/`. Config extensions are additive (backward compatible). The `gh` CLI is used for GitHub operations (PR, CI, comments).

**Tech Stack:** Go 1.24, Cobra, Bubbletea/Bubbles/Lipgloss, tmux, gh CLI

---

## Phase 1: Foundation (Config & Shared Utilities)

### Task 1: Extend Config Struct

**Files:**
- Modify: `config/config.go`
- Modify: `config/config_test.go`

**Step 1: Write the failing test**

Add to `config/config_test.go`:

```go
func TestLoadDashConfig(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "config.yaml")
	content := []byte(`
directories:
  - ~/projects
dash:
  refresh_ms: 300
  error_patterns:
    - "FAIL"
    - "panic:"
  prompt_pattern: "\\$\\s*$"
spawn:
  worktree_base: ~/work/code
  agent_command: "claude --dangerously-skip-permissions"
  default_setup: "cp ../.env .env"
`)
	os.WriteFile(configPath, content, 0644)

	cfg, err := LoadFrom(configPath)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.Dash.RefreshMs != 300 {
		t.Errorf("expected refresh_ms 300, got %d", cfg.Dash.RefreshMs)
	}
	if len(cfg.Dash.ErrorPatterns) != 2 {
		t.Errorf("expected 2 error patterns, got %d", len(cfg.Dash.ErrorPatterns))
	}
	if cfg.Dash.PromptPattern != "\\$\\s*$" {
		t.Errorf("unexpected prompt pattern: %s", cfg.Dash.PromptPattern)
	}
	if cfg.Spawn.AgentCommand != "claude --dangerously-skip-permissions" {
		t.Errorf("unexpected agent command: %s", cfg.Spawn.AgentCommand)
	}
	if cfg.Spawn.DefaultSetup != "cp ../.env .env" {
		t.Errorf("unexpected default setup: %s", cfg.Spawn.DefaultSetup)
	}
}

func TestDashConfigDefaults(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "config.yaml")
	os.WriteFile(configPath, []byte("directories:\n  - ~/projects\n"), 0644)

	cfg, err := LoadFrom(configPath)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.Dash.RefreshMs != 500 {
		t.Errorf("expected default refresh_ms 500, got %d", cfg.Dash.RefreshMs)
	}
	if cfg.Spawn.AgentCommand != "claude --dangerously-skip-permissions" {
		t.Errorf("expected default agent command, got: %s", cfg.Spawn.AgentCommand)
	}
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./config/ -run TestLoadDashConfig -v`
Expected: FAIL — `Dash` and `Spawn` fields don't exist yet

**Step 3: Write minimal implementation**

Add to `config/config.go` — new structs:

```go
type DashConfig struct {
	RefreshMs     int      `yaml:"refresh_ms"`
	ErrorPatterns []string `yaml:"error_patterns"`
	PromptPattern string   `yaml:"prompt_pattern"`
}

type SpawnConfig struct {
	WorktreeBase string `yaml:"worktree_base"`
	AgentCommand string `yaml:"agent_command"`
	DefaultSetup string `yaml:"default_setup"`
}
```

Add fields to `Config`:

```go
type Config struct {
	Directories       []string    `yaml:"directories"`
	IgnoreDirectories []string    `yaml:"ignore_directories"`
	Sandbox           Sandbox     `yaml:"sandbox"`
	Projects          Projects    `yaml:"projects"`
	Editor            string      `yaml:"editor"`
	Dash              DashConfig  `yaml:"dash"`
	Spawn             SpawnConfig `yaml:"spawn"`
}
```

In `LoadFrom`, after unmarshaling, apply defaults for new fields:

```go
if cfg.Dash.RefreshMs == 0 {
	cfg.Dash.RefreshMs = 500
}
if cfg.Dash.PromptPattern == "" {
	cfg.Dash.PromptPattern = `\$\s*$`
}
if len(cfg.Dash.ErrorPatterns) == 0 {
	cfg.Dash.ErrorPatterns = []string{"FAIL", "panic:", "Error:"}
}
if cfg.Spawn.AgentCommand == "" {
	cfg.Spawn.AgentCommand = "claude --dangerously-skip-permissions"
}
if cfg.Spawn.WorktreeBase == "" {
	cfg.Spawn.WorktreeBase = filepath.Join(homeDir, "work", "code")
}
```

Also update `defaultConfig()` to include these defaults.

**Step 4: Run tests to verify they pass**

Run: `go test ./config/ -v`
Expected: ALL PASS

**Step 5: Commit**

```bash
git add config/config.go config/config_test.go
git commit -m "feat(config): add dash and spawn config sections"
```

---

### Task 2: Add tmux SendKeys Helper

**Files:**
- Modify: `internal/tmux/tmux.go`
- Modify: `internal/tmux/tmux_test.go`

**Step 1: Write the failing test**

Add to `internal/tmux/tmux_test.go`:

```go
func TestBuildSendKeysArgs(t *testing.T) {
	tests := []struct {
		name    string
		target  string
		text    string
		want    []string
	}{
		{
			name:   "simple text",
			target: "myapp-fix:0.1",
			text:   "fix the auth bug",
			want:   []string{"send-keys", "-t", "myapp-fix:0.1", "fix the auth bug", "Enter"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := BuildSendKeysArgs(tt.target, tt.text)
			if len(got) != len(tt.want) {
				t.Errorf("BuildSendKeysArgs() len = %d, want %d", len(got), len(tt.want))
				return
			}
			for i := range got {
				if got[i] != tt.want[i] {
					t.Errorf("BuildSendKeysArgs()[%d] = %q, want %q", i, got[i], tt.want[i])
				}
			}
		})
	}
}

func TestBuildListSessionsArgs(t *testing.T) {
	args := BuildListSessionsArgs()
	if args[0] != "list-sessions" {
		t.Errorf("expected list-sessions, got %s", args[0])
	}
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./internal/tmux/ -run TestBuildSendKeysArgs -v`
Expected: FAIL — function doesn't exist

**Step 3: Write minimal implementation**

Add to `internal/tmux/tmux.go`:

```go
// BuildSendKeysArgs builds tmux send-keys args for sending text to a pane.
func BuildSendKeysArgs(target, text string) []string {
	return []string{"send-keys", "-t", target, text, "Enter"}
}

// SendKeys sends text to a tmux pane target (e.g., "session:0.1").
func SendKeys(target, text string) error {
	args := BuildSendKeysArgs(target, text)
	return exec.Command("tmux", args...).Run()
}

// BuildListSessionsArgs builds tmux list-sessions args.
func BuildListSessionsArgs() []string {
	return []string{"list-sessions", "-F", "#{session_name}:#{session_path}:#{session_activity}"}
}

// BuildCapturePaneArgs builds tmux capture-pane args.
func BuildCapturePaneArgs(target string) []string {
	return []string{"capture-pane", "-t", target, "-p", "-e"}
}
```

**Step 4: Run tests to verify they pass**

Run: `go test ./internal/tmux/ -v`
Expected: ALL PASS

**Step 5: Commit**

```bash
git add internal/tmux/tmux.go internal/tmux/tmux_test.go
git commit -m "feat(tmux): add SendKeys and session listing helpers"
```

---

### Task 3: Task-to-Branch Name Generator

**Files:**
- Create: `internal/cmd/spawn_helpers.go`
- Create: `internal/cmd/spawn_helpers_test.go`

**Step 1: Write the failing test**

Create `internal/cmd/spawn_helpers_test.go`:

```go
package cmd

import "testing"

func TestTaskToBranch(t *testing.T) {
	tests := []struct {
		task string
		want string
	}{
		{"fix the auth token expiry bug", "spawn/fix-the-auth-token-expiry-bug"},
		{"Add Dark Mode Support!", "spawn/add-dark-mode-support"},
		{"refactor: database connection pooling layer", "spawn/refactor-database-connection-pooling-layer"},
		{"", "spawn/task"},
		{"a very long task description that exceeds the fifty character limit for branch names which should be truncated", "spawn/a-very-long-task-description-that-exceeds-the"},
	}
	for _, tt := range tests {
		t.Run(tt.task, func(t *testing.T) {
			got := taskToBranch(tt.task)
			if got != tt.want {
				t.Errorf("taskToBranch(%q) = %q, want %q", tt.task, got, tt.want)
			}
		})
	}
}

func TestParseTaskFile(t *testing.T) {
	input := `# My tasks
fix the authentication bug

add dark mode support
# this is a comment

refactor database layer
`
	tasks := parseTaskFile(input)
	if len(tasks) != 3 {
		t.Fatalf("expected 3 tasks, got %d", len(tasks))
	}
	if tasks[0] != "fix the authentication bug" {
		t.Errorf("task 0: got %q", tasks[0])
	}
	if tasks[1] != "add dark mode support" {
		t.Errorf("task 1: got %q", tasks[1])
	}
	if tasks[2] != "refactor database layer" {
		t.Errorf("task 2: got %q", tasks[2])
	}
}

func TestParseTaskFileEmpty(t *testing.T) {
	tasks := parseTaskFile("# only comments\n\n")
	if len(tasks) != 0 {
		t.Errorf("expected 0 tasks, got %d", len(tasks))
	}
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./internal/cmd/ -run TestTaskToBranch -v`
Expected: FAIL — function doesn't exist

**Step 3: Write minimal implementation**

Create `internal/cmd/spawn_helpers.go`:

```go
package cmd

import (
	"regexp"
	"strings"
)

var nonAlphaNumeric = regexp.MustCompile(`[^a-z0-9-]+`)
var multiHyphen = regexp.MustCompile(`-{2,}`)

// taskToBranch converts a task description to a git branch name.
// "fix the auth bug" → "spawn/fix-the-auth-bug"
func taskToBranch(task string) string {
	if task == "" {
		return "spawn/task"
	}
	name := strings.ToLower(task)
	name = nonAlphaNumeric.ReplaceAllString(name, "-")
	name = multiHyphen.ReplaceAllString(name, "-")
	name = strings.Trim(name, "-")
	if len(name) > 50 {
		name = name[:50]
		name = strings.TrimRight(name, "-")
	}
	return "spawn/" + name
}

// parseTaskFile parses a task file. One task per line.
// Blank lines and lines starting with # are ignored.
func parseTaskFile(content string) []string {
	var tasks []string
	for _, line := range strings.Split(content, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		tasks = append(tasks, line)
	}
	return tasks
}
```

**Step 4: Run tests to verify they pass**

Run: `go test ./internal/cmd/ -run "TestTaskToBranch|TestParseTaskFile" -v`
Expected: ALL PASS

**Step 5: Commit**

```bash
git add internal/cmd/spawn_helpers.go internal/cmd/spawn_helpers_test.go
git commit -m "feat: add task-to-branch naming and task file parsing"
```

---

## Phase 2: tsp dash

### Task 4: Activity Detection Logic

**Files:**
- Create: `internal/cmd/dash_helpers.go`
- Create: `internal/cmd/dash_helpers_test.go`

**Step 1: Write the failing test**

Create `internal/cmd/dash_helpers_test.go`:

```go
package cmd

import (
	"testing"
	"time"
)

func TestInferStatus(t *testing.T) {
	now := time.Now()
	patterns := []string{"FAIL", "panic:", "Error:"}
	promptPattern := `\$\s*$`

	tests := []struct {
		name        string
		prev        string
		current     string
		lastChanged time.Time
		wantStatus  string
	}{
		{
			name:        "active when content changed",
			prev:        "compiling...",
			current:     "tests running...",
			lastChanged: now,
			wantStatus:  "active",
		},
		{
			name:        "idle when unchanged for 30s",
			prev:        "waiting...",
			current:     "waiting...",
			lastChanged: now.Add(-35 * time.Second),
			wantStatus:  "idle",
		},
		{
			name:        "done when prompt visible and idle 60s",
			prev:        "user@host:~$ ",
			current:     "user@host:~$ ",
			lastChanged: now.Add(-65 * time.Second),
			wantStatus:  "done",
		},
		{
			name:        "error when content has error pattern",
			prev:        "running tests...",
			current:     "--- FAIL: TestAuth (0.01s)",
			lastChanged: now,
			wantStatus:  "error",
		},
		{
			name:        "error overrides idle",
			prev:        "--- FAIL: TestAuth",
			current:     "--- FAIL: TestAuth",
			lastChanged: now.Add(-35 * time.Second),
			wantStatus:  "error",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := inferStatus(tt.prev, tt.current, tt.lastChanged, now, patterns, promptPattern)
			if got != tt.wantStatus {
				t.Errorf("inferStatus() = %q, want %q", got, tt.wantStatus)
			}
		})
	}
}

func TestStatusIcon(t *testing.T) {
	tests := []struct {
		status string
		want   string
	}{
		{"active", "●"},
		{"idle", "◌"},
		{"done", "✓"},
		{"error", "✗"},
		{"unknown", "?"},
	}
	for _, tt := range tests {
		t.Run(tt.status, func(t *testing.T) {
			got := statusIcon(tt.status)
			if got != tt.want {
				t.Errorf("statusIcon(%q) = %q, want %q", tt.status, got, tt.want)
			}
		})
	}
}

func TestFormatTimeSince(t *testing.T) {
	now := time.Now()
	tests := []struct {
		since time.Time
		want  string
	}{
		{now.Add(-2 * time.Second), "2s ago"},
		{now.Add(-3 * time.Minute), "3m ago"},
		{now.Add(-2 * time.Hour), "2h ago"},
	}
	for _, tt := range tests {
		t.Run(tt.want, func(t *testing.T) {
			got := formatTimeSince(tt.since, now)
			if got != tt.want {
				t.Errorf("formatTimeSince() = %q, want %q", got, tt.want)
			}
		})
	}
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./internal/cmd/ -run "TestInferStatus|TestStatusIcon|TestFormatTimeSince" -v`
Expected: FAIL — functions don't exist

**Step 3: Write minimal implementation**

Create `internal/cmd/dash_helpers.go`:

```go
package cmd

import (
	"fmt"
	"regexp"
	"strings"
	"time"
)

type sessionInfo struct {
	name        string
	status      string // active, idle, done, error
	lastChanged time.Time
	prevContent string
	paneContent string
	paneCount   int
	currentPane int
}

// inferStatus determines session status from pane content changes.
func inferStatus(prev, current string, lastChanged, now time.Time, errorPatterns []string, promptPattern string) string {
	// Check for error patterns first (highest priority)
	for _, pattern := range errorPatterns {
		if strings.Contains(current, pattern) {
			return "error"
		}
	}

	// Content changed → active
	if prev != current {
		return "active"
	}

	elapsed := now.Sub(lastChanged)

	// Check for shell prompt (done state)
	if elapsed > 60*time.Second && promptPattern != "" {
		if re, err := regexp.Compile(promptPattern); err == nil {
			lines := strings.Split(strings.TrimRight(current, "\n"), "\n")
			if len(lines) > 0 {
				lastLine := strings.TrimRight(lines[len(lines)-1], " ")
				if re.MatchString(lastLine) {
					return "done"
				}
			}
		}
	}

	// Unchanged for >30s → idle
	if elapsed > 30*time.Second {
		return "idle"
	}

	return "active"
}

func statusIcon(status string) string {
	switch status {
	case "active":
		return "●"
	case "idle":
		return "◌"
	case "done":
		return "✓"
	case "error":
		return "✗"
	default:
		return "?"
	}
}

func formatTimeSince(since, now time.Time) string {
	d := now.Sub(since)
	switch {
	case d < time.Minute:
		return fmt.Sprintf("%ds ago", int(d.Seconds()))
	case d < time.Hour:
		return fmt.Sprintf("%dm ago", int(d.Minutes()))
	default:
		return fmt.Sprintf("%dh ago", int(d.Hours()))
	}
}
```

**Step 4: Run tests to verify they pass**

Run: `go test ./internal/cmd/ -run "TestInferStatus|TestStatusIcon|TestFormatTimeSince" -v`
Expected: ALL PASS

**Step 5: Commit**

```bash
git add internal/cmd/dash_helpers.go internal/cmd/dash_helpers_test.go
git commit -m "feat(dash): add activity detection and status inference logic"
```

---

### Task 5: Dash TUI Model

**Files:**
- Create: `internal/cmd/dash.go`

This task builds the full TUI. It extends the `peek` pattern with the richer left panel and key actions.

**Step 1: Write the command**

Create `internal/cmd/dash.go`:

```go
package cmd

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/matteo-hertel/tmux-super-powers/config"
	tmuxpkg "github.com/matteo-hertel/tmux-super-powers/internal/tmux"
	"github.com/spf13/cobra"
)

var dashCmd = &cobra.Command{
	Use:   "dash",
	Short: "Real-time dashboard of all tmux sessions",
	Long: `Mission control for all your tmux sessions.

Shows live pane preview, activity status, and quick actions.

Key bindings:
  j/k or arrows  Navigate sessions
  tab             Cycle panes in selected session
  enter           Jump to session
  x               Kill session (with confirmation)
  q/esc           Quit`,
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

		m := dashModel{
			sessions:      make([]sessionInfo, len(sessions)),
			cfg:           cfg,
			lastRefreshed: time.Now(),
		}
		for i, s := range sessions {
			content := capturePaneContent(s, 0)
			m.sessions[i] = sessionInfo{
				name:        s,
				status:      "active",
				lastChanged: time.Now(),
				prevContent: "",
				paneContent: content,
			}
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

type dashModel struct {
	sessions      []sessionInfo
	cursor        int
	jumpTo        string
	previewPane   int
	width         int
	height        int
	cfg           *config.Config
	lastRefreshed time.Time
	confirmKill   bool // true when awaiting kill confirmation
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
		now := time.Now()
		for i := range m.sessions {
			s := &m.sessions[i]
			newContent := capturePaneContent(s.name, m.previewPaneFor(i))
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
		return m, dashTickCmd(m.cfg.Dash.RefreshMs)

	case tea.KeyMsg:
		if m.confirmKill {
			switch msg.String() {
			case "y":
				if m.cursor < len(m.sessions) {
					name := m.sessions[m.cursor].name
					tmuxpkg.KillSession(name)
					m.sessions = append(m.sessions[:m.cursor], m.sessions[m.cursor+1:]...)
					if m.cursor >= len(m.sessions) && m.cursor > 0 {
						m.cursor--
					}
				}
				m.confirmKill = false
				return m, nil
			default:
				m.confirmKill = false
				return m, nil
			}
		}

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
			m.previewPane++
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
			case "x":
				if len(m.sessions) > 0 {
					m.confirmKill = true
				}
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

func (m dashModel) previewPaneFor(i int) int {
	if i == m.cursor {
		return m.previewPane
	}
	return 0
}

func (m dashModel) View() string {
	if m.width == 0 || m.height == 0 {
		return "Loading..."
	}

	leftWidth := m.width * 35 / 100
	rightWidth := m.width - leftWidth - 3

	// Status color helpers
	statusColor := func(status string) lipgloss.Color {
		switch status {
		case "active":
			return lipgloss.Color("82")  // green
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

	now := time.Now()
	var sessionLines []string
	for i, s := range m.sessions {
		icon := statusIcon(s.status)
		timeSince := formatTimeSince(s.lastChanged, now)

		line := fmt.Sprintf(" %s %-20s %s", icon, truncate(s.name, 20), timeSince)

		if i == m.cursor {
			style := lipgloss.NewStyle().
				Bold(true).
				Foreground(lipgloss.Color("212")).
				Background(lipgloss.Color("236"))
			sessionLines = append(sessionLines, style.Render(fmt.Sprintf("▸%s", line)))
		} else {
			style := lipgloss.NewStyle().Foreground(statusColor(s.status))
			sessionLines = append(sessionLines, style.Render(fmt.Sprintf(" %s", line)))
		}
	}

	leftPanel := lipgloss.NewStyle().
		Width(leftWidth).
		Height(m.height - 4).
		BorderStyle(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("62")).
		Padding(1).
		Render(strings.Join(sessionLines, "\n"))

	// Right panel: live preview
	var previewContent string
	if len(m.sessions) > 0 && m.cursor < len(m.sessions) {
		previewContent = m.sessions[m.cursor].paneContent
	}
	if previewContent == "" {
		previewContent = "No content"
	}
	lines := strings.Split(previewContent, "\n")
	maxLines := m.height - 6
	if maxLines > 0 && len(lines) > maxLines {
		lines = lines[len(lines)-maxLines:]
	}
	previewContent = strings.Join(lines, "\n")

	rightPanel := lipgloss.NewStyle().
		Width(rightWidth).
		Height(m.height - 4).
		BorderStyle(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("62")).
		Padding(0, 1).
		Render(previewContent)

	title := lipgloss.NewStyle().
		Bold(true).
		Foreground(lipgloss.Color("212")).
		Render("  Dashboard — j/k: navigate | tab: pane | enter: jump | x: kill | q: quit")

	statusBar := ""
	if m.confirmKill && m.cursor < len(m.sessions) {
		statusBar = lipgloss.NewStyle().
			Foreground(lipgloss.Color("196")).
			Bold(true).
			Render(fmt.Sprintf("  Kill session '%s'? (y/n)", m.sessions[m.cursor].name))
	}

	layout := lipgloss.JoinHorizontal(lipgloss.Top, leftPanel, rightPanel)
	result := fmt.Sprintf("%s\n%s", title, layout)
	if statusBar != "" {
		result += "\n" + statusBar
	}

	return result
}

func truncate(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max-1] + "…"
}

// getWorktreeMap returns a map of session-name → worktree-branch
// for enriching dash display with worktree info.
func getWorktreeMap() map[string]string {
	cmd := exec.Command("git", "worktree", "list", "--porcelain")
	output, err := cmd.Output()
	if err != nil {
		return nil
	}
	result := make(map[string]string)
	worktrees := parseWorktreesPorcelain(string(output))
	for _, wt := range worktrees {
		// Session names are typically {repo}-{branch}
		result[wt.Branch] = wt.Path
	}
	return result
}
```

**Step 2: Run build to verify it compiles**

Run: `go build ./cmd/tsp`
Expected: compiles without errors

**Step 3: Commit**

```bash
git add internal/cmd/dash.go
git commit -m "feat: add tsp dash command — real-time session dashboard"
```

---

### Task 6: Register dash Command

**Files:**
- Modify: `internal/cmd/root.go`

**Step 1: Add dashCmd to root**

Add `rootCmd.AddCommand(dashCmd)` in `init()` in `root.go`.

**Step 2: Build and verify**

Run: `go build -o tsp ./cmd/tsp && ./tsp dash --help`
Expected: Shows dash help text

**Step 3: Commit**

```bash
git add internal/cmd/root.go
git commit -m "feat: register dash command in root"
```

---

## Phase 3: tsp spawn

### Task 7: Spawn Command

**Files:**
- Create: `internal/cmd/spawn.go`

**Step 1: Write the command**

Create `internal/cmd/spawn.go`:

```go
package cmd

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/matteo-hertel/tmux-super-powers/config"
	tmuxpkg "github.com/matteo-hertel/tmux-super-powers/internal/tmux"
	pathutil "github.com/matteo-hertel/tmux-super-powers/internal/pathutil"
	"github.com/spf13/cobra"
)

var spawnCmd = &cobra.Command{
	Use:   "spawn [flags] task1 task2 ...",
	Short: "Deploy multiple AI agents in parallel worktrees",
	Long: `Create worktrees with tmux sessions for each task and send the task prompt to claude.

Each task gets:
1. A branch auto-named from the task description (spawn/fix-auth-bug)
2. A git worktree
3. Dependencies installed
4. A tmux session with nvim (left) + claude (right)
5. The task prompt sent to claude automatically

Examples:
  tsp spawn "fix the auth bug" "add dark mode" "refactor db layer"
  tsp spawn --file tasks.txt
  tsp spawn --base main --dash "implement user avatars"
  tsp spawn --dry-run "test task"`,
	Args: cobra.ArbitraryArgs,
	Run: func(cmd *cobra.Command, args []string) {
		taskFile, _ := cmd.Flags().GetString("file")
		baseBranch, _ := cmd.Flags().GetString("base")
		openDash, _ := cmd.Flags().GetBool("dash")
		setup, _ := cmd.Flags().GetString("setup")
		noInstall, _ := cmd.Flags().GetBool("no-install")
		dryRun, _ := cmd.Flags().GetBool("dry-run")

		if !isGitRepo() {
			fmt.Fprintf(os.Stderr, "Error: not a git repository\n")
			os.Exit(1)
		}

		// Collect tasks
		var tasks []string
		if taskFile != "" {
			data, err := os.ReadFile(taskFile)
			if err != nil {
				fmt.Fprintf(os.Stderr, "Error reading task file: %v\n", err)
				os.Exit(1)
			}
			tasks = parseTaskFile(string(data))
		}
		tasks = append(tasks, args...)

		if len(tasks) == 0 {
			fmt.Fprintf(os.Stderr, "Error: no tasks provided\n")
			os.Exit(1)
		}

		// Resolve base branch
		if baseBranch == "" {
			var err error
			baseBranch, err = getCurrentBranch()
			if err != nil {
				fmt.Fprintf(os.Stderr, "Error: cannot determine current branch: %v\n", err)
				os.Exit(1)
			}
		}

		repoRoot, err := getRepoRoot()
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error: cannot determine repo root: %v\n", err)
			os.Exit(1)
		}
		repoName := filepath.Base(repoRoot)

		cfg, _ := config.Load()
		worktreeBase := pathutil.ExpandPath(cfg.Spawn.WorktreeBase)
		agentCmd := cfg.Spawn.AgentCommand
		if setup == "" {
			setup = cfg.Spawn.DefaultSetup
		}

		fmt.Printf("Spawning %d agents from branch %s...\n\n", len(tasks), baseBranch)

		for i, task := range tasks {
			branch := taskToBranch(task)
			branchShort := strings.TrimPrefix(branch, "spawn/")
			sessionName := tmuxpkg.SanitizeSessionName(fmt.Sprintf("%s-%s", repoName, branchShort))
			worktreePath := filepath.Join(worktreeBase, fmt.Sprintf("%s-%s", repoName, branchShort))

			fmt.Printf("[%d/%d] %s\n", i+1, len(tasks), branchShort)

			if dryRun {
				fmt.Printf("      branch:    %s\n", branch)
				fmt.Printf("      worktree:  %s\n", worktreePath)
				fmt.Printf("      session:   %s\n", sessionName)
				fmt.Printf("      prompt:    %s\n\n", task)
				continue
			}

			// Create branch
			if !branchExists(branch) {
				if err := createBranch(branch, baseBranch); err != nil {
					fmt.Printf("      ✗ branch creation failed: %v\n", err)
					continue
				}
				fmt.Printf("      ✓ branch created\n")
			} else {
				fmt.Printf("      ✓ branch exists\n")
			}

			// Create worktree
			if _, err := os.Stat(worktreePath); err == nil {
				fmt.Printf("      ✓ worktree exists at %s\n", worktreePath)
			} else {
				if err := createWorktree(worktreePath, branch); err != nil {
					fmt.Printf("      ✗ worktree creation failed: %v\n", err)
					continue
				}
				fmt.Printf("      ✓ worktree created\n")
			}

			// Install dependencies
			if !noInstall {
				pm := detectPackageManager(repoRoot)
				if pm != "" {
					if pm == "yarn" {
						copyYarnCache(repoRoot, worktreePath)
					}
					fmt.Printf("      ◌ %s install...\n", pm)
					if err := runPackageManager(pm, worktreePath); err != nil {
						fmt.Printf("      ⚠ %s install failed: %v\n", pm, err)
					} else {
						fmt.Printf("      ✓ %s install\n", pm)
					}
				}
			}

			// Run setup command
			if setup != "" {
				fmt.Printf("      ◌ running setup...\n")
				setupCmd := exec.Command("sh", "-c", setup)
				setupCmd.Dir = worktreePath
				if err := setupCmd.Run(); err != nil {
					fmt.Printf("      ⚠ setup failed: %v\n", err)
				} else {
					fmt.Printf("      ✓ setup complete\n")
				}
			}

			// Create tmux session
			if tmuxpkg.SessionExists(sessionName) {
				tmuxpkg.KillSession(sessionName)
			}
			tmuxpkg.CreateTwoPaneSession(sessionName, worktreePath, "nvim", agentCmd)
			fmt.Printf("      ✓ session created\n")

			// Send task prompt to claude pane
			target := fmt.Sprintf("%s:0.1", sessionName)
			tmuxpkg.SendKeys(target, task)
			fmt.Printf("      ✓ prompt sent to agent\n\n")
		}

		if dryRun {
			fmt.Println("Dry run complete. No changes made.")
			return
		}

		fmt.Printf("All %d agents deployed.", len(tasks))
		if openDash {
			fmt.Println(" Opening dashboard...")
			// Re-exec as tsp dash
			dashExec := exec.Command(os.Args[0], "dash")
			dashExec.Stdin = os.Stdin
			dashExec.Stdout = os.Stdout
			dashExec.Stderr = os.Stderr
			dashExec.Run()
		} else {
			fmt.Println(" Run `tsp dash` to monitor.")
		}
	},
}

func init() {
	spawnCmd.Flags().StringP("file", "f", "", "Read tasks from file (one per line)")
	spawnCmd.Flags().StringP("base", "b", "", "Base branch for worktrees (default: current branch)")
	spawnCmd.Flags().Bool("dash", false, "Open tsp dash after deploying all agents")
	spawnCmd.Flags().String("setup", "", "Command to run in each worktree after install")
	spawnCmd.Flags().Bool("no-install", false, "Skip dependency installation")
	spawnCmd.Flags().Bool("dry-run", false, "Show what would be created without doing it")
}
```

**Step 2: Build and verify**

Run: `go build ./cmd/tsp`
Expected: compiles

**Step 3: Commit**

```bash
git add internal/cmd/spawn.go
git commit -m "feat: add tsp spawn command — multi-agent deployer"
```

---

### Task 8: Register spawn Command

**Files:**
- Modify: `internal/cmd/root.go`

**Step 1: Add spawnCmd to root**

Add `rootCmd.AddCommand(spawnCmd)` in `init()` in `root.go`.

**Step 2: Build and verify**

Run: `go build -o tsp ./cmd/tsp && ./tsp spawn --help`
Expected: Shows spawn help text

**Step 3: Commit**

```bash
git add internal/cmd/root.go
git commit -m "feat: register spawn command in root"
```

---

## Phase 4: tsp harvest

### Task 9: Harvest Data Collection Helpers

**Files:**
- Create: `internal/cmd/harvest_helpers.go`
- Create: `internal/cmd/harvest_helpers_test.go`

**Step 1: Write the failing test**

Create `internal/cmd/harvest_helpers_test.go`:

```go
package cmd

import "testing"

func TestParseDiffStat(t *testing.T) {
	input := ` src/auth.go   | 12 ++++++------
 src/db.go     |  4 ++--
 2 files changed, 8 insertions(+), 8 deletions(-)`

	files, ins, del := parseDiffStat(input)
	if files != 2 {
		t.Errorf("files: got %d, want 2", files)
	}
	if ins != 8 {
		t.Errorf("insertions: got %d, want 8", ins)
	}
	if del != 8 {
		t.Errorf("deletions: got %d, want 8", del)
	}
}

func TestParseDiffStatEmpty(t *testing.T) {
	files, ins, del := parseDiffStat("")
	if files != 0 || ins != 0 || del != 0 {
		t.Errorf("expected all zeros, got %d %d %d", files, ins, del)
	}
}

func TestFormatPRComments(t *testing.T) {
	comments := []prComment{
		{File: "src/auth.go", Line: 45, Author: "reviewer", Body: "Handle empty token"},
		{File: "src/auth.go", Line: 78, Author: "reviewer", Body: "Use a constant"},
		{File: "src/db.go", Line: 10, Author: "other", Body: "Good catch"},
	}
	result := formatPRComments(comments)
	if !strings.Contains(result, "src/auth.go") {
		t.Error("expected auth.go in output")
	}
	if !strings.Contains(result, "Handle empty token") {
		t.Error("expected comment body in output")
	}
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./internal/cmd/ -run "TestParseDiffStat|TestFormatPRComments" -v`
Expected: FAIL

**Step 3: Write minimal implementation**

Create `internal/cmd/harvest_helpers.go`:

```go
package cmd

import (
	"encoding/json"
	"fmt"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
)

type worktreeInfo struct {
	sessionName  string
	branch       string
	baseBranch   string
	worktreePath string
	filesChanged int
	insertions   int
	deletions    int
	status       string // "ready", "wip", "clean"
	diffOutput   string
	aheadCount   int
	prNumber     int
	prURL        string
	ciStatus     string // "pass", "fail", "pending", ""
	reviewCount  int
}

type prComment struct {
	File   string
	Line   int
	Author string
	Body   string
}

var diffStatSummary = regexp.MustCompile(`(\d+) files? changed(?:, (\d+) insertions?\(\+\))?(?:, (\d+) deletions?\(-\))?`)

// parseDiffStat parses the summary line of `git diff --stat`.
func parseDiffStat(output string) (files, insertions, deletions int) {
	matches := diffStatSummary.FindStringSubmatch(output)
	if len(matches) == 0 {
		return 0, 0, 0
	}
	files, _ = strconv.Atoi(matches[1])
	if matches[2] != "" {
		insertions, _ = strconv.Atoi(matches[2])
	}
	if matches[3] != "" {
		deletions, _ = strconv.Atoi(matches[3])
	}
	return
}

// collectWorktreeInfo gathers diff and PR data for a worktree.
func collectWorktreeInfo(wt Worktree, repoName string) worktreeInfo {
	info := worktreeInfo{
		sessionName:  fmt.Sprintf("%s-%s", repoName, wt.Branch),
		branch:       wt.Branch,
		worktreePath: wt.Path,
	}

	// Get diff stat
	statCmd := exec.Command("git", "-C", wt.Path, "diff", "--stat")
	if out, err := statCmd.Output(); err == nil {
		info.filesChanged, info.insertions, info.deletions = parseDiffStat(string(out))
	}

	// Determine status
	if info.filesChanged > 0 {
		info.status = "wip"
	} else {
		// Check commits ahead of base
		logCmd := exec.Command("git", "-C", wt.Path, "log", "--oneline", "HEAD")
		if out, err := logCmd.Output(); err == nil {
			lines := strings.Split(strings.TrimSpace(string(out)), "\n")
			info.aheadCount = len(lines)
		}
		if info.aheadCount > 0 {
			info.status = "ready"
		} else {
			info.status = "clean"
		}
	}

	// Get full diff
	diffCmd := exec.Command("git", "-C", wt.Path, "diff")
	if out, err := diffCmd.Output(); err == nil {
		info.diffOutput = string(out)
	}

	// Try to find PR
	info.prNumber, info.prURL = findPRForBranch(wt.Branch)
	if info.prNumber > 0 {
		info.ciStatus = getCIStatus(info.prNumber)
		info.reviewCount = getReviewCommentCount(info.prNumber)
	}

	return info
}

// findPRForBranch uses gh CLI to find a PR for the given branch.
func findPRForBranch(branch string) (int, string) {
	cmd := exec.Command("gh", "pr", "list", "--head", branch, "--json", "number,url", "--limit", "1")
	out, err := cmd.Output()
	if err != nil {
		return 0, ""
	}
	var prs []struct {
		Number int    `json:"number"`
		URL    string `json:"url"`
	}
	if err := json.Unmarshal(out, &prs); err != nil || len(prs) == 0 {
		return 0, ""
	}
	return prs[0].Number, prs[0].URL
}

// getCIStatus checks CI status for a PR number.
func getCIStatus(prNumber int) string {
	cmd := exec.Command("gh", "pr", "checks", fmt.Sprintf("%d", prNumber), "--json", "conclusion", "--jq", ".[].conclusion")
	out, err := cmd.Output()
	if err != nil {
		return ""
	}
	conclusions := strings.TrimSpace(string(out))
	if strings.Contains(conclusions, "failure") {
		return "fail"
	}
	if strings.Contains(conclusions, "pending") || strings.Contains(conclusions, "queued") {
		return "pending"
	}
	if conclusions != "" {
		return "pass"
	}
	return ""
}

// getReviewCommentCount returns the number of PR review comments.
func getReviewCommentCount(prNumber int) int {
	cmd := exec.Command("gh", "pr", "view", fmt.Sprintf("%d", prNumber), "--json", "comments", "--jq", ".comments | length")
	out, err := cmd.Output()
	if err != nil {
		return 0
	}
	count, _ := strconv.Atoi(strings.TrimSpace(string(out)))
	return count
}

// fetchFailingCILogs fetches failing CI logs for a PR.
func fetchFailingCILogs(prNumber int) (string, error) {
	// Get failing run IDs
	cmd := exec.Command("gh", "pr", "checks", fmt.Sprintf("%d", prNumber), "--json", "name,conclusion,detailsUrl")
	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("failed to get checks: %w", err)
	}

	var checks []struct {
		Name       string `json:"name"`
		Conclusion string `json:"conclusion"`
		DetailsURL string `json:"detailsUrl"`
	}
	if err := json.Unmarshal(out, &checks); err != nil {
		return "", err
	}

	var logs strings.Builder
	for _, check := range checks {
		if check.Conclusion == "failure" {
			logs.WriteString(fmt.Sprintf("### %s (FAILED)\n", check.Name))
			// Try to get run logs
			logCmd := exec.Command("gh", "run", "view", "--log-failed")
			logOut, err := logCmd.Output()
			if err == nil {
				logs.WriteString(string(logOut))
			} else {
				logs.WriteString(fmt.Sprintf("(could not fetch logs: %v)\n", err))
			}
			logs.WriteString("\n")
		}
	}

	if logs.Len() == 0 {
		return "", fmt.Errorf("no failing checks found")
	}
	return logs.String(), nil
}

// fetchPRComments fetches review comments for a PR.
func fetchPRComments(prNumber int) ([]prComment, error) {
	cmd := exec.Command("gh", "api", fmt.Sprintf("repos/{owner}/{repo}/pulls/%d/comments", prNumber))
	out, err := cmd.Output()
	if err != nil {
		return nil, err
	}

	var raw []struct {
		Path string `json:"path"`
		Line int    `json:"line"`
		User struct {
			Login string `json:"login"`
		} `json:"user"`
		Body string `json:"body"`
	}
	if err := json.Unmarshal(out, &raw); err != nil {
		return nil, err
	}

	var comments []prComment
	for _, r := range raw {
		comments = append(comments, prComment{
			File:   r.Path,
			Line:   r.Line,
			Author: r.User.Login,
			Body:   r.Body,
		})
	}
	return comments, nil
}

// formatPRComments formats PR comments grouped by file.
func formatPRComments(comments []prComment) string {
	byFile := make(map[string][]prComment)
	for _, c := range comments {
		byFile[c.File] = append(byFile[c.File], c)
	}

	var b strings.Builder
	b.WriteString("## PR Review Comments\n\n")
	for file, cs := range byFile {
		b.WriteString(fmt.Sprintf("### %s\n", file))
		for _, c := range cs {
			b.WriteString(fmt.Sprintf("Line %d — @%s: %q\n", c.Line, c.Author, c.Body))
		}
		b.WriteString("\n")
	}
	return b.String()
}
```

**Step 4: Run tests to verify they pass**

Run: `go test ./internal/cmd/ -run "TestParseDiffStat|TestFormatPRComments" -v`
Expected: ALL PASS

**Step 5: Commit**

```bash
git add internal/cmd/harvest_helpers.go internal/cmd/harvest_helpers_test.go
git commit -m "feat(harvest): add diff parsing, PR/CI data collection helpers"
```

---

### Task 10: Harvest TUI Model

**Files:**
- Create: `internal/cmd/harvest.go`

This is the main TUI for harvest with all key bindings including `f` (fix CI) and `r` (review comments).

**Step 1: Write the command**

Create `internal/cmd/harvest.go`:

```go
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
  tab             Switch between list and diff panels
  enter           Expand/collapse file diff
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
	worktrees     []worktreeInfo
	cursor        int
	jumpTo        string
	repoName      string
	scrollOffset  int
	mode          harvestMode
	statusMsg     string
	textInput     textinput.Model
	width         int
	height        int
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

	// Get repo root from worktree path (go up to find .git)
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

		line := fmt.Sprintf(" %-18s %8s %s%s", truncate(wt.branch, 18), stat, wt.status, prInfo)

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
```

**Step 2: Build and verify**

Run: `go build ./cmd/tsp`
Expected: compiles

**Step 3: Commit**

```bash
git add internal/cmd/harvest.go
git commit -m "feat: add tsp harvest command — review diffs, fix CI, address PR comments"
```

---

### Task 11: Register harvest Command

**Files:**
- Modify: `internal/cmd/root.go`

**Step 1: Add harvestCmd to root**

Add `rootCmd.AddCommand(harvestCmd)` in `init()` in `root.go`.

**Step 2: Build and verify**

Run: `go build -o tsp ./cmd/tsp && ./tsp harvest --help`
Expected: Shows harvest help text

**Step 3: Commit**

```bash
git add internal/cmd/root.go
git commit -m "feat: register harvest command in root"
```

---

## Phase 5: Consolidation

### Task 12: Merge sandbox + project → `tsp new`

**Files:**
- Create: `internal/cmd/new.go`
- Modify: `internal/cmd/root.go`

**Step 1: Write the command**

Create `internal/cmd/new.go`:

```go
package cmd

import (
	"fmt"
	"os"

	"github.com/matteo-hertel/tmux-super-powers/config"
	"github.com/spf13/cobra"
)

var newCmd = &cobra.Command{
	Use:   "new [name]",
	Short: "Create a new project (sandbox or project)",
	Long: `Create a new project directory with a tmux session.

Use --sandbox or --project to specify the type.
If neither is specified, defaults to project.

Examples:
  tsp new myapp --sandbox
  tsp new myapp --project
  tsp new                    # interactive, defaults to project`,
	Run: func(cmd *cobra.Command, args []string) {
		isSandbox, _ := cmd.Flags().GetBool("sandbox")

		cfg, err := config.Load()
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error loading config: %v\n", err)
			os.Exit(1)
		}

		var creatorCfg projectCreatorConfig
		if isSandbox {
			creatorCfg = projectCreatorConfig{
				Title:         "Create a new sandbox project",
				Placeholder:   "Enter project name",
				BasePath:      cfg.Sandbox.Path,
				SessionPrefix: "sandbox",
			}
		} else {
			creatorCfg = projectCreatorConfig{
				Title:         "Create a new project",
				Placeholder:   "Enter project name",
				BasePath:      cfg.Projects.Path,
				SessionPrefix: "project",
			}
		}

		runProjectCreator(creatorCfg)
	},
}

func init() {
	newCmd.Flags().Bool("sandbox", false, "Create in sandbox directory")
	newCmd.Flags().Bool("project", false, "Create in projects directory (default)")
}
```

**Step 2: Register in root.go**

Add `rootCmd.AddCommand(newCmd)` in `init()`. Keep `sandboxCmd` and `projectCmd` registered for backward compat (they still work).

**Step 3: Build and verify**

Run: `go build -o tsp ./cmd/tsp && ./tsp new --help`
Expected: Shows new command help

**Step 4: Commit**

```bash
git add internal/cmd/new.go internal/cmd/root.go
git commit -m "feat: add tsp new command (consolidates sandbox + project)"
```

---

### Task 13: Merge wtx-rm + txrm → `tsp rm`

**Files:**
- Create: `internal/cmd/rm.go`
- Modify: `internal/cmd/root.go`

**Step 1: Write the command**

Create `internal/cmd/rm.go`:

```go
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
			isWt := false
			if wt, ok := wtMap[session]; ok {
				isWt = true
				_ = wt
			}
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
```

**Step 2: Register in root.go**

Add `rootCmd.AddCommand(rmCmd)` in `init()`. Keep `wtxRmCmd` and `txrmCmd` for backward compat.

**Step 3: Build and verify**

Run: `go build -o tsp ./cmd/tsp && ./tsp rm --help`
Expected: Shows rm command help

**Step 4: Commit**

```bash
git add internal/cmd/rm.go internal/cmd/root.go
git commit -m "feat: add tsp rm command (consolidates wtx-rm + txrm)"
```

---

### Task 14: Update CLAUDE.md

**Files:**
- Modify: `CLAUDE.md`

**Step 1: Update command list**

Add the new commands to the Architecture section:

```markdown
- `tsp dash` - Real-time session dashboard (mission control)
- `tsp spawn` - Deploy multiple AI agents in parallel worktrees
- `tsp harvest` - Review diffs, create PRs, fix CI, address review comments
- `tsp new` - Create new project (consolidates sandbox + project)
- `tsp rm` - Remove sessions with smart worktree detection
```

**Step 2: Add workflow section**

```markdown
### Parallel Agent Workflow
1. `tsp spawn "task1" "task2" --dash` — deploy agents
2. `tsp dash` — monitor in real-time
3. `tsp harvest` — review diffs, create PRs, fix CI, merge
4. `tsp rm` — clean up remaining sessions
```

**Step 3: Commit**

```bash
git add CLAUDE.md
git commit -m "docs: update CLAUDE.md with new commands and workflow"
```

---

### Task 15: Full Test Suite Run

**Step 1: Run all tests**

Run: `go test ./... -v`
Expected: ALL PASS

**Step 2: Build final binary**

Run: `go build -o tsp ./cmd/tsp && ./tsp --help`
Expected: Shows all commands including dash, spawn, harvest, new, rm

**Step 3: Verify no unused imports or lint issues**

Run: `go vet ./...`
Expected: No issues

**Step 4: Final commit if any fixes needed**

```bash
git add -A
git commit -m "chore: fix any test/lint issues from integration"
```

---

## Summary

| Phase | Tasks | New Files | Modified Files |
|-------|-------|-----------|----------------|
| 1: Foundation | 1-3 | spawn_helpers.go, spawn_helpers_test.go | config.go, config_test.go, tmux.go, tmux_test.go |
| 2: Dash | 4-6 | dash.go, dash_helpers.go, dash_helpers_test.go | root.go |
| 3: Spawn | 7-8 | spawn.go | root.go |
| 4: Harvest | 9-11 | harvest.go, harvest_helpers.go, harvest_helpers_test.go | root.go |
| 5: Consolidation | 12-15 | new.go, rm.go | root.go, CLAUDE.md |

**Total: 15 tasks, ~10 new files, ~6 modified files**
