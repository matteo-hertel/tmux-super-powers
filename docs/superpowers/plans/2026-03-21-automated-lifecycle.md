# Automated Post-Spawn Lifecycle Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Automate the agent lifecycle after spawn — auto-watch CI/reviews, auto-fix failures, auto-cleanup after merge — plus fix worktree deletion, speed up dep install, harden pane selection, and add config repair.

**Architecture:** Event bus replaces channel-based pub/sub. Monitor publishes typed events; Notifier, WebSocket, and a new Watcher service subscribe. Watcher runs a per-session state machine (working → done → pr_polling → watching → fixing → green → merged → cleanup). Incremental migration: old channel mechanism kept alongside bus until all consumers migrated.

**Tech Stack:** Go, cobra, bubbletea, gorilla/websocket, `os.Link()` for hardlinks, `gh` CLI for GitHub operations.

**Spec:** `docs/superpowers/specs/2026-03-21-automated-lifecycle-design.md`

---

## Phase 1: Independent Fixes (Tasks 1-4, parallelizable)

These tasks have no dependencies on each other. They can be dispatched to separate agents simultaneously.

---

### Task 1: Fix Worktree Deletion

**Files:**
- Modify: `internal/cmd/dash.go:461-475` (`discardWorktree()`)
- Modify: `internal/cmd/dash.go:408-459` (`mergeBranch()`)
- Modify: `internal/cmd/rm.go:74-85`
- Modify: `internal/cmd/gtwremove.go:201-277` (`removeSelectedWorktrees()`)

- [ ] **Step 1: Fix `discardWorktree()` in dash.go**

Open `internal/cmd/dash.go`. Find `discardWorktree()` (line 461). Replace:

```go
func (m *dashModel) discardWorktree() {
	if m.cursor >= len(m.sessions) {
		return
	}
	s := m.sessions[m.cursor]
	tmuxpkg.KillSession(s.name)
	if s.isWorktree {
		os.RemoveAll(s.worktreePath)
		exec.Command("git", "worktree", "remove", s.worktreePath, "--force").Run()
		exec.Command("git", "branch", "-D", s.branch).Run()
	}
	m.removeSession(m.cursor)
	m.statusMsg = fmt.Sprintf("Removed %s", s.name)
	m.mode = dashStatusMessage
}
```

With:

```go
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
```

- [ ] **Step 2: Fix `mergeBranch()` worktree cleanup in dash.go**

Find `mergeBranch()` (line 408). Around lines 431-433 inside the `if s.isWorktree` block, replace:

```go
		tmuxpkg.KillSession(s.name)
		exec.Command("git", "worktree", "remove", s.worktreePath, "--force").Run()
		exec.Command("git", "branch", "-D", s.branch).Run()
```

With:

```go
		tmuxpkg.KillSession(s.name)
		if err := exec.Command("git", "-C", s.gitPath, "worktree", "remove", s.worktreePath, "--force").Run(); err != nil {
			exec.Command("git", "-C", s.gitPath, "worktree", "prune").Run()
		}
		exec.Command("git", "-C", s.gitPath, "branch", "-D", s.branch).Run()
```

- [ ] **Step 3: Fix `rm.go` worktree cleanup**

Open `internal/cmd/rm.go`. Find lines 74-85 inside the `for _, session := range fm.toRemove` loop. Replace:

```go
				if wt, isWt := fm.wtMap[session]; isWt && !sessionsOnly {
					fmt.Printf("Removing worktree session: %s\n", session)
					tmuxpkg.KillSession(session)
					os.RemoveAll(wt.Path)
					exec.Command("git", "worktree", "remove", wt.Path, "--force").Run()
					exec.Command("git", "branch", "-D", wt.Branch).Run()
					fmt.Printf("  Worktree, branch, and session removed.\n")
```

With:

```go
				if wt, isWt := fm.wtMap[session]; isWt && !sessionsOnly {
					fmt.Printf("Removing worktree session: %s\n", session)
					tmuxpkg.KillSession(session)
					if err := exec.Command("git", "worktree", "remove", wt.Path, "--force").Run(); err != nil {
						fmt.Printf("  Warning: git worktree remove failed: %v\n", err)
						exec.Command("git", "worktree", "prune").Run()
					}
					if err := exec.Command("git", "branch", "-D", wt.Branch).Run(); err != nil {
						fmt.Printf("  Warning: branch delete failed: %v\n", err)
					}
					fmt.Printf("  Worktree, branch, and session removed.\n")
```

- [ ] **Step 4: Fix `gtwremove.go` — remove `os.RemoveAll` from phase 1**

Open `internal/cmd/gtwremove.go`. In `removeSelectedWorktrees()`, find the parallel phase (lines 220-246). Remove the `os.RemoveAll` block (lines 234-241):

```go
			if _, err := os.Stat(wt.Path); err == nil {
				out.WriteString(fmt.Sprintf("  Removing directory '%s'...\n", wt.Path))
				if err := os.RemoveAll(wt.Path); err != nil {
					out.WriteString(fmt.Sprintf("  Warning: Failed to remove directory: %v\n", err))
				} else {
					out.WriteString("  Directory removed successfully.\n")
				}
			}
```

Replace with:

```go
			// Directory removal handled by git worktree remove in phase 2
```

- [ ] **Step 5: Build and verify**

Run: `go build ./cmd/tsp`
Expected: Compiles successfully.

- [ ] **Step 6: Commit**

```bash
git add internal/cmd/dash.go internal/cmd/rm.go internal/cmd/gtwremove.go
git commit -m "fix: worktree deletion — use git worktree remove instead of os.RemoveAll

Remove os.RemoveAll calls that ran before git worktree remove, causing
git metadata to be left behind. Add -C flag for correct git context.
Add error handling with fallback to git worktree prune."
```

---

### Task 2: Dep Caching (Hardlink Copy)

**Files:**
- Modify: `internal/service/spawn.go`
- Create: `internal/service/spawn_test.go` (add test)

- [ ] **Step 1: Write the test**

Open `internal/service/spawn_test.go`. Add:

```go
func TestSpawnCopyNodeModules(t *testing.T) {
	// Create temp dirs simulating repo root and worktree
	repoRoot := t.TempDir()
	worktree := t.TempDir()

	// Create a fake node_modules in repo root
	nmDir := filepath.Join(repoRoot, "node_modules", "fake-pkg")
	if err := os.MkdirAll(nmDir, 0755); err != nil {
		t.Fatal(err)
	}
	testFile := filepath.Join(nmDir, "index.js")
	if err := os.WriteFile(testFile, []byte("module.exports = 42"), 0644); err != nil {
		t.Fatal(err)
	}

	// Create a symlink in node_modules
	symlinkPath := filepath.Join(repoRoot, "node_modules", ".bin")
	os.MkdirAll(filepath.Dir(symlinkPath), 0755)
	os.Symlink("../fake-pkg/index.js", symlinkPath)

	// Copy
	err := spawnCopyNodeModules(repoRoot, worktree)
	if err != nil {
		t.Fatalf("spawnCopyNodeModules failed: %v", err)
	}

	// Verify file was hardlinked (same inode)
	srcInfo, _ := os.Stat(testFile)
	dstFile := filepath.Join(worktree, "node_modules", "fake-pkg", "index.js")
	dstInfo, err := os.Stat(dstFile)
	if err != nil {
		t.Fatalf("destination file not found: %v", err)
	}
	if !os.SameFile(srcInfo, dstInfo) {
		t.Error("expected hardlink (same inode), got different files")
	}

	// Verify content
	content, _ := os.ReadFile(dstFile)
	if string(content) != "module.exports = 42" {
		t.Errorf("content mismatch: %s", content)
	}
}

func TestSpawnCopyNodeModulesNoNodeModules(t *testing.T) {
	repoRoot := t.TempDir()
	worktree := t.TempDir()

	// Should not error when node_modules doesn't exist
	err := spawnCopyNodeModules(repoRoot, worktree)
	if err != nil {
		t.Fatalf("expected nil error, got: %v", err)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/service/ -run TestSpawnCopyNodeModules -v`
Expected: FAIL — `spawnCopyNodeModules` not defined.

- [ ] **Step 3: Implement `spawnCopyNodeModules`**

Open `internal/service/spawn.go`. Add before `spawnRunPM()`:

```go
func spawnCopyNodeModules(repoRoot, worktreePath string) error {
	srcNM := filepath.Join(repoRoot, "node_modules")
	if _, err := os.Stat(srcNM); err != nil {
		return nil
	}
	dstNM := filepath.Join(worktreePath, "node_modules")
	return filepath.WalkDir(srcNM, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		rel, _ := filepath.Rel(srcNM, path)
		dst := filepath.Join(dstNM, rel)
		if d.IsDir() {
			return os.MkdirAll(dst, 0755)
		}
		if d.Type()&os.ModeSymlink != 0 {
			target, err := os.Readlink(path)
			if err != nil {
				return nil
			}
			return os.Symlink(target, dst)
		}
		if !d.Type().IsRegular() {
			return nil
		}
		return os.Link(path, dst)
	})
}
```

Add `"io/fs"` to imports.

- [ ] **Step 4: Wire into SpawnAgents**

In `SpawnAgents()`, add the hardlink copy call before `spawnRunPM()`. Find (around line 125-130):

```go
		if !noInstall {
			pm := spawnDetectPM(repoRoot)
			if pm != "" {
				spawnRunPM(pm, worktreePath, repoRoot)
			}
		}
```

Replace with:

```go
		if !noInstall {
			spawnCopyNodeModules(repoRoot, worktreePath)
			pm := spawnDetectPM(repoRoot)
			if pm != "" {
				spawnRunPM(pm, worktreePath, repoRoot)
			}
		}
```

- [ ] **Step 5: Run tests**

Run: `go test ./internal/service/ -run TestSpawnCopyNodeModules -v`
Expected: PASS

- [ ] **Step 6: Build**

Run: `go build ./cmd/tsp`
Expected: Compiles successfully.

- [ ] **Step 7: Commit**

```bash
git add internal/service/spawn.go internal/service/spawn_test.go
git commit -m "feat: hardlink-copy node_modules to worktrees before install

Uses filepath.WalkDir + os.Link for platform-agnostic hardlink copy.
Symlinks re-created as symlinks. Falls back silently if no node_modules."
```

---

### Task 3: Harden Pane Selection (Server Side)

**Files:**
- Modify: `internal/service/sessions.go:57-94`
- Modify: `internal/service/sessions_test.go`

- [ ] **Step 1: Replace existing tests**

Open `internal/service/sessions_test.go`. **Replace** the existing `TestPaneTypeFromProcess` function (do not add a duplicate) with:

```go
func TestPaneTypeFromProcess(t *testing.T) {
	tests := []struct {
		process  string
		expected string
	}{
		{"nvim", "editor"},
		{"vim", "editor"},
		{"emacs", "editor"},
		{"nano", "editor"},
		{"claude", "agent"},
		{"aider", "agent"},
		{"codex", "agent"},
		{"bash", "shell"},
		{"zsh", "shell"},
		{"fish", "shell"},
		{"sh", "shell"},
		{"", "shell"},
		{"2.1.71", "agent"},          // Claude Code version
		{"2.1.81", "agent"},          // newer version
		{"3.0.0", "agent"},           // future major
		{"node", "process"},          // node itself is not an agent
		{"python3", "process"},
		{"go", "process"},
		{"2.1.71-rc1", "process"},    // non-standard version — not agent
		{"v2.1.71", "process"},       // prefixed — not agent
	}
	for _, tt := range tests {
		t.Run(tt.process, func(t *testing.T) {
			result := PaneTypeFromProcess(tt.process)
			if result != tt.expected {
				t.Errorf("PaneTypeFromProcess(%q) = %q, want %q", tt.process, result, tt.expected)
			}
		})
	}
}
```

- [ ] **Step 2: Run tests to see which fail**

Run: `go test ./internal/service/ -run TestPaneTypeFromProcess -v`
Expected: "aider" and "codex" cases FAIL.

- [ ] **Step 3: Update PaneTypeFromProcess**

Open `internal/service/sessions.go`. Replace only the `PaneTypeFromProcess` function (keep `isClaudeVersion` unchanged):

```go
func PaneTypeFromProcess(process string) string {
	switch process {
	case "nvim", "vim", "emacs", "nano":
		return "editor"
	case "claude", "aider", "codex":
		return "agent"
	case "bash", "zsh", "fish", "sh", "":
		return "shell"
	default:
		if isClaudeVersion(process) {
			return "agent"
		}
		return "process"
	}
}
```

No change to `isClaudeVersion` — it already handles the core semver case. The main fix is adding "aider" and "codex" to the explicit list.

- [ ] **Step 4: Run tests**

Run: `go test ./internal/service/ -run TestPaneTypeFromProcess -v`
Expected: All PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/service/sessions.go internal/service/sessions_test.go
git commit -m "feat: add aider and codex to agent process detection"
```

---

### Task 4: Config Repair Command

**Files:**
- Modify: `config/config.go`
- Modify: `internal/cmd/config.go`
- Modify: `internal/cmd/root.go` (register subcommand)

- [ ] **Step 1: Add WatcherConfig to config.go**

Open `config/config.go`. Add after `ServeConfig`:

```go
type WatcherConfig struct {
	Enabled       bool `yaml:"enabled"`
	PollIntervalS int  `yaml:"poll_interval_s"`
	MaxCIRetries  int  `yaml:"max_ci_retries"`
	AutoCleanup   bool `yaml:"auto_cleanup"`
}
```

Add `Watcher WatcherConfig \`yaml:"watcher"\`` to the `Config` struct (after `Serve`).

Add defaults in `LoadFrom()` after the `// Serve defaults` section:

```go
	// Watcher defaults
	if cfg.Watcher.PollIntervalS == 0 {
		cfg.Watcher.PollIntervalS = 30
	}
	if cfg.Watcher.MaxCIRetries == 0 {
		cfg.Watcher.MaxCIRetries = 3
	}
```

Add to `defaultConfig()` return:

```go
		Watcher: WatcherConfig{
			Enabled:       true,
			PollIntervalS: 30,
			MaxCIRetries:  3,
			AutoCleanup:   true,
		},
```

- [ ] **Step 2: Add Repair function**

Add to `config/config.go`:

```go
// Repair compares a config against defaults and fills in missing fields.
// Returns the list of changes made and the updated config.
func Repair(cfg *Config) ([]string, *Config) {
	defaults := defaultConfig()
	var changes []string

	if len(cfg.Dash.ErrorPatterns) == 0 {
		cfg.Dash.ErrorPatterns = defaults.Dash.ErrorPatterns
		changes = append(changes, "dash.error_patterns: set to defaults")
	}
	if cfg.Dash.PromptPattern == "" {
		cfg.Dash.PromptPattern = defaults.Dash.PromptPattern
		changes = append(changes, "dash.prompt_pattern: set to default")
	}
	if len(cfg.Dash.InputPatterns) == 0 {
		cfg.Dash.InputPatterns = defaults.Dash.InputPatterns
		changes = append(changes, "dash.input_patterns: set to defaults")
	}
	if cfg.Dash.RefreshMs == 0 {
		cfg.Dash.RefreshMs = defaults.Dash.RefreshMs
		changes = append(changes, "dash.refresh_ms: set to 500")
	}
	if cfg.Spawn.AgentCommand == "" {
		cfg.Spawn.AgentCommand = defaults.Spawn.AgentCommand
		changes = append(changes, "spawn.agent_command: set to default")
	}
	if cfg.Spawn.WorktreeBase == "" {
		cfg.Spawn.WorktreeBase = defaults.Spawn.WorktreeBase
		changes = append(changes, "spawn.worktree_base: set to default")
	}
	if cfg.Serve.Port == 0 {
		cfg.Serve.Port = defaults.Serve.Port
		changes = append(changes, "serve.port: set to 7777")
	}
	if cfg.Serve.RefreshMs == 0 {
		cfg.Serve.RefreshMs = defaults.Serve.RefreshMs
		changes = append(changes, "serve.refresh_ms: set to default")
	}
	// Watcher section (new)
	if cfg.Watcher.PollIntervalS == 0 {
		cfg.Watcher = defaults.Watcher
		changes = append(changes, "watcher: added with defaults (enabled, 30s poll, 3 retries, auto-cleanup)")
	}

	return changes, cfg
}
```

- [ ] **Step 3: Add repair subcommand**

Open `internal/cmd/config.go`. Add after the existing `configCmd`:

```go
var configRepairCmd = &cobra.Command{
	Use:   "repair",
	Short: "Detect and fill missing config fields with defaults",
	Run: func(cmd *cobra.Command, args []string) {
		configPath := config.ConfigPath()

		cfg, err := config.Load()
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error loading config: %v\n", err)
			fmt.Fprintf(os.Stderr, "Consider backing up and resetting: cp %s %s.bak\n", configPath, configPath)
			os.Exit(1)
		}

		changes, updated := config.Repair(cfg)

		if len(changes) == 0 {
			fmt.Println("Config is up to date. No changes needed.")
			return
		}

		// Backup
		bakPath := configPath + ".bak"
		if data, err := os.ReadFile(configPath); err == nil {
			os.WriteFile(bakPath, data, 0644)
			fmt.Printf("Backup saved to %s\n", bakPath)
		}

		if err := config.Save(updated); err != nil {
			fmt.Fprintf(os.Stderr, "Error saving config: %v\n", err)
			os.Exit(1)
		}

		fmt.Printf("Config updated (%d changes):\n", len(changes))
		for _, c := range changes {
			fmt.Printf("  + %s\n", c)
		}
	},
}

func init() {
	configCmd.AddCommand(configRepairCmd)
}
```

- [ ] **Step 4: Build and verify**

Run: `go build ./cmd/tsp`
Expected: Compiles successfully.

- [ ] **Step 5: Commit**

```bash
git add config/config.go internal/cmd/config.go
git commit -m "feat: add tsp config repair and WatcherConfig

Detects missing config fields and fills them with defaults.
Adds watcher section for automated CI/review watching."
```

---

## Phase 2: Event Bus (Task 5, prerequisite for Phase 3)

---

### Task 5: Event Bus

**Files:**
- Create: `internal/service/events.go`
- Create: `internal/service/events_test.go`

- [ ] **Step 1: Write the test**

Create `internal/service/events_test.go`:

```go
package service

import (
	"sync"
	"testing"
	"time"
)

func TestBusPublishSubscribe(t *testing.T) {
	// Note: all event types are in the same package (service), no import needed
	bus := NewBus()
	var received []Event
	var mu sync.Mutex

	unsub := bus.Subscribe(func(e Event) {
		mu.Lock()
		received = append(received, e)
		mu.Unlock()
	})
	defer unsub()

	bus.Publish(StatusChangedEvent{Session: "test", From: "active", To: "done"})
	time.Sleep(50 * time.Millisecond)

	mu.Lock()
	defer mu.Unlock()
	if len(received) != 1 {
		t.Fatalf("expected 1 event, got %d", len(received))
	}
	e, ok := received[0].(StatusChangedEvent)
	if !ok {
		t.Fatalf("expected StatusChangedEvent, got %T", received[0])
	}
	if e.Session != "test" || e.From != "active" || e.To != "done" {
		t.Errorf("unexpected event: %+v", e)
	}
}

func TestBusUnsubscribe(t *testing.T) {
	bus := NewBus()
	count := 0
	var mu sync.Mutex

	unsub := bus.Subscribe(func(e Event) {
		mu.Lock()
		count++
		mu.Unlock()
	})

	bus.Publish(SessionCreatedEvent{Name: "a"})
	time.Sleep(50 * time.Millisecond)
	unsub()

	bus.Publish(SessionCreatedEvent{Name: "b"})
	time.Sleep(50 * time.Millisecond)

	mu.Lock()
	defer mu.Unlock()
	if count != 1 {
		t.Errorf("expected 1 event after unsubscribe, got %d", count)
	}
}

func TestBusHandlerPanicRecovery(t *testing.T) {
	bus := NewBus()
	var received []Event
	var mu sync.Mutex

	// Panicking subscriber
	bus.Subscribe(func(e Event) {
		panic("test panic")
	})
	// Normal subscriber
	bus.Subscribe(func(e Event) {
		mu.Lock()
		received = append(received, e)
		mu.Unlock()
	})

	bus.Publish(SessionCreatedEvent{Name: "test"})
	time.Sleep(50 * time.Millisecond)

	mu.Lock()
	defer mu.Unlock()
	if len(received) != 1 {
		t.Fatalf("expected normal subscriber to still receive event, got %d", len(received))
	}
}

func TestBusMultipleSubscribers(t *testing.T) {
	bus := NewBus()
	var count1, count2 int
	var mu sync.Mutex

	bus.Subscribe(func(e Event) {
		mu.Lock()
		count1++
		mu.Unlock()
	})
	bus.Subscribe(func(e Event) {
		mu.Lock()
		count2++
		mu.Unlock()
	})

	bus.Publish(SessionCreatedEvent{Name: "x"})
	time.Sleep(50 * time.Millisecond)

	mu.Lock()
	defer mu.Unlock()
	if count1 != 1 || count2 != 1 {
		t.Errorf("expected both subscribers to receive, got %d and %d", count1, count2)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/service/ -run TestBus -v`
Expected: FAIL — types not defined.

- [ ] **Step 3: Implement the event bus**

Create `internal/service/events.go`:

```go
package service

import (
	"log"
	"sync"
	"time"
)

// Event is the interface all events implement.
type Event interface {
	EventType() string
}

// UnsubscribeFunc removes a subscriber when called.
type UnsubscribeFunc func()

// Bus is a typed pub/sub event bus.
type Bus struct {
	mu          sync.RWMutex
	subscribers map[int]func(Event)
	nextID      int
}

// NewBus creates a new event bus.
func NewBus() *Bus {
	return &Bus{
		subscribers: make(map[int]func(Event)),
	}
}

// Subscribe registers a handler that receives all published events.
// Returns an UnsubscribeFunc to remove the handler.
func (b *Bus) Subscribe(handler func(Event)) UnsubscribeFunc {
	b.mu.Lock()
	id := b.nextID
	b.nextID++
	b.subscribers[id] = handler
	b.mu.Unlock()
	return func() {
		b.mu.Lock()
		delete(b.subscribers, id)
		b.mu.Unlock()
	}
}

// Publish sends an event to all subscribers.
// Each handler is called in its own goroutine. Panics are recovered.
func (b *Bus) Publish(e Event) {
	b.mu.RLock()
	handlers := make([]func(Event), 0, len(b.subscribers))
	for _, h := range b.subscribers {
		handlers = append(handlers, h)
	}
	b.mu.RUnlock()

	for _, h := range handlers {
		go func(handler func(Event)) {
			defer func() {
				if r := recover(); r != nil {
					log.Printf("event bus: handler panicked on %s: %v", e.EventType(), r)
				}
			}()
			handler(e)
		}(h)
	}
}

// --- Core lifecycle events ---

type SessionCreatedEvent struct{ Name string }
func (e SessionCreatedEvent) EventType() string { return "session.created" }

type SessionRemovedEvent struct{ Name string }
func (e SessionRemovedEvent) EventType() string { return "session.removed" }

type StatusChangedEvent struct {
	Session string
	From    string
	To      string
}
func (e StatusChangedEvent) EventType() string { return "status.changed" }

type PaneUpdatedEvent struct {
	Session   string
	PaneIndex int
	Content   string
}
func (e PaneUpdatedEvent) EventType() string { return "pane.updated" }

// --- Agent health events ---

type AgentStuckEvent struct {
	Session      string
	PaneIndex    int
	IdleDuration time.Duration
}
func (e AgentStuckEvent) EventType() string { return "agent.stuck" }

type AgentCrashedEvent struct {
	Session     string
	PaneIndex   int
	PrevProcess string
}
func (e AgentCrashedEvent) EventType() string { return "agent.crashed" }

type AgentWaitingEvent struct {
	Session   string
	PaneIndex int
	Prompt    string
}
func (e AgentWaitingEvent) EventType() string { return "agent.waiting" }

// --- PR/CI lifecycle events ---

type PRDetectedEvent struct {
	Session  string
	PRNumber int
	URL      string
}
func (e PRDetectedEvent) EventType() string { return "pr.detected" }

type CIStatusChangedEvent struct {
	Session  string
	PRNumber int
	From     string
	To       string
}
func (e CIStatusChangedEvent) EventType() string { return "ci.status.changed" }

type ReviewsChangedEvent struct {
	Session   string
	PRNumber  int
	Count     int
	PrevCount int
}
func (e ReviewsChangedEvent) EventType() string { return "reviews.changed" }

type PRMergedEvent struct {
	Session  string
	PRNumber int
}
func (e PRMergedEvent) EventType() string { return "pr.merged" }

// --- Action events ---

type FixAttemptedEvent struct {
	Session     string
	FixType     string // "ci" or "reviews"
	Attempt     int
	MaxAttempts int
}
func (e FixAttemptedEvent) EventType() string { return "fix.attempted" }

type CleanupCompletedEvent struct {
	Session      string
	WorktreePath string
	Branch       string
}
func (e CleanupCompletedEvent) EventType() string { return "cleanup.completed" }
```

- [ ] **Step 4: Run tests**

Run: `go test ./internal/service/ -run TestBus -v`
Expected: All PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/service/events.go internal/service/events_test.go
git commit -m "feat: add typed event bus for pub/sub

Bus with Publish/Subscribe, goroutine-per-handler dispatch,
panic recovery, and unsubscribe support. 13 event types covering
session lifecycle, agent health, PR/CI, and actions."
```

---

### Task 6: Integrate Event Bus into Monitor

**Files:**
- Modify: `internal/service/monitor.go`
- Modify: `internal/server/server.go`

- [ ] **Step 1: Add Bus field to Monitor**

Open `internal/service/monitor.go`. Add `bus *Bus` field to the `Monitor` struct and update `NewMonitor`:

```go
type Monitor struct {
	mu            sync.RWMutex
	sessions      []Session
	refreshMs     int
	errorPatterns []string
	promptPattern string
	inputPatterns []string
	subscribers   []chan []Session // kept during migration
	subMu         sync.Mutex
	stopCh        chan struct{}
	bus           *Bus
}

func NewMonitor(refreshMs int, errorPatterns []string, promptPattern string, inputPatterns []string, bus *Bus) *Monitor {
	return &Monitor{
		refreshMs:     refreshMs,
		errorPatterns: errorPatterns,
		promptPattern: promptPattern,
		inputPatterns: inputPatterns,
		stopCh:        make(chan struct{}),
		bus:           bus,
	}
}
```

- [ ] **Step 2: Add event publishing to poll()**

In `poll()`, after `m.sessions = updated` (line 180), before `m.mu.Unlock()`, add event emission logic. Find:

```go
	m.sessions = updated
	m.mu.Unlock()
	m.notify()
```

Replace with:

```go
	// Collect events to publish AFTER releasing the lock (prevents deadlock
	// since event handlers may call FindSession/Snapshot which need RLock).
	var events []Event
	prevNames := make(map[string]bool)
	for name := range existing {
		prevNames[name] = true
	}
	for _, s := range updated {
		if !prevNames[s.Name] {
			events = append(events, SessionCreatedEvent{Name: s.Name})
		}
		if prev, ok := existing[s.Name]; ok {
			if prev.Status != s.Status {
				events = append(events, StatusChangedEvent{Session: s.Name, From: prev.Status, To: s.Status})
			}
			if s.Status == "waiting" {
				for _, p := range s.Panes {
					if p.Status == "waiting" {
						events = append(events, AgentWaitingEvent{Session: s.Name, PaneIndex: p.Index, Prompt: p.Prompt})
					}
				}
			}
			// Detect agent crash: pane was agent, now shell
			for _, p := range s.Panes {
				for _, pp := range prev.Panes {
					if pp.Index == p.Index && pp.Type == "agent" && p.Type == "shell" {
						events = append(events, AgentCrashedEvent{Session: s.Name, PaneIndex: p.Index, PrevProcess: pp.Process})
					}
				}
			}
		}
	}
	// Detect removed sessions
	currentNames := make(map[string]bool)
	for _, s := range updated {
		currentNames[s.Name] = true
	}
	for name := range existing {
		if !currentNames[name] {
			events = append(events, SessionRemovedEvent{Name: name})
		}
	}

	m.sessions = updated
	m.mu.Unlock()
	m.notify() // keep channel notify during migration

	// Publish events outside the lock
	for _, e := range events {
		m.bus.Publish(e)
	}
```

- [ ] **Step 3: Update server.go to create Bus and pass to Monitor**

Open `internal/server/server.go`. Add `bus *Bus` field to `Server`. In `New()`, create the bus before the monitor:

Find:

```go
	srv := &Server{
		cfg: cfg,
		monitor: service.NewMonitor(
			cfg.Serve.RefreshMs,
			cfg.Dash.ErrorPatterns,
			cfg.Dash.PromptPattern,
			cfg.Dash.InputPatterns,
		),
```

Replace with:

```go
	bus := service.NewBus()
	srv := &Server{
		cfg: cfg,
		bus: bus,
		monitor: service.NewMonitor(
			cfg.Serve.RefreshMs,
			cfg.Dash.ErrorPatterns,
			cfg.Dash.PromptPattern,
			cfg.Dash.InputPatterns,
			bus,
		),
```

- [ ] **Step 4: Build and run all tests**

Run: `go build ./cmd/tsp && go test ./...`
Expected: Compiles and all tests pass.

- [ ] **Step 5: Commit**

```bash
git add internal/service/monitor.go internal/server/server.go
git commit -m "feat: integrate event bus into Monitor

Monitor publishes StatusChanged, SessionCreated, SessionRemoved,
AgentWaiting, and AgentCrashed events during poll loop. Old channel
mechanism kept alongside for migration."
```

---

## Phase 3: Watcher Service (Task 7-8, depends on Phase 2)

---

### Task 7: Watcher State Machine & Core Logic

**Files:**
- Create: `internal/service/watcher.go`
- Create: `internal/service/watcher_test.go`

- [ ] **Step 1: Write test for state transitions**

Create `internal/service/watcher_test.go`:

```go
package service

import (
	"testing"

	"github.com/matteo-hertel/tmux-super-powers/config"
)

func TestWatcherStateTransitions(t *testing.T) {
	bus := NewBus()
	w := NewWatcher(bus, config.WatcherConfig{
		Enabled:       true,
		PollIntervalS: 1,
		MaxCIRetries:  3,
		AutoCleanup:   true,
	})

	// Simulate adding a tracked session
	w.Track("test-session", "feature/test", "/tmp/wt", "/tmp/repo")

	state := w.State("test-session")
	if state != "working" {
		t.Errorf("expected working, got %s", state)
	}

	// Simulate agent finishing
	w.HandleEvent(StatusChangedEvent{Session: "test-session", From: "active", To: "done"})
	state = w.State("test-session")
	if state != "done" {
		t.Errorf("expected done, got %s", state)
	}
}

func TestWatcherSessionRemoval(t *testing.T) {
	bus := NewBus()
	w := NewWatcher(bus, config.WatcherConfig{Enabled: true, PollIntervalS: 1, MaxCIRetries: 3})

	w.Track("test", "branch", "/path", "/repo")
	if w.State("test") != "working" {
		t.Fatal("expected working")
	}

	w.HandleEvent(SessionRemovedEvent{Name: "test"})
	if w.State("test") != "" {
		t.Errorf("expected empty state after removal, got %s", w.State("test"))
	}
}

func TestWatcherIgnoresNonWorktree(t *testing.T) {
	bus := NewBus()
	w := NewWatcher(bus, config.WatcherConfig{Enabled: true, PollIntervalS: 1, MaxCIRetries: 3})

	// Don't track — should be a no-op
	w.HandleEvent(StatusChangedEvent{Session: "untracked", From: "active", To: "done"})
	if w.State("untracked") != "" {
		t.Errorf("expected empty for untracked session")
	}
}

func TestWatcherRetryTracking(t *testing.T) {
	bus := NewBus()
	w := NewWatcher(bus, config.WatcherConfig{Enabled: true, PollIntervalS: 1, MaxCIRetries: 3})

	w.Track("s", "b", "/wt", "/repo")
	ts := w.getTracked("s")
	if ts == nil {
		t.Fatal("expected tracked session")
	}
	ts.state = "watching"
	ts.prNumber = 1

	// Simulate 3 CI failures
	for i := 0; i < 3; i++ {
		ts.ciRetries++
	}
	if ts.ciRetries < 3 {
		t.Errorf("expected 3 retries, got %d", ts.ciRetries)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/service/ -run TestWatcher -v`
Expected: FAIL — types not defined.

- [ ] **Step 3: Implement Watcher**

Create `internal/service/watcher.go`:

```go
package service

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/matteo-hertel/tmux-super-powers/config"
	tmuxpkg "github.com/matteo-hertel/tmux-super-powers/internal/tmux"
)

// trackedSession holds the lifecycle state for a single spawned session.
type trackedSession struct {
	state        string // working, done, pr_polling, watching, fixing_ci, fixing_reviews, green, gave_up, merged, cleanup_done
	branch       string
	worktreePath string
	gitPath      string
	prNumber     int
	prURL        string
	ciRetries    int
	reviewCount  int
	pollErrors   int
	lastPoll     time.Time
}

// Watcher tracks spawned sessions and automates the post-PR lifecycle.
type Watcher struct {
	mu       sync.Mutex
	tracked  map[string]*trackedSession
	bus      *Bus
	cfg      config.WatcherConfig
	monitor  *Monitor
	stopCh   chan struct{}
	unsub    UnsubscribeFunc
}

func NewWatcher(bus *Bus, cfg config.WatcherConfig) *Watcher {
	return &Watcher{
		tracked: make(map[string]*trackedSession),
		bus:     bus,
		cfg:     cfg,
		stopCh:  make(chan struct{}),
	}
}

// SetMonitor sets the monitor reference for session lookups.
func (w *Watcher) SetMonitor(m *Monitor) {
	w.monitor = m
}

// Start begins the watcher loop and subscribes to events.
func (w *Watcher) Start() {
	if !w.cfg.Enabled {
		return
	}
	w.loadState()
	w.unsub = w.bus.Subscribe(func(e Event) {
		w.HandleEvent(e)
	})
	go w.pollLoop()
}

// Stop stops the watcher.
func (w *Watcher) Stop() {
	if w.unsub != nil {
		w.unsub()
	}
	close(w.stopCh)
	w.saveState()
}

// Track starts tracking a spawned session.
func (w *Watcher) Track(sessionName, branch, worktreePath, gitPath string) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.tracked[sessionName] = &trackedSession{
		state:        "working",
		branch:       branch,
		worktreePath: worktreePath,
		gitPath:      gitPath,
	}
	w.saveStateLocked()
}

// State returns the current state for a tracked session, or "" if not tracked.
func (w *Watcher) State(sessionName string) string {
	w.mu.Lock()
	defer w.mu.Unlock()
	if ts, ok := w.tracked[sessionName]; ok {
		return ts.state
	}
	return ""
}

func (w *Watcher) getTracked(name string) *trackedSession {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.tracked[name]
}

// HandleEvent processes a single event.
func (w *Watcher) HandleEvent(e Event) {
	w.mu.Lock()
	defer w.mu.Unlock()

	switch ev := e.(type) {
	case StatusChangedEvent:
		ts, ok := w.tracked[ev.Session]
		if !ok {
			return
		}
		switch ts.state {
		case "working":
			if ev.To == "done" {
				ts.state = "done"
				w.saveStateLocked()
			}
		case "fixing_ci", "fixing_reviews":
			// Wait for agent to start working (From != "done") then finish (To == "done")
			if ev.From != "done" && ev.To == "done" {
				ts.state = "watching"
				w.saveStateLocked()
			}
		}

	case SessionRemovedEvent:
		delete(w.tracked, ev.Name)
		w.saveStateLocked()
	}
}

// pollLoop runs periodic checks for PR/CI/merge status.
func (w *Watcher) pollLoop() {
	interval := time.Duration(w.cfg.PollIntervalS) * time.Second
	if interval == 0 {
		interval = 30 * time.Second
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-w.stopCh:
			return
		case <-ticker.C:
			w.pollAll()
		}
	}
}

func (w *Watcher) pollAll() {
	w.mu.Lock()
	// Snapshot tracked sessions to avoid holding lock during gh calls
	type pollItem struct {
		name string
		ts   *trackedSession
	}
	var items []pollItem
	for name, ts := range w.tracked {
		items = append(items, pollItem{name: name, ts: ts})
	}
	w.mu.Unlock()

	for _, item := range items {
		w.pollSession(item.name, item.ts)
	}
}

func (w *Watcher) pollSession(name string, ts *trackedSession) {
	w.mu.Lock()
	state := ts.state
	w.mu.Unlock()

	switch state {
	case "done", "pr_polling":
		w.pollForPR(name, ts)
	case "watching", "green":
		w.pollCIAndReviews(name, ts)
	case "gave_up":
		w.pollForMerge(name, ts)
	}
}

func (w *Watcher) pollForPR(name string, ts *trackedSession) {
	prNumber, prURL := FindPRForBranch(ts.branch)
	if prNumber > 0 {
		w.mu.Lock()
		ts.prNumber = prNumber
		ts.prURL = prURL
		ts.state = "watching"
		ts.pollErrors = 0
		w.saveStateLocked()
		w.mu.Unlock()
		w.bus.Publish(PRDetectedEvent{Session: name, PRNumber: prNumber, URL: prURL})
	} else {
		w.mu.Lock()
		if ts.state == "done" {
			ts.state = "pr_polling"
			w.saveStateLocked()
		}
		w.mu.Unlock()
	}
}

func (w *Watcher) pollCIAndReviews(name string, ts *trackedSession) {
	if ts.prNumber == 0 {
		return
	}

	// Check merge status
	merged := w.checkMerged(ts.prNumber, ts.gitPath)
	if merged {
		w.handleMerged(name, ts)
		return
	}

	// Check CI
	ciStatus := GetCIStatus(ts.prNumber)
	w.mu.Lock()
	prevCI := ""
	if ts.state == "green" {
		prevCI = "pass"
	}

	if ciStatus == "fail" && ts.state != "fixing_ci" {
		if ts.ciRetries >= w.cfg.MaxCIRetries {
			ts.state = "gave_up"
			w.saveStateLocked()
			w.mu.Unlock()
			w.bus.Publish(CIStatusChangedEvent{Session: name, PRNumber: ts.prNumber, From: prevCI, To: "fail"})
			return
		}
		ts.state = "fixing_ci"
		ts.ciRetries++
		w.saveStateLocked()
		w.mu.Unlock()
		w.bus.Publish(CIStatusChangedEvent{Session: name, PRNumber: ts.prNumber, From: prevCI, To: "fail"})
		w.sendFixCI(name, ts)
		return
	}

	if ciStatus == "pass" {
		if ts.ciRetries > 0 {
			ts.ciRetries = 0
		}
		ts.state = "green"
		w.saveStateLocked()
		w.mu.Unlock()
		if prevCI != "pass" {
			w.bus.Publish(CIStatusChangedEvent{Session: name, PRNumber: ts.prNumber, From: prevCI, To: "pass"})
		}
		// Check for new reviews
		w.checkReviews(name, ts)
		return
	}
	w.mu.Unlock()
}

func (w *Watcher) checkReviews(name string, ts *trackedSession) {
	count := GetReviewCommentCount(ts.prNumber)
	w.mu.Lock()
	if count > ts.reviewCount {
		prevCount := ts.reviewCount
		ts.reviewCount = count
		ts.state = "fixing_reviews"
		w.saveStateLocked()
		w.mu.Unlock()
		w.bus.Publish(ReviewsChangedEvent{Session: name, PRNumber: ts.prNumber, Count: count, PrevCount: prevCount})
		w.sendFixReviews(name, ts)
		return
	}
	w.mu.Unlock()
}

func (w *Watcher) pollForMerge(name string, ts *trackedSession) {
	if ts.prNumber == 0 {
		return
	}
	if w.checkMerged(ts.prNumber, ts.gitPath) {
		w.handleMerged(name, ts)
	}
}

func (w *Watcher) checkMerged(prNumber int, gitPath string) bool {
	out, err := ghPRState(prNumber)
	if err != nil {
		return false
	}
	return out == "MERGED"
}

func (w *Watcher) handleMerged(name string, ts *trackedSession) {
	w.mu.Lock()
	ts.state = "merged"
	w.saveStateLocked()
	w.mu.Unlock()

	w.bus.Publish(PRMergedEvent{Session: name, PRNumber: ts.prNumber})

	if w.cfg.AutoCleanup {
		w.cleanup(name, ts)
	}
}

func (w *Watcher) cleanup(name string, ts *trackedSession) {
	KillSessionByName(name)
	RemoveWorktree(ts.gitPath, ts.worktreePath, ts.branch)

	w.mu.Lock()
	ts.state = "cleanup_done"
	delete(w.tracked, name)
	w.saveStateLocked()
	w.mu.Unlock()

	w.bus.Publish(CleanupCompletedEvent{Session: name, WorktreePath: ts.worktreePath, Branch: ts.branch})
}

func (w *Watcher) sendFixCI(name string, ts *trackedSession) {
	logs, err := FetchFailingCILogs(ts.prNumber)
	if err != nil {
		log.Printf("watcher: failed to fetch CI logs for %s: %v", name, err)
		return
	}
	prompt := "The CI pipeline failed. Here are the failing logs:\n\n" + logs + "\n\nPlease fix the issues and push."
	if len(prompt) > 4000 {
		prompt = prompt[:4000] + "\n\n[truncated]"
	}
	agentPane := w.findAgentPane(name)
	if err := SendToPane(name, agentPane, prompt); err != nil {
		log.Printf("watcher: failed to send fix-ci to %s: %v", name, err)
	}
	w.bus.Publish(FixAttemptedEvent{Session: name, FixType: "ci", Attempt: ts.ciRetries, MaxAttempts: w.cfg.MaxCIRetries})
}

func (w *Watcher) sendFixReviews(name string, ts *trackedSession) {
	comments, err := FetchPRComments(ts.prNumber)
	if err != nil || len(comments) == 0 {
		log.Printf("watcher: no review comments for %s: %v", name, err)
		return
	}
	formatted := FormatPRComments(comments)
	prompt := "Please address these PR review comments:\n\n" + formatted
	agentPane := w.findAgentPane(name)
	if err := SendToPane(name, agentPane, prompt); err != nil {
		log.Printf("watcher: failed to send fix-reviews to %s: %v", name, err)
	}
	w.bus.Publish(FixAttemptedEvent{Session: name, FixType: "reviews", Attempt: 1, MaxAttempts: 1})
}

func (w *Watcher) findAgentPane(name string) int {
	if w.monitor == nil {
		return 1
	}
	s := w.monitor.FindSession(name)
	if s == nil {
		return 1
	}
	for _, p := range s.Panes {
		if p.Type == "agent" {
			return p.Index
		}
	}
	return 1
}

// --- State persistence ---

type watcherPersist struct {
	Sessions map[string]persistedSession `json:"sessions"`
}

type persistedSession struct {
	State        string `json:"state"`
	Branch       string `json:"branch"`
	WorktreePath string `json:"worktreePath"`
	GitPath      string `json:"gitPath"`
	PRNumber     int    `json:"prNumber,omitempty"`
	CIRetries    int    `json:"ciRetries,omitempty"`
	ReviewCount  int    `json:"reviewCount,omitempty"`
}

func (w *Watcher) saveStateLocked() {
	p := watcherPersist{Sessions: make(map[string]persistedSession)}
	for name, ts := range w.tracked {
		p.Sessions[name] = persistedSession{
			State:        ts.state,
			Branch:       ts.branch,
			WorktreePath: ts.worktreePath,
			GitPath:      ts.gitPath,
			PRNumber:     ts.prNumber,
			CIRetries:    ts.ciRetries,
			ReviewCount:  ts.reviewCount,
		}
	}
	data, err := json.MarshalIndent(p, "", "  ")
	if err != nil {
		return
	}
	path := filepath.Join(config.TspDir(), "watcher-state.json")
	os.WriteFile(path, data, 0600)
}

func (w *Watcher) saveState() {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.saveStateLocked()
}

func (w *Watcher) loadState() {
	path := filepath.Join(config.TspDir(), "watcher-state.json")
	data, err := os.ReadFile(path)
	if err != nil {
		return
	}
	var p watcherPersist
	if err := json.Unmarshal(data, &p); err != nil {
		return
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	for name, ps := range p.Sessions {
		ts := &trackedSession{
			state:        ps.State,
			branch:       ps.Branch,
			worktreePath: ps.WorktreePath,
			gitPath:      ps.GitPath,
			prNumber:     ps.PRNumber,
			ciRetries:    ps.CIRetries,
			reviewCount:  ps.ReviewCount,
		}
		// Recovery: in-flight fixing states restart as watching
		if ts.state == "fixing_ci" || ts.state == "fixing_reviews" {
			ts.state = "watching"
		}
		w.tracked[name] = ts
	}
}

// --- Helper wrappers for git/tmux operations ---

func ghPRState(prNumber int) (string, error) {
	cmd := exec.Command("gh", "pr", "view", fmt.Sprintf("%d", prNumber), "--json", "state", "--jq", ".state")
	out, err := cmd.Output()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}

func KillSessionByName(name string) {
	tmuxpkg.KillSession(name)
}

func RemoveWorktree(gitPath, worktreePath, branch string) {
	if err := exec.Command("git", "-C", gitPath, "worktree", "remove", worktreePath, "--force").Run(); err != nil {
		exec.Command("git", "-C", gitPath, "worktree", "prune").Run()
	}
	exec.Command("git", "-C", gitPath, "branch", "-D", branch).Run()
}
```

- [ ] **Step 4: Run tests**

Run: `go test ./internal/service/ -run TestWatcher -v`
Expected: All PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/service/watcher.go internal/service/watcher_test.go
git commit -m "feat: add Watcher service with state machine

Per-session lifecycle tracking: working -> done -> pr_polling ->
watching -> fixing_ci/reviews -> green -> merged -> cleanup.
State persistence to ~/.tsp/watcher-state.json. Restart recovery."
```

---

### Task 8: Wire Watcher into Server

**Files:**
- Modify: `internal/server/server.go`
- Modify: `internal/service/spawn.go`

- [ ] **Step 1: Add Watcher to Server and start it**

Open `internal/server/server.go`. Add `watcher *service.Watcher` field to `Server`. In `New()`, after creating the notifier:

```go
	srv.watcher = service.NewWatcher(bus, cfg.Watcher)
	srv.watcher.SetMonitor(srv.monitor)
```

In `Start()`, after `s.notifier.Start()`:

```go
	s.watcher.Start()
```

In `Stop()`, before `s.notifier.Stop()`:

```go
	s.watcher.Stop()
```

- [ ] **Step 2: Auto-track spawned sessions**

Open `internal/service/spawn.go`. Add a way for the server to pass the watcher to track new spawns. The cleanest way: after `SpawnAgents` returns results, the handler in `handlers.go` calls `watcher.Track()` for each successful spawn.

Open `internal/server/handlers.go`. In `handleSpawn()`, after the results are returned, add:

```go
	for _, r := range results {
		if r.Status == "ok" && s.watcher != nil {
			s.watcher.Track(r.Session, r.Branch, "", "") // worktreePath and gitPath filled by monitor
		}
	}
```

Actually, we need worktreePath and gitPath. Let's add them to `SpawnResult`:

Open `internal/service/spawn.go`. Add to `SpawnResult`:

```go
type SpawnResult struct {
	Task         string `json:"task"`
	Branch       string `json:"branch"`
	Session      string `json:"session"`
	Status       string `json:"status"`
	Error        string `json:"error,omitempty"`
	WorktreePath string `json:"worktreePath,omitempty"`
	GitPath      string `json:"gitPath,omitempty"`
}
```

In `SpawnAgents()`, set these on the result (around line 105):

```go
		result := SpawnResult{Task: task, Branch: branch, Session: sessionName, WorktreePath: worktreePath, GitPath: repoRoot}
```

Now in `handleSpawn()`:

```go
	for _, r := range results {
		if r.Status == "ok" && s.watcher != nil {
			s.watcher.Track(r.Session, r.Branch, r.WorktreePath, r.GitPath)
		}
	}
```

- [ ] **Step 3: Build and run all tests**

Run: `go build ./cmd/tsp && go test ./...`
Expected: Compiles and all tests pass.

- [ ] **Step 4: Commit**

```bash
git add internal/server/server.go internal/server/handlers.go internal/service/spawn.go
git commit -m "feat: wire Watcher into server, auto-track spawned sessions

Watcher starts with the server. Spawned sessions are automatically
tracked for lifecycle automation (CI watching, fix, cleanup)."
```

---

## Phase 4: Mobile App Pane Fix (Task 9, independent)

---

### Task 9: Harden Mobile App Pane Selection

**Files (in repo `~/work/mobileapps/apps/tmux-super-powers`):**
- Modify: `src/app/(tabs)/(servers)/[serverId]/[sessionName].tsx`

> Note: This task operates on the mobile app repo, not the TSP CLI repo.

- [ ] **Step 1: Fix targetPane fallback**

Search for `targetPane` useMemo. Find the pattern:

```typescript
const targetPane = useMemo(() => {
  if (selectedPaneIndex !== undefined) {
    return panes.find((p) => p.index === selectedPaneIndex) ?? panes[0];
  }
  return (
    agentPanes.find((p) => p.status === "waiting") ??
    agentPanes[0] ??
    panes[0]
  );
}, [panes, agentPanes, selectedPaneIndex]);
```

Replace with:

```typescript
const targetPane = useMemo(() => {
  if (selectedPaneIndex !== undefined) {
    const selected = panes.find((p) => p.index === selectedPaneIndex);
    if (selected && selected.type === "agent") {
      return selected;
    }
  }
  return (
    agentPanes.find((p) => p.status === "waiting") ??
    agentPanes[0] ??
    null
  );
}, [panes, agentPanes, selectedPaneIndex]);
```

- [ ] **Step 2: Always show pane selector**

Find: `const showPaneSelector = agentPanes.length > 1;`

Replace: `const showPaneSelector = agentPanes.length >= 1;`

- [ ] **Step 3: Add null guard before sending**

Find the `handleSend` or send mutation call. Add a guard:

```typescript
if (!targetPane) {
  Alert.alert("No agent pane", "No agent pane detected in this session");
  return;
}
if (targetPane.type !== "agent") {
  Alert.alert("Wrong pane", "Selected pane is not an agent pane");
  return;
}
```

- [ ] **Step 4: Commit**

```bash
cd ~/work/mobileapps/apps/tmux-super-powers
git add src/app/\(tabs\)/\(servers\)/\[serverId\]/\[sessionName\].tsx
git commit -m "fix: harden pane selection — never fall back to editor pane

Remove panes[0] fallbacks, always show pane selector, validate
pane type before sending messages."
```

---

## Execution Order Summary

```
Phase 1 (parallel):  Task 1 (worktree fix)
                     Task 2 (dep caching)
                     Task 3 (pane hardening server)
                     Task 4 (config repair)

Phase 2 (sequential): Task 5 (event bus)
                      Task 6 (integrate bus into monitor)

Phase 3 (sequential): Task 7 (watcher state machine)
                      Task 8 (wire watcher into server)

Phase 4 (independent): Task 9 (mobile app pane fix)
```

Tasks 1-4 and Task 9 can all run in parallel. Tasks 5-8 must be sequential.
