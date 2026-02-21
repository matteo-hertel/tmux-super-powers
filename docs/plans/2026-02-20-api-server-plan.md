# TSP API Server Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Expose tsp functionality as HTTP/WebSocket API endpoints for a native mobile app over Tailscale.

**Architecture:** A `tsp serve` command starts an HTTP server bound to the Tailscale interface. A service layer extracts business logic from `internal/cmd/` so both TUI and API share the same code. WebSocket streams real-time session data. Launchd plist support for always-on daemon.

**Tech Stack:** Go net/http (stdlib), gorilla/websocket, existing tmux/config packages.

---

### Task 1: Add gorilla/websocket dependency

**Files:**
- Modify: `go.mod`

**Step 1: Add the dependency**

Run: `cd /Users/matteohertel/work/code/sandbox/tmux-super-powers && go get github.com/gorilla/websocket`

**Step 2: Tidy modules**

Run: `cd /Users/matteohertel/work/code/sandbox/tmux-super-powers && go mod tidy`

**Step 3: Verify**

Run: `grep gorilla go.mod`
Expected: `github.com/gorilla/websocket` appears in require block

**Step 4: Commit**

```bash
git add go.mod go.sum
git commit -m "deps: add gorilla/websocket for API server"
```

---

### Task 2: Add server config to Config struct

**Files:**
- Modify: `config/config.go:11-19` (add ServeConfig to Config struct)
- Test: `config/config_test.go`

**Step 1: Write the failing test**

Add to `config/config_test.go`:

```go
func TestServeConfigDefaults(t *testing.T) {
	cfg := &Config{}
	// After Load, serve defaults should be populated
	tmpFile := filepath.Join(t.TempDir(), "test-config.yaml")
	os.WriteFile(tmpFile, []byte("directories:\n  - ~/test\n"), 0644)
	loaded, err := LoadFrom(tmpFile)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if loaded.Serve.Port != 7777 {
		t.Errorf("expected default port 7777, got %d", loaded.Serve.Port)
	}
	if loaded.Serve.RefreshMs != 500 {
		t.Errorf("expected default refresh 500, got %d", loaded.Serve.RefreshMs)
	}
}
```

**Step 2: Run test to verify it fails**

Run: `cd /Users/matteohertel/work/code/sandbox/tmux-super-powers && go test ./config/ -run TestServeConfigDefaults -v`
Expected: FAIL — `loaded.Serve` undefined

**Step 3: Write minimal implementation**

Add `ServeConfig` struct and field to `config/config.go`:

```go
type ServeConfig struct {
	Port      int    `yaml:"port"`
	Bind      string `yaml:"bind"`
	RefreshMs int    `yaml:"refresh_ms"`
}
```

Add to `Config` struct:

```go
Serve ServeConfig `yaml:"serve"`
```

Add defaults in `Load()` after the spawn defaults block (~line 92):

```go
// Serve defaults
if cfg.Serve.Port == 0 {
	cfg.Serve.Port = 7777
}
if cfg.Serve.RefreshMs == 0 {
	cfg.Serve.RefreshMs = cfg.Dash.RefreshMs
}
```

Add to `defaultConfig()`:

```go
Serve: ServeConfig{
	Port:      7777,
	RefreshMs: 500,
},
```

**Step 4: Run test to verify it passes**

Run: `cd /Users/matteohertel/work/code/sandbox/tmux-super-powers && go test ./config/ -run TestServeConfigDefaults -v`
Expected: PASS

**Step 5: Commit**

```bash
git add config/config.go config/config_test.go
git commit -m "feat: add serve config with port/bind/refresh defaults"
```

---

### Task 3: Create service layer — session operations

This extracts tmux session logic from `internal/cmd/` into a reusable service.

**Files:**
- Create: `internal/service/sessions.go`
- Create: `internal/service/sessions_test.go`

**Step 1: Write the failing tests**

Create `internal/service/sessions_test.go`:

```go
package service

import (
	"testing"
)

func TestPaneTypeFromProcess(t *testing.T) {
	tests := []struct {
		process string
		want    string
	}{
		{"nvim", "editor"},
		{"vim", "editor"},
		{"emacs", "editor"},
		{"nano", "editor"},
		{"claude", "agent"},
		{"bash", "shell"},
		{"zsh", "shell"},
		{"fish", "shell"},
		{"node", "process"},
		{"npm", "process"},
		{"go", "process"},
		{"python", "process"},
		{"", "shell"},
	}
	for _, tt := range tests {
		t.Run(tt.process, func(t *testing.T) {
			got := PaneTypeFromProcess(tt.process)
			if got != tt.want {
				t.Errorf("PaneTypeFromProcess(%q) = %q, want %q", tt.process, got, tt.want)
			}
		})
	}
}

func TestSessionToJSON(t *testing.T) {
	s := Session{
		Name:       "test-session",
		Status:     "active",
		Branch:     "main",
		IsWorktree: false,
		IsGitRepo:  true,
		Panes: []Pane{
			{Index: 0, Type: "editor", Process: "nvim"},
			{Index: 1, Type: "agent", Status: "active", Content: "working..."},
		},
	}
	if s.Name != "test-session" {
		t.Errorf("expected name test-session, got %s", s.Name)
	}
	if len(s.Panes) != 2 {
		t.Errorf("expected 2 panes, got %d", len(s.Panes))
	}
	if s.Panes[0].Type != "editor" {
		t.Errorf("expected pane 0 type editor, got %s", s.Panes[0].Type)
	}
}
```

**Step 2: Run tests to verify they fail**

Run: `cd /Users/matteohertel/work/code/sandbox/tmux-super-powers && go test ./internal/service/ -v`
Expected: FAIL — package doesn't exist

**Step 3: Write minimal implementation**

Create `internal/service/sessions.go`:

```go
package service

import (
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"
	"time"

	tmuxpkg "github.com/matteo-hertel/tmux-super-powers/internal/tmux"
)

// Session represents a tmux session with all enriched data.
type Session struct {
	Name        string    `json:"name"`
	Status      string    `json:"status"`
	Branch      string    `json:"branch,omitempty"`
	IsWorktree  bool      `json:"isWorktree"`
	IsGitRepo   bool      `json:"isGitRepo"`
	GitPath     string    `json:"-"`
	LastChanged time.Time `json:"lastChanged"`
	Panes       []Pane    `json:"panes"`
	Diff        *DiffStat `json:"diff,omitempty"`
	PR          *PRInfo   `json:"pr,omitempty"`

	// Internal state (not serialized)
	prevContent  string
	worktreePath string
}

// Pane represents a single tmux pane within a session.
type Pane struct {
	Index   int    `json:"index"`
	Type    string `json:"type"`    // editor, agent, shell, process
	Process string `json:"process"` // process name (nvim, claude, bash, etc.)
	Status  string `json:"status,omitempty"`
	Content string `json:"content,omitempty"`
}

// DiffStat holds git diff statistics.
type DiffStat struct {
	Files      int `json:"files"`
	Insertions int `json:"insertions"`
	Deletions  int `json:"deletions"`
}

// PRInfo holds pull request metadata.
type PRInfo struct {
	Number      int    `json:"number"`
	URL         string `json:"url"`
	CIStatus    string `json:"ciStatus"`
	ReviewCount int    `json:"reviewCount"`
}

// PaneTypeFromProcess classifies a pane based on the running process name.
func PaneTypeFromProcess(process string) string {
	p := strings.ToLower(process)
	switch {
	case p == "nvim" || p == "vim" || p == "emacs" || p == "nano":
		return "editor"
	case p == "claude" || strings.HasPrefix(p, "claude"):
		return "agent"
	case p == "bash" || p == "zsh" || p == "fish" || p == "sh" || p == "":
		return "shell"
	default:
		return "process"
	}
}

// ListSessions returns all tmux session names.
func ListSessions() ([]string, error) {
	cmd := exec.Command("tmux", "list-sessions", "-F", "#{session_name}")
	output, err := cmd.Output()
	if err != nil {
		if strings.Contains(err.Error(), "no server running") {
			return []string{}, nil
		}
		return nil, err
	}
	names := strings.Split(strings.TrimSpace(string(output)), "\n")
	if len(names) == 1 && names[0] == "" {
		return []string{}, nil
	}
	return names, nil
}

// GetPaneProcess returns the process running in a specific pane.
func GetPaneProcess(session string, pane int) string {
	target := fmt.Sprintf("%s:0.%d", session, pane)
	cmd := exec.Command("tmux", "display-message", "-t", target, "-p", "#{pane_current_command}")
	out, err := cmd.Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

// GetPaneCount returns the number of panes in a session's first window.
func GetPaneCount(session string) int {
	cmd := exec.Command("tmux", "list-panes", "-t", session, "-F", "#{pane_index}")
	out, err := cmd.Output()
	if err != nil {
		return 1
	}
	lines := strings.Split(strings.TrimSpace(string(out)), "\n")
	return len(lines)
}

// CapturePaneContent captures the visible content of a pane.
func CapturePaneContent(session string, pane int) string {
	target := fmt.Sprintf("%s:0.%d", session, pane)
	cmd := exec.Command("tmux", tmuxpkg.BuildCapturePaneArgs(target)...)
	output, err := cmd.Output()
	if err != nil {
		if pane > 0 {
			return CapturePaneContent(session, 0)
		}
		return fmt.Sprintf("(unable to capture: %v)", err)
	}
	return string(output)
}

// DetectSessionGitInfo checks if a session's cwd is inside a git repo.
func DetectSessionGitInfo(sessionName string) (gitPath, branch string) {
	cwd := tmuxpkg.GetPaneCwd(sessionName)
	if cwd == "" {
		return "", ""
	}
	topCmd := exec.Command("git", "-C", cwd, "rev-parse", "--show-toplevel")
	topOut, err := topCmd.Output()
	if err != nil {
		return "", ""
	}
	gitPath = strings.TrimSpace(string(topOut))

	branchCmd := exec.Command("git", "-C", gitPath, "rev-parse", "--abbrev-ref", "HEAD")
	branchOut, err := branchCmd.Output()
	if err != nil {
		return gitPath, ""
	}
	branch = strings.TrimSpace(string(branchOut))
	return gitPath, branch
}

// KillSession kills a tmux session and optionally cleans up its worktree.
func KillSession(name string, cleanupWorktree bool, worktreePath, branch string) error {
	if err := tmuxpkg.KillSession(name); err != nil {
		return fmt.Errorf("failed to kill session: %w", err)
	}
	if cleanupWorktree && worktreePath != "" {
		exec.Command("rm", "-rf", worktreePath).Run()
		exec.Command("git", "worktree", "remove", worktreePath, "--force").Run()
		if branch != "" {
			exec.Command("git", "branch", "-D", branch).Run()
		}
	}
	return nil
}

// CreateSession creates a new tmux session.
func CreateSession(name, dir, leftCmd, rightCmd string) error {
	if tmuxpkg.SessionExists(name) {
		return fmt.Errorf("session %q already exists", name)
	}
	return tmuxpkg.CreateTwoPaneSession(name, dir, leftCmd, rightCmd)
}

// SendToPane sends text to a specific pane.
func SendToPane(session string, pane int, text string) error {
	target := fmt.Sprintf("%s:0.%d", session, pane)
	return tmuxpkg.SendKeys(target, text)
}

// TmuxRunning checks if the tmux server is running.
func TmuxRunning() bool {
	cmd := exec.Command("tmux", "list-sessions")
	return cmd.Run() == nil
}

// GhAvailable checks if the gh CLI is installed.
func GhAvailable() bool {
	_, err := exec.LookPath("gh")
	return err == nil
}

// MarshalSessions serializes sessions to JSON.
func MarshalSessions(sessions []Session) ([]byte, error) {
	return json.Marshal(map[string]interface{}{
		"sessions": sessions,
	})
}
```

**Step 4: Run tests to verify they pass**

Run: `cd /Users/matteohertel/work/code/sandbox/tmux-super-powers && go test ./internal/service/ -v`
Expected: PASS

**Step 5: Commit**

```bash
git add internal/service/sessions.go internal/service/sessions_test.go
git commit -m "feat: add service layer with session operations and pane type detection"
```

---

### Task 4: Create service layer — git/PR operations

**Files:**
- Create: `internal/service/git.go`
- Create: `internal/service/git_test.go`

**Step 1: Write the failing tests**

Create `internal/service/git_test.go`:

```go
package service

import (
	"testing"
)

func TestParseDiffStat(t *testing.T) {
	tests := []struct {
		input                        string
		wantFiles, wantIns, wantDel int
	}{
		{" 3 files changed, 45 insertions(+), 12 deletions(-)", 3, 45, 12},
		{" 1 file changed, 10 insertions(+)", 1, 10, 0},
		{" 2 files changed, 5 deletions(-)", 2, 0, 5},
		{"no match here", 0, 0, 0},
		{"", 0, 0, 0},
	}
	for _, tt := range tests {
		files, ins, del := ParseDiffStat(tt.input)
		if files != tt.wantFiles || ins != tt.wantIns || del != tt.wantDel {
			t.Errorf("ParseDiffStat(%q) = (%d,%d,%d), want (%d,%d,%d)",
				tt.input, files, ins, del, tt.wantFiles, tt.wantIns, tt.wantDel)
		}
	}
}

func TestFormatPRComments(t *testing.T) {
	comments := []PRComment{
		{File: "main.go", Line: 10, Author: "alice", Body: "fix this"},
		{File: "main.go", Line: 20, Author: "bob", Body: "looks wrong"},
		{File: "util.go", Line: 5, Author: "alice", Body: "add test"},
	}
	out := FormatPRComments(comments)
	if out == "" {
		t.Error("expected non-empty output")
	}
	if !containsStr(out, "main.go") || !containsStr(out, "util.go") {
		t.Error("expected both files in output")
	}
}

func containsStr(s, sub string) bool {
	return len(s) >= len(sub) && (s == sub || len(s) > 0 && containsSubstring(s, sub))
}

func containsSubstring(s, sub string) bool {
	for i := 0; i <= len(s)-len(sub); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
```

**Step 2: Run tests to verify they fail**

Run: `cd /Users/matteohertel/work/code/sandbox/tmux-super-powers && go test ./internal/service/ -run "TestParseDiffStat|TestFormatPRComments" -v`
Expected: FAIL — functions don't exist

**Step 3: Write minimal implementation**

Create `internal/service/git.go`:

```go
package service

import (
	"encoding/json"
	"fmt"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
)

// PRComment represents a single PR review comment.
type PRComment struct {
	File   string `json:"file"`
	Line   int    `json:"line"`
	Author string `json:"author"`
	Body   string `json:"body"`
}

var diffStatSummaryRe = regexp.MustCompile(`(\d+) files? changed(?:, (\d+) insertions?\(\+\))?(?:, (\d+) deletions?\(-\))?`)

// ParseDiffStat parses the summary line of `git diff --stat`.
func ParseDiffStat(output string) (files, insertions, deletions int) {
	matches := diffStatSummaryRe.FindStringSubmatch(output)
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

// GetDiffStat gets diff statistics for a git repo.
func GetDiffStat(gitPath string) (files, insertions, deletions int, diffOutput string) {
	statCmd := exec.Command("git", "-C", gitPath, "diff", "--stat")
	if out, err := statCmd.Output(); err == nil {
		files, insertions, deletions = ParseDiffStat(string(out))
	}
	diffCmd := exec.Command("git", "-C", gitPath, "diff")
	if out, err := diffCmd.Output(); err == nil {
		diffOutput = string(out)
	}
	return
}

// FindPRForBranch uses gh CLI to find a PR for the given branch.
func FindPRForBranch(branch string) (int, string) {
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

// GetCIStatus checks CI status for a PR number.
func GetCIStatus(prNumber int) string {
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

// GetReviewCommentCount returns the number of PR review comments.
func GetReviewCommentCount(prNumber int) int {
	cmd := exec.Command("gh", "pr", "view", fmt.Sprintf("%d", prNumber), "--json", "comments", "--jq", ".comments | length")
	out, err := cmd.Output()
	if err != nil {
		return 0
	}
	count, _ := strconv.Atoi(strings.TrimSpace(string(out)))
	return count
}

// FetchFailingCILogs fetches failing CI logs for a PR.
func FetchFailingCILogs(prNumber int) (string, error) {
	cmd := exec.Command("gh", "pr", "checks", fmt.Sprintf("%d", prNumber), "--json", "name,conclusion,detailsUrl")
	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("failed to get checks: %w", err)
	}
	var checks []struct {
		Name       string `json:"name"`
		Conclusion string `json:"conclusion"`
	}
	if err := json.Unmarshal(out, &checks); err != nil {
		return "", err
	}
	var logs strings.Builder
	for _, check := range checks {
		if check.Conclusion == "failure" {
			logs.WriteString(fmt.Sprintf("### %s (FAILED)\n", check.Name))
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

// FetchPRComments fetches review comments for a PR.
func FetchPRComments(prNumber int) ([]PRComment, error) {
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
	var comments []PRComment
	for _, r := range raw {
		comments = append(comments, PRComment{
			File:   r.Path,
			Line:   r.Line,
			Author: r.User.Login,
			Body:   r.Body,
		})
	}
	return comments, nil
}

// FormatPRComments formats PR comments grouped by file.
func FormatPRComments(comments []PRComment) string {
	byFile := make(map[string][]PRComment)
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

// CreatePR pushes the branch and creates a PR via gh CLI.
func CreatePR(gitPath, branch string) (string, error) {
	pushCmd := exec.Command("git", "-C", gitPath, "push", "-u", "origin", branch)
	if err := pushCmd.Run(); err != nil {
		return "", fmt.Errorf("push failed: %w", err)
	}
	prCmd := exec.Command("gh", "pr", "create",
		"--head", branch,
		"--title", branch,
		"--body", fmt.Sprintf("Auto-created from `tsp serve`\n\nBranch: %s", branch),
	)
	prCmd.Dir = gitPath
	out, err := prCmd.Output()
	if err != nil {
		return "", fmt.Errorf("PR creation failed: %w", err)
	}
	return strings.TrimSpace(string(out)), nil
}

// MergePR merges a PR by number.
func MergePR(prNumber int, gitPath string) error {
	cmd := exec.Command("gh", "pr", "merge", fmt.Sprintf("%d", prNumber), "--merge")
	cmd.Dir = gitPath
	return cmd.Run()
}

// EnrichWithPRData populates PR info for a session.
func EnrichWithPRData(s *Session) {
	if s.PR != nil && s.PR.Number > 0 {
		return
	}
	prNum, prURL := FindPRForBranch(s.Branch)
	if prNum > 0 {
		s.PR = &PRInfo{
			Number:      prNum,
			URL:         prURL,
			CIStatus:    GetCIStatus(prNum),
			ReviewCount: GetReviewCommentCount(prNum),
		}
	}
}
```

**Step 4: Run tests to verify they pass**

Run: `cd /Users/matteohertel/work/code/sandbox/tmux-super-powers && go test ./internal/service/ -run "TestParseDiffStat|TestFormatPRComments" -v`
Expected: PASS

**Step 5: Commit**

```bash
git add internal/service/git.go internal/service/git_test.go
git commit -m "feat: add service layer git/PR operations"
```

---

### Task 5: Create service layer — status inference

Extract status inference from `dash_helpers.go` into the service layer.

**Files:**
- Create: `internal/service/status.go`
- Create: `internal/service/status_test.go`

**Step 1: Write the failing tests**

Create `internal/service/status_test.go`:

```go
package service

import (
	"testing"
	"time"
)

func TestInferStatus(t *testing.T) {
	now := time.Now()
	errorPatterns := []string{"FAIL", "panic:"}
	promptPattern := `\$\s*$`

	tests := []struct {
		name        string
		prev        string
		current     string
		lastChanged time.Time
		want        string
	}{
		{"error pattern", "foo", "FAIL: test", now, "error"},
		{"content changed", "old", "new", now, "active"},
		{"idle <30s", "same", "same", now.Add(-10 * time.Second), "active"},
		{"idle >30s", "same", "same", now.Add(-40 * time.Second), "idle"},
		{"done >60s with prompt", "same", "same\n$ ", now.Add(-90 * time.Second), "done"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := InferStatus(tt.prev, tt.current, tt.lastChanged, now, errorPatterns, promptPattern)
			if got != tt.want {
				t.Errorf("InferStatus() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestStatusIcon(t *testing.T) {
	if StatusIcon("active") != "●" {
		t.Error("expected ● for active")
	}
	if StatusIcon("done") != "✓" {
		t.Error("expected ✓ for done")
	}
}

func TestFormatTimeSince(t *testing.T) {
	now := time.Now()
	got := FormatTimeSince(now.Add(-30*time.Second), now)
	if got != "30s ago" {
		t.Errorf("expected '30s ago', got %q", got)
	}
}
```

**Step 2: Run tests to verify they fail**

Run: `cd /Users/matteohertel/work/code/sandbox/tmux-super-powers && go test ./internal/service/ -run "TestInferStatus|TestStatusIcon|TestFormatTimeSince" -v`
Expected: FAIL — functions don't exist

**Step 3: Write minimal implementation**

Create `internal/service/status.go`:

```go
package service

import (
	"fmt"
	"regexp"
	"strings"
	"time"
)

// InferStatus determines session status from pane content changes.
func InferStatus(prev, current string, lastChanged, now time.Time, errorPatterns []string, promptPattern string) string {
	for _, pattern := range errorPatterns {
		if strings.Contains(current, pattern) {
			return "error"
		}
	}
	if prev != current {
		return "active"
	}
	elapsed := now.Sub(lastChanged)
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
	if elapsed > 30*time.Second {
		return "idle"
	}
	return "active"
}

// StatusIcon returns a Unicode icon for a status string.
func StatusIcon(status string) string {
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

// FormatTimeSince formats a duration since a time as a human-readable string.
func FormatTimeSince(since, now time.Time) string {
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

Run: `cd /Users/matteohertel/work/code/sandbox/tmux-super-powers && go test ./internal/service/ -run "TestInferStatus|TestStatusIcon|TestFormatTimeSince" -v`
Expected: PASS

**Step 5: Commit**

```bash
git add internal/service/status.go internal/service/status_test.go
git commit -m "feat: add service layer status inference"
```

---

### Task 6: Create service layer — monitoring loop

The monitor continuously polls all sessions and maintains their state.

**Files:**
- Create: `internal/service/monitor.go`
- Create: `internal/service/monitor_test.go`

**Step 1: Write the failing test**

Create `internal/service/monitor_test.go`:

```go
package service

import (
	"testing"
)

func TestNewMonitor(t *testing.T) {
	m := NewMonitor(500, []string{"FAIL"}, `\$\s*$`)
	if m == nil {
		t.Fatal("expected non-nil monitor")
	}
	if m.refreshMs != 500 {
		t.Errorf("expected refreshMs 500, got %d", m.refreshMs)
	}
}

func TestMonitorSnapshot(t *testing.T) {
	m := NewMonitor(500, []string{"FAIL"}, `\$\s*$`)
	// Snapshot of empty monitor should return empty slice
	sessions := m.Snapshot()
	if len(sessions) != 0 {
		t.Errorf("expected empty snapshot, got %d sessions", len(sessions))
	}
}
```

**Step 2: Run test to verify it fails**

Run: `cd /Users/matteohertel/work/code/sandbox/tmux-super-powers && go test ./internal/service/ -run "TestNewMonitor|TestMonitorSnapshot" -v`
Expected: FAIL

**Step 3: Write minimal implementation**

Create `internal/service/monitor.go`:

```go
package service

import (
	"sync"
	"time"
)

// Monitor continuously polls tmux sessions and maintains their state.
type Monitor struct {
	mu            sync.RWMutex
	sessions      []Session
	refreshMs     int
	errorPatterns []string
	promptPattern string
	subscribers   []chan []Session
	subMu         sync.Mutex
	stopCh        chan struct{}
}

// NewMonitor creates a new session monitor.
func NewMonitor(refreshMs int, errorPatterns []string, promptPattern string) *Monitor {
	return &Monitor{
		refreshMs:     refreshMs,
		errorPatterns: errorPatterns,
		promptPattern: promptPattern,
		stopCh:        make(chan struct{}),
	}
}

// Start begins the monitoring loop in a goroutine.
func (m *Monitor) Start() {
	go m.loop()
}

// Stop halts the monitoring loop.
func (m *Monitor) Stop() {
	close(m.stopCh)
}

// Snapshot returns a copy of the current session states.
func (m *Monitor) Snapshot() []Session {
	m.mu.RLock()
	defer m.mu.RUnlock()
	cp := make([]Session, len(m.sessions))
	copy(cp, m.sessions)
	return cp
}

// FindSession returns a session by name, or nil if not found.
func (m *Monitor) FindSession(name string) *Session {
	m.mu.RLock()
	defer m.mu.RUnlock()
	for i := range m.sessions {
		if m.sessions[i].Name == name {
			s := m.sessions[i]
			return &s
		}
	}
	return nil
}

// Subscribe returns a channel that receives session snapshots on every refresh.
func (m *Monitor) Subscribe() chan []Session {
	ch := make(chan []Session, 1)
	m.subMu.Lock()
	m.subscribers = append(m.subscribers, ch)
	m.subMu.Unlock()
	return ch
}

// Unsubscribe removes a subscriber channel.
func (m *Monitor) Unsubscribe(ch chan []Session) {
	m.subMu.Lock()
	defer m.subMu.Unlock()
	for i, sub := range m.subscribers {
		if sub == ch {
			m.subscribers = append(m.subscribers[:i], m.subscribers[i+1:]...)
			close(ch)
			return
		}
	}
}

func (m *Monitor) loop() {
	ticker := time.NewTicker(time.Duration(m.refreshMs) * time.Millisecond)
	defer ticker.Stop()

	// Initial poll
	m.poll()

	for {
		select {
		case <-m.stopCh:
			return
		case <-ticker.C:
			m.poll()
		}
	}
}

func (m *Monitor) poll() {
	names, err := ListSessions()
	if err != nil || len(names) == 0 {
		m.mu.Lock()
		m.sessions = nil
		m.mu.Unlock()
		m.notify()
		return
	}

	now := time.Now()

	m.mu.Lock()
	// Build lookup of existing sessions
	existing := make(map[string]*Session)
	for i := range m.sessions {
		existing[m.sessions[i].Name] = &m.sessions[i]
	}

	var updated []Session
	for _, name := range names {
		paneCount := GetPaneCount(name)
		var panes []Pane
		var primaryContent string

		for p := 0; p < paneCount; p++ {
			process := GetPaneProcess(name, p)
			pType := PaneTypeFromProcess(process)
			pane := Pane{
				Index:   p,
				Type:    pType,
				Process: process,
			}
			// Only capture content for non-editor panes
			if pType != "editor" {
				content := CapturePaneContent(name, p)
				pane.Content = content
				if primaryContent == "" {
					primaryContent = content
				}
			}
			panes = append(panes, pane)
		}

		s := Session{
			Name:        name,
			Panes:       panes,
			LastChanged: now,
		}

		// Carry over state from existing session
		if prev, ok := existing[name]; ok {
			s.LastChanged = prev.LastChanged
			s.prevContent = prev.prevContent
			s.Branch = prev.Branch
			s.IsWorktree = prev.IsWorktree
			s.IsGitRepo = prev.IsGitRepo
			s.GitPath = prev.GitPath
			s.worktreePath = prev.worktreePath
			s.Diff = prev.Diff
			s.PR = prev.PR

			// Check if content changed
			if primaryContent != prev.prevContent {
				s.LastChanged = now
			}
			s.prevContent = primaryContent
		} else {
			// New session — detect git info
			gitPath, branch := DetectSessionGitInfo(name)
			if gitPath != "" {
				s.IsGitRepo = true
				s.GitPath = gitPath
				s.Branch = branch
			}
			s.prevContent = primaryContent
		}

		// Infer status from primary (agent/shell) pane
		if prev, ok := existing[name]; ok {
			s.Status = InferStatus(prev.prevContent, primaryContent, s.LastChanged, now, m.errorPatterns, m.promptPattern)
		} else {
			s.Status = "active"
		}

		// Set status on agent/shell panes
		for i := range s.Panes {
			if s.Panes[i].Type == "agent" || s.Panes[i].Type == "shell" || s.Panes[i].Type == "process" {
				s.Panes[i].Status = s.Status
			}
		}

		updated = append(updated, s)
	}

	m.sessions = updated
	m.mu.Unlock()

	m.notify()
}

func (m *Monitor) notify() {
	snapshot := m.Snapshot()
	m.subMu.Lock()
	defer m.subMu.Unlock()
	for _, ch := range m.subscribers {
		select {
		case ch <- snapshot:
		default:
			// Drop if subscriber is slow
		}
	}
}
```

**Step 4: Run tests to verify they pass**

Run: `cd /Users/matteohertel/work/code/sandbox/tmux-super-powers && go test ./internal/service/ -run "TestNewMonitor|TestMonitorSnapshot" -v`
Expected: PASS

**Step 5: Commit**

```bash
git add internal/service/monitor.go internal/service/monitor_test.go
git commit -m "feat: add session monitor with pub/sub for WebSocket streaming"
```

---

### Task 7: Create HTTP server with REST endpoints

**Files:**
- Create: `internal/server/server.go`
- Create: `internal/server/handlers.go`
- Create: `internal/server/handlers_test.go`

**Step 1: Write the failing test**

Create `internal/server/handlers_test.go`:

```go
package server

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestHealthEndpoint(t *testing.T) {
	srv := &Server{}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/health", srv.handleHealth)

	req := httptest.NewRequest("GET", "/api/health", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK && w.Code != http.StatusServiceUnavailable {
		t.Errorf("expected 200 or 503, got %d", w.Code)
	}
}

func TestConfigEndpoint(t *testing.T) {
	srv := &Server{}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/config", srv.handleConfig)

	req := httptest.NewRequest("GET", "/api/config", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	// Should return 200 even with nil config (returns default)
	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", w.Code)
	}
}
```

**Step 2: Run tests to verify they fail**

Run: `cd /Users/matteohertel/work/code/sandbox/tmux-super-powers && go test ./internal/server/ -v`
Expected: FAIL — package doesn't exist

**Step 3: Write implementation**

Create `internal/server/server.go`:

```go
package server

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/gorilla/websocket"
	"github.com/matteo-hertel/tmux-super-powers/config"
	"github.com/matteo-hertel/tmux-super-powers/internal/service"
)

// Server is the HTTP/WebSocket API server.
type Server struct {
	cfg      *config.Config
	monitor  *service.Monitor
	upgrader websocket.Upgrader
	httpSrv  *http.Server
}

// New creates a new API server.
func New(cfg *config.Config) *Server {
	return &Server{
		cfg: cfg,
		monitor: service.NewMonitor(
			cfg.Serve.RefreshMs,
			cfg.Dash.ErrorPatterns,
			cfg.Dash.PromptPattern,
		),
		upgrader: websocket.Upgrader{
			CheckOrigin: func(r *http.Request) bool { return true },
		},
	}
}

// Start starts the monitor and HTTP server.
func (s *Server) Start(bind string, port int) error {
	s.monitor.Start()

	mux := http.NewServeMux()
	s.registerRoutes(mux)

	addr := fmt.Sprintf("%s:%d", bind, port)
	s.httpSrv = &http.Server{
		Addr:         addr,
		Handler:      withLogging(withCORS(mux)),
		ReadTimeout:  10 * time.Second,
		WriteTimeout: 10 * time.Second,
	}

	log.Printf("tsp serve listening on %s", addr)
	return s.httpSrv.ListenAndServe()
}

// Stop gracefully shuts down the server.
func (s *Server) Stop() error {
	s.monitor.Stop()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	return s.httpSrv.Shutdown(ctx)
}

func (s *Server) registerRoutes(mux *http.ServeMux) {
	// System
	mux.HandleFunc("GET /api/health", s.handleHealth)
	mux.HandleFunc("GET /api/config", s.handleConfig)

	// Sessions
	mux.HandleFunc("GET /api/sessions", s.handleListSessions)
	mux.HandleFunc("GET /api/sessions/{name}", s.handleGetSession)
	mux.HandleFunc("POST /api/sessions", s.handleCreateSession)
	mux.HandleFunc("DELETE /api/sessions/{name}", s.handleDeleteSession)
	mux.HandleFunc("POST /api/sessions/{name}/send", s.handleSendToPane)

	// Spawn
	mux.HandleFunc("POST /api/spawn", s.handleSpawn)

	// PR/CI
	mux.HandleFunc("GET /api/sessions/{name}/pr", s.handleGetPR)
	mux.HandleFunc("POST /api/sessions/{name}/pr", s.handleCreatePR)
	mux.HandleFunc("POST /api/sessions/{name}/fix-ci", s.handleFixCI)
	mux.HandleFunc("POST /api/sessions/{name}/fix-reviews", s.handleFixReviews)
	mux.HandleFunc("POST /api/sessions/{name}/merge", s.handleMerge)

	// WebSocket
	mux.HandleFunc("GET /api/ws", s.handleWebSocket)
}

// DetectTailscaleIP finds the Tailscale interface IP (100.x.x.x CGNAT range).
func DetectTailscaleIP() string {
	ifaces, err := net.Interfaces()
	if err != nil {
		return ""
	}
	for _, iface := range ifaces {
		addrs, err := iface.Addrs()
		if err != nil {
			continue
		}
		for _, addr := range addrs {
			var ip net.IP
			switch v := addr.(type) {
			case *net.IPNet:
				ip = v.IP
			case *net.IPAddr:
				ip = v.IP
			}
			if ip == nil || ip.To4() == nil {
				continue
			}
			// Tailscale CGNAT range: 100.64.0.0/10
			if ip.To4()[0] == 100 && ip.To4()[1] >= 64 && ip.To4()[1] <= 127 {
				return ip.String()
			}
		}
	}
	return ""
}

// Middleware: CORS
func withCORS(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, DELETE, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type")
		if r.Method == "OPTIONS" {
			w.WriteHeader(http.StatusOK)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// Middleware: request logging
func withLogging(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		next.ServeHTTP(w, r)
		log.Printf("%s %s %s", r.Method, r.URL.Path, time.Since(start))
	})
}

// writeJSON writes a JSON response.
func writeJSON(w http.ResponseWriter, status int, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}

// writeError writes a JSON error response.
func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}

// ResolveBindAddress determines the bind address.
// Priority: explicit bind flag > Tailscale IP > localhost
func ResolveBindAddress(bind string) string {
	if bind != "" {
		return bind
	}
	if tsIP := DetectTailscaleIP(); tsIP != "" {
		log.Printf("Tailscale detected, binding to %s", tsIP)
		return tsIP
	}
	log.Println("WARNING: Tailscale not detected, binding to 127.0.0.1")
	return "127.0.0.1"
}

// ParseSessionName extracts the session name, handling URL-encoded values.
func ParseSessionName(r *http.Request) string {
	name := r.PathValue("name")
	return strings.ReplaceAll(name, "%20", " ")
}
```

Create `internal/server/handlers.go`:

```go
package server

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/matteo-hertel/tmux-super-powers/internal/service"
)

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	tmuxOK := service.TmuxRunning()
	ghOK := service.GhAvailable()
	status := http.StatusOK
	if !tmuxOK {
		status = http.StatusServiceUnavailable
	}
	writeJSON(w, status, map[string]interface{}{
		"tmux": tmuxOK,
		"gh":   ghOK,
		"time": time.Now().Format(time.RFC3339),
	})
}

func (s *Server) handleConfig(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, s.cfg)
}

func (s *Server) handleListSessions(w http.ResponseWriter, r *http.Request) {
	if !service.TmuxRunning() {
		writeError(w, http.StatusServiceUnavailable, "tmux is not running")
		return
	}
	sessions := s.monitor.Snapshot()
	writeJSON(w, http.StatusOK, map[string]interface{}{"sessions": sessions})
}

func (s *Server) handleGetSession(w http.ResponseWriter, r *http.Request) {
	name := ParseSessionName(r)
	session := s.monitor.FindSession(name)
	if session == nil {
		writeError(w, http.StatusNotFound, "session not found")
		return
	}
	// Enrich with PR data on demand
	if session.IsGitRepo && session.Branch != "" {
		service.EnrichWithPRData(session)
	}
	// Load diff on demand
	if session.IsGitRepo && session.Diff == nil {
		files, ins, del, _ := service.GetDiffStat(session.GitPath)
		if files > 0 {
			session.Diff = &service.DiffStat{Files: files, Insertions: ins, Deletions: del}
		}
	}
	writeJSON(w, http.StatusOK, session)
}

func (s *Server) handleCreateSession(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Name     string `json:"name"`
		Dir      string `json:"dir"`
		LeftCmd  string `json:"leftCmd"`
		RightCmd string `json:"rightCmd"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	if req.Name == "" || req.Dir == "" {
		writeError(w, http.StatusBadRequest, "name and dir are required")
		return
	}
	if err := service.CreateSession(req.Name, req.Dir, req.LeftCmd, req.RightCmd); err != nil {
		writeError(w, http.StatusConflict, err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, map[string]string{"status": "created", "name": req.Name})
}

func (s *Server) handleDeleteSession(w http.ResponseWriter, r *http.Request) {
	name := ParseSessionName(r)
	session := s.monitor.FindSession(name)
	if session == nil {
		writeError(w, http.StatusNotFound, "session not found")
		return
	}
	var req struct {
		CleanupWorktree bool `json:"cleanupWorktree"`
	}
	json.NewDecoder(r.Body).Decode(&req) // optional body

	err := service.KillSession(name, req.CleanupWorktree && session.IsWorktree, session.worktreePath, session.Branch)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
}

func (s *Server) handleSendToPane(w http.ResponseWriter, r *http.Request) {
	name := ParseSessionName(r)
	session := s.monitor.FindSession(name)
	if session == nil {
		writeError(w, http.StatusNotFound, "session not found")
		return
	}
	var req struct {
		Pane int    `json:"pane"`
		Text string `json:"text"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	if req.Text == "" {
		writeError(w, http.StatusBadRequest, "text is required")
		return
	}
	if err := service.SendToPane(name, req.Pane, req.Text); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "sent"})
}

func (s *Server) handleSpawn(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Tasks     []string `json:"tasks"`
		Base      string   `json:"base"`
		NoInstall bool     `json:"noInstall"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	if len(req.Tasks) == 0 {
		writeError(w, http.StatusBadRequest, "tasks array is required")
		return
	}
	results, err := service.SpawnAgents(req.Tasks, req.Base, req.NoInstall, s.cfg)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, map[string]interface{}{"results": results})
}

func (s *Server) handleGetPR(w http.ResponseWriter, r *http.Request) {
	name := ParseSessionName(r)
	session := s.monitor.FindSession(name)
	if session == nil {
		writeError(w, http.StatusNotFound, "session not found")
		return
	}
	if !service.GhAvailable() {
		writeError(w, http.StatusNotImplemented, "gh CLI not installed")
		return
	}
	if !session.IsGitRepo || session.Branch == "" {
		writeError(w, http.StatusBadRequest, "session is not a git repo or has no branch")
		return
	}
	service.EnrichWithPRData(session)
	if session.PR == nil {
		writeJSON(w, http.StatusOK, map[string]interface{}{"pr": nil})
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{"pr": session.PR})
}

func (s *Server) handleCreatePR(w http.ResponseWriter, r *http.Request) {
	name := ParseSessionName(r)
	session := s.monitor.FindSession(name)
	if session == nil {
		writeError(w, http.StatusNotFound, "session not found")
		return
	}
	if !service.GhAvailable() {
		writeError(w, http.StatusNotImplemented, "gh CLI not installed")
		return
	}
	if !session.IsGitRepo {
		writeError(w, http.StatusBadRequest, "session is not a git repo")
		return
	}
	url, err := service.CreatePR(session.GitPath, session.Branch)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, map[string]string{"url": url})
}

func (s *Server) handleFixCI(w http.ResponseWriter, r *http.Request) {
	name := ParseSessionName(r)
	session := s.monitor.FindSession(name)
	if session == nil {
		writeError(w, http.StatusNotFound, "session not found")
		return
	}
	if !service.GhAvailable() {
		writeError(w, http.StatusNotImplemented, "gh CLI not installed")
		return
	}
	service.EnrichWithPRData(session)
	if session.PR == nil || session.PR.Number == 0 {
		writeError(w, http.StatusBadRequest, "no PR found — create one first")
		return
	}
	logs, err := service.FetchFailingCILogs(session.PR.Number)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	prompt := "The CI pipeline failed. Here are the failing logs:\n\n" + logs + "\n\nPlease fix the issues and push."
	if len(prompt) > 4000 {
		prompt = prompt[:4000] + "\n\n[truncated]"
	}
	// Send to agent pane (pane 1 by default for worktree sessions)
	agentPane := 1
	for _, p := range session.Panes {
		if p.Type == "agent" {
			agentPane = p.Index
			break
		}
	}
	if err := service.SendToPane(name, agentPane, prompt); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "fix-ci prompt sent"})
}

func (s *Server) handleFixReviews(w http.ResponseWriter, r *http.Request) {
	name := ParseSessionName(r)
	session := s.monitor.FindSession(name)
	if session == nil {
		writeError(w, http.StatusNotFound, "session not found")
		return
	}
	if !service.GhAvailable() {
		writeError(w, http.StatusNotImplemented, "gh CLI not installed")
		return
	}
	service.EnrichWithPRData(session)
	if session.PR == nil || session.PR.Number == 0 {
		writeError(w, http.StatusBadRequest, "no PR found — create one first")
		return
	}
	comments, err := service.FetchPRComments(session.PR.Number)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if len(comments) == 0 {
		writeError(w, http.StatusBadRequest, "no review comments found")
		return
	}
	formatted := service.FormatPRComments(comments)
	prompt := "Please address these PR review comments:\n\n" + formatted

	agentPane := 1
	for _, p := range session.Panes {
		if p.Type == "agent" {
			agentPane = p.Index
			break
		}
	}
	if err := service.SendToPane(name, agentPane, prompt); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "review comments sent"})
}

func (s *Server) handleMerge(w http.ResponseWriter, r *http.Request) {
	name := ParseSessionName(r)
	session := s.monitor.FindSession(name)
	if session == nil {
		writeError(w, http.StatusNotFound, "session not found")
		return
	}
	if !service.GhAvailable() {
		writeError(w, http.StatusNotImplemented, "gh CLI not installed")
		return
	}
	service.EnrichWithPRData(session)
	if session.PR == nil || session.PR.Number == 0 {
		writeError(w, http.StatusBadRequest, "no PR found — create one first")
		return
	}
	if err := service.MergePR(session.PR.Number, session.GitPath); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "merged"})
}

func (s *Server) handleWebSocket(w http.ResponseWriter, r *http.Request) {
	conn, err := s.upgrader.Upgrade(w, r, nil)
	if err != nil {
		return
	}
	defer conn.Close()

	ch := s.monitor.Subscribe()
	defer s.monitor.Unsubscribe(ch)

	// Send initial snapshot
	if data, err := service.MarshalSessions(s.monitor.Snapshot()); err == nil {
		conn.WriteMessage(websocket.TextMessage, data)
	}

	// Read pump (for ping/pong and close detection)
	done := make(chan struct{})
	go func() {
		defer close(done)
		for {
			_, _, err := conn.ReadMessage()
			if err != nil {
				return
			}
		}
	}()

	for {
		select {
		case sessions, ok := <-ch:
			if !ok {
				return
			}
			data, err := service.MarshalSessions(sessions)
			if err != nil {
				continue
			}
			if err := conn.WriteMessage(websocket.TextMessage, data); err != nil {
				return
			}
		case <-done:
			return
		}
	}
}
```

**Step 4: Run tests to verify they pass**

Run: `cd /Users/matteohertel/work/code/sandbox/tmux-super-powers && go test ./internal/server/ -v`
Expected: PASS

**Step 5: Commit**

```bash
git add internal/server/server.go internal/server/handlers.go internal/server/handlers_test.go
git commit -m "feat: add HTTP/WebSocket API server with all endpoints"
```

---

### Task 8: Create spawn service function

Extract spawn logic from the cobra command into the service layer.

**Files:**
- Create: `internal/service/spawn.go`
- Create: `internal/service/spawn_test.go`

**Step 1: Write the failing test**

Create `internal/service/spawn_test.go`:

```go
package service

import (
	"testing"
)

func TestTaskToBranch(t *testing.T) {
	tests := []struct {
		task string
		want string
	}{
		{"fix the auth bug", "spawn/fix-the-auth-bug"},
		{"", "spawn/task"},
		{"Add Dark Mode!!!", "spawn/add-dark-mode"},
		{"a very long task name that exceeds the maximum character limit for branch names in git", "spawn/a-very-long-task-name-that-exceeds-the-maximum"},
	}
	for _, tt := range tests {
		got := TaskToBranch(tt.task)
		if got != tt.want {
			t.Errorf("TaskToBranch(%q) = %q, want %q", tt.task, got, tt.want)
		}
	}
}
```

**Step 2: Run test to verify it fails**

Run: `cd /Users/matteohertel/work/code/sandbox/tmux-super-powers && go test ./internal/service/ -run TestTaskToBranch -v`
Expected: FAIL

**Step 3: Write minimal implementation**

Create `internal/service/spawn.go`:

```go
package service

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/matteo-hertel/tmux-super-powers/config"
	"github.com/matteo-hertel/tmux-super-powers/internal/pathutil"
	tmuxpkg "github.com/matteo-hertel/tmux-super-powers/internal/tmux"
)

var nonAlphaNum = regexp.MustCompile(`[^a-z0-9-]+`)
var multiDash = regexp.MustCompile(`-{2,}`)

// TaskToBranch converts a task description to a git branch name.
func TaskToBranch(task string) string {
	if task == "" {
		return "spawn/task"
	}
	name := strings.ToLower(task)
	name = nonAlphaNum.ReplaceAllString(name, "-")
	name = multiDash.ReplaceAllString(name, "-")
	name = strings.Trim(name, "-")
	if len(name) > 50 {
		name = name[:50]
		if idx := strings.LastIndex(name, "-"); idx > 0 {
			name = name[:idx]
		}
		name = strings.TrimRight(name, "-")
	}
	return "spawn/" + name
}

// SpawnResult holds the result of spawning a single agent.
type SpawnResult struct {
	Task    string `json:"task"`
	Branch  string `json:"branch"`
	Session string `json:"session"`
	Status  string `json:"status"` // "ok" or "error"
	Error   string `json:"error,omitempty"`
}

// SpawnAgents deploys agents with tasks into worktrees.
func SpawnAgents(tasks []string, baseBranch string, noInstall bool, cfg *config.Config) ([]SpawnResult, error) {
	repoRoot, err := getRepoRoot()
	if err != nil {
		return nil, fmt.Errorf("not a git repository: %w", err)
	}
	repoName := filepath.Base(repoRoot)

	if baseBranch == "" {
		baseBranch, err = getCurrentBranch()
		if err != nil {
			return nil, fmt.Errorf("cannot determine current branch: %w", err)
		}
	}

	worktreeBase := pathutil.ExpandPath(cfg.Spawn.WorktreeBase)
	agentCmd := cfg.Spawn.AgentCommand

	var results []SpawnResult
	for _, task := range tasks {
		branch := TaskToBranch(task)
		branchShort := strings.TrimPrefix(branch, "spawn/")
		sessionName := tmuxpkg.SanitizeSessionName(fmt.Sprintf("%s-%s", repoName, branchShort))
		worktreePath := filepath.Join(worktreeBase, fmt.Sprintf("%s-%s", repoName, branchShort))

		result := SpawnResult{Task: task, Branch: branch, Session: sessionName}

		// Create branch
		if !branchExists(branch) {
			if err := createBranch(branch, baseBranch); err != nil {
				result.Status = "error"
				result.Error = fmt.Sprintf("branch creation failed: %v", err)
				results = append(results, result)
				continue
			}
		}

		// Create worktree
		if _, err := os.Stat(worktreePath); err != nil {
			if err := createWorktree(worktreePath, branch); err != nil {
				result.Status = "error"
				result.Error = fmt.Sprintf("worktree creation failed: %v", err)
				results = append(results, result)
				continue
			}
		}

		// Install dependencies
		if !noInstall {
			pm := detectPackageManager(repoRoot)
			if pm != "" {
				runPM(pm, worktreePath, repoRoot)
			}
		}

		// Create session
		if tmuxpkg.SessionExists(sessionName) {
			tmuxpkg.KillSession(sessionName)
		}
		tmuxpkg.CreateTwoPaneSession(sessionName, worktreePath, "nvim", agentCmd)

		// Send task prompt
		target := fmt.Sprintf("%s:0.1", sessionName)
		tmuxpkg.SendKeys(target, task)

		result.Status = "ok"
		results = append(results, result)
	}

	return results, nil
}

func getRepoRoot() (string, error) {
	cmd := exec.Command("git", "rev-parse", "--show-toplevel")
	out, err := cmd.Output()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}

func getCurrentBranch() (string, error) {
	cmd := exec.Command("git", "rev-parse", "--abbrev-ref", "HEAD")
	out, err := cmd.Output()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}

func branchExists(branch string) bool {
	cmd := exec.Command("git", "show-ref", "--verify", "--quiet", fmt.Sprintf("refs/heads/%s", branch))
	return cmd.Run() == nil
}

func createBranch(branch, from string) error {
	return exec.Command("git", "branch", branch, from).Run()
}

func createWorktree(path, branch string) error {
	return exec.Command("git", "worktree", "add", path, branch).Run()
}

func detectPackageManager(repoRoot string) string {
	lockFiles := []struct {
		file string
		pm   string
	}{
		{"bun.lockb", "bun"},
		{"bun.lock", "bun"},
		{"pnpm-lock.yaml", "pnpm"},
		{"yarn.lock", "yarn"},
		{"package-lock.json", "npm"},
	}
	for _, lf := range lockFiles {
		if _, err := os.Stat(filepath.Join(repoRoot, lf.file)); err == nil {
			return lf.pm
		}
	}
	if _, err := os.Stat(filepath.Join(repoRoot, "package.json")); err == nil {
		return "npm"
	}
	return ""
}

func runPM(pm, path, repoRoot string) {
	if pm == "yarn" {
		// Copy yarn cache for faster installs
		yarnDir := filepath.Join(path, ".yarn")
		os.MkdirAll(yarnDir, 0755)
		for _, name := range []string{"cache", "install-state.gz", "unplugged"} {
			src := filepath.Join(repoRoot, ".yarn", name)
			dst := filepath.Join(yarnDir, name)
			if _, err := os.Stat(src); err == nil {
				if _, err := os.Stat(dst); err != nil {
					exec.Command("cp", "-a", src, dst).Run()
				}
			}
		}
	}
	var cmd *exec.Cmd
	switch pm {
	case "yarn":
		cmd = exec.Command("yarn", "install")
	case "pnpm":
		cmd = exec.Command("pnpm", "install")
	case "bun":
		cmd = exec.Command("bun", "install")
	default:
		cmd = exec.Command("npm", "install")
	}
	cmd.Dir = path
	cmd.Run()
}
```

**Step 4: Run tests to verify they pass**

Run: `cd /Users/matteohertel/work/code/sandbox/tmux-super-powers && go test ./internal/service/ -run TestTaskToBranch -v`
Expected: PASS

**Step 5: Commit**

```bash
git add internal/service/spawn.go internal/service/spawn_test.go
git commit -m "feat: add spawn service with task-to-branch conversion"
```

---

### Task 9: Create `tsp serve` command

**Files:**
- Create: `internal/cmd/serve.go`
- Modify: `internal/cmd/root.go:28-48` (register serveCmd)

**Step 1: Write the serve command**

Create `internal/cmd/serve.go`:

```go
package cmd

import (
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"

	"github.com/matteo-hertel/tmux-super-powers/config"
	"github.com/matteo-hertel/tmux-super-powers/internal/server"
	"github.com/spf13/cobra"
)

var serveCmd = &cobra.Command{
	Use:   "serve",
	Short: "Start the API server for mobile app access",
	Long: `Start an HTTP/WebSocket API server that exposes tsp functionality.

The server binds to your Tailscale IP by default (100.x.x.x range).
If Tailscale is not detected, falls back to localhost.

Examples:
  tsp serve              # Start on default port (7777)
  tsp serve --port 8080  # Custom port
  tsp serve --bind 0.0.0.0  # Override bind address

Daemon management:
  tsp serve --install    # Install as launchd service (auto-start on login)
  tsp serve --uninstall  # Remove launchd service`,
	Run: func(cmd *cobra.Command, args []string) {
		install, _ := cmd.Flags().GetBool("install")
		uninstall, _ := cmd.Flags().GetBool("uninstall")

		if install {
			installLaunchd()
			return
		}
		if uninstall {
			uninstallLaunchd()
			return
		}

		port, _ := cmd.Flags().GetInt("port")
		bind, _ := cmd.Flags().GetString("bind")

		cfg, err := config.Load()
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error loading config: %v\n", err)
			os.Exit(1)
		}

		if port == 0 {
			port = cfg.Serve.Port
		}

		bindAddr := server.ResolveBindAddress(bind)

		srv := server.New(cfg)

		// Graceful shutdown
		sigCh := make(chan os.Signal, 1)
		signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
		go func() {
			<-sigCh
			log.Println("Shutting down...")
			srv.Stop()
		}()

		if err := srv.Start(bindAddr, port); err != nil {
			log.Fatalf("Server error: %v", err)
		}
	},
}

func init() {
	serveCmd.Flags().IntP("port", "p", 0, "Port to listen on (default: from config or 7777)")
	serveCmd.Flags().String("bind", "", "Address to bind to (default: Tailscale IP or 127.0.0.1)")
	serveCmd.Flags().Bool("install", false, "Install as launchd service")
	serveCmd.Flags().Bool("uninstall", false, "Remove launchd service")
}
```

**Step 2: Register the command**

Add to `internal/cmd/root.go` in the `init()` function:

```go
rootCmd.AddCommand(serveCmd)
```

**Step 3: Build to verify it compiles**

Run: `cd /Users/matteohertel/work/code/sandbox/tmux-super-powers && go build ./cmd/tsp`
Expected: compiles successfully

**Step 4: Verify the command shows in help**

Run: `cd /Users/matteohertel/work/code/sandbox/tmux-super-powers && ./tsp serve --help`
Expected: shows serve command help text

**Step 5: Commit**

```bash
git add internal/cmd/serve.go internal/cmd/root.go
git commit -m "feat: add tsp serve command for API server"
```

---

### Task 10: Add launchd integration

**Files:**
- Create: `internal/cmd/launchd.go`

**Step 1: Write the launchd helper**

Create `internal/cmd/launchd.go`:

```go
package cmd

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"text/template"
)

const plistLabel = "com.tsp.serve"

var plistTemplate = template.Must(template.New("plist").Parse(`<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
    <key>Label</key>
    <string>{{.Label}}</string>
    <key>ProgramArguments</key>
    <array>
        <string>{{.Binary}}</string>
        <string>serve</string>
    </array>
    <key>RunAtLoad</key>
    <true/>
    <key>KeepAlive</key>
    <true/>
    <key>StandardOutPath</key>
    <string>{{.LogDir}}/serve.log</string>
    <key>StandardErrorPath</key>
    <string>{{.LogDir}}/serve.log</string>
</dict>
</plist>
`))

func plistPath() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, "Library", "LaunchAgents", plistLabel+".plist")
}

func logDir() string {
	home, _ := os.UserHomeDir()
	dir := filepath.Join(home, ".tsp")
	os.MkdirAll(dir, 0755)
	return dir
}

func installLaunchd() {
	binary, err := exec.LookPath("tsp")
	if err != nil {
		// Fall back to current executable
		binary, _ = os.Executable()
	}

	data := struct {
		Label  string
		Binary string
		LogDir string
	}{
		Label:  plistLabel,
		Binary: binary,
		LogDir: logDir(),
	}

	path := plistPath()
	f, err := os.Create(path)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error creating plist: %v\n", err)
		os.Exit(1)
	}
	defer f.Close()

	if err := plistTemplate.Execute(f, data); err != nil {
		fmt.Fprintf(os.Stderr, "Error writing plist: %v\n", err)
		os.Exit(1)
	}

	if err := exec.Command("launchctl", "load", path).Run(); err != nil {
		fmt.Fprintf(os.Stderr, "Error loading service: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("Installed and started %s\n", plistLabel)
	fmt.Printf("Plist: %s\n", path)
	fmt.Printf("Logs:  %s/serve.log\n", logDir())
}

func uninstallLaunchd() {
	path := plistPath()
	exec.Command("launchctl", "unload", path).Run()

	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		fmt.Fprintf(os.Stderr, "Error removing plist: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("Uninstalled %s\n", plistLabel)
}
```

**Step 2: Build to verify it compiles**

Run: `cd /Users/matteohertel/work/code/sandbox/tmux-super-powers && go build ./cmd/tsp`
Expected: compiles successfully

**Step 3: Commit**

```bash
git add internal/cmd/launchd.go
git commit -m "feat: add launchd install/uninstall for tsp serve daemon"
```

---

### Task 11: Update CLAUDE.md with serve command

**Files:**
- Modify: `CLAUDE.md`

**Step 1: Add serve command to the command structure section**

Add `tsp serve` to the command list in CLAUDE.md:

```
- `tsp serve` - HTTP/WebSocket API server for mobile app access
```

Add a section about the API:

```markdown
### API Server
- `tsp serve` starts the API server on the Tailscale interface (port 7777 by default)
- `tsp serve --install` installs as a launchd daemon
- REST endpoints at `/api/sessions`, `/api/spawn`, `/api/sessions/{name}/pr`, etc.
- WebSocket at `/api/ws` for real-time session streaming
```

**Step 2: Commit**

```bash
git add CLAUDE.md
git commit -m "docs: add serve command to CLAUDE.md"
```

---

### Task 12: Run all tests and final build verification

**Step 1: Run all tests**

Run: `cd /Users/matteohertel/work/code/sandbox/tmux-super-powers && go test ./...`
Expected: all PASS

**Step 2: Build the binary**

Run: `cd /Users/matteohertel/work/code/sandbox/tmux-super-powers && go build -o tsp ./cmd/tsp`
Expected: compiles with no errors

**Step 3: Verify serve command**

Run: `cd /Users/matteohertel/work/code/sandbox/tmux-super-powers && ./tsp serve --help`
Expected: shows help text with port, bind, install, uninstall flags

**Step 4: Quick smoke test**

Run: `cd /Users/matteohertel/work/code/sandbox/tmux-super-powers && ./tsp serve --port 7777 &; sleep 2; curl -s http://127.0.0.1:7777/api/health | python3 -m json.tool; kill %1`
Expected: JSON response with tmux and gh status

**Step 5: Commit (if any fixes needed)**

```bash
git add -A
git commit -m "fix: address test/build issues from integration"
```
