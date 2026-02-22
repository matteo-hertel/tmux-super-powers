# Conductor-Parity Design: dash, spawn, harvest

**Date:** 2026-02-20
**Goal:** Close the gap between tmux-super-powers and conductor.build with 3 high-impact commands, plus consolidate existing commands for a cleaner CLI surface.

## Problem

The current CLI handles worktree creation (`wtx-new`) and cleanup (`wtx-rm`) well, but lacks:
- A unified view of all active agent sessions (what are they doing?)
- One-command multi-agent deployment (spawn N agents from N tasks)
- A review/action workflow (see diffs, create PRs, merge, or send feedback)

These are the 3 capabilities that make conductor.build compelling. This design brings them to the terminal.

---

## Command 1: `tsp dash` — Mission Control

### Purpose
Real-time dashboard showing all active tmux sessions with live preview, activity status, and quick actions.

### Interface
```
tsp dash [--worktrees-only] [--refresh N]
```

**Flags:**
- `--worktrees-only` / `-w`: Only show sessions that correspond to git worktrees (filter out plain sessions)
- `--refresh N`: Pane capture refresh interval in milliseconds (default: 500)

### TUI Layout

Two-panel layout (like peek, but richer):

**Left panel (35% width):** Session list
- Each row: session name, branch (if worktree), status icon, last activity time
- Status icons:
  - `●` active (pane content changed in last refresh cycle)
  - `◌` idle (pane content unchanged for >30s)
  - `✓` done (pane shows `$` prompt with no running process for >60s)
  - `✗` error (pane content contains "error", "FAIL", "panic" patterns)
- Color coding: green=active, dim=idle, yellow=done, red=error

**Right panel (65% width):** Live pane capture of selected session
- Auto-refreshes on tick
- Shows last N lines that fit the terminal height
- ANSI color preservation via `-e` flag

### Key Bindings
| Key | Action |
|-----|--------|
| `j/k` or arrows | Navigate sessions |
| `tab` | Cycle panes within selected session |
| `enter` | Jump to session (attach/switch) |
| `d` | Show diff for this worktree (inline or launch `tsp harvest` filtered) |
| `x` | Kill session (with confirmation) |
| `s` | Spawn new agent (opens spawn prompt) |
| `r` | Force refresh |
| `q/esc` | Quit dashboard |

### Activity Detection

Status is inferred by comparing pane captures across refresh cycles:

```go
type sessionStatus struct {
    name          string
    branch        string // empty if not a worktree session
    isWorktree    bool
    status        Status // active, idle, done, error
    lastChanged   time.Time
    paneContent   string
    paneCount     int
    currentPane   int
}
```

**Detection logic:**
1. Capture pane content each tick
2. Compare with previous capture (string equality)
3. If changed → `active`, update `lastChanged`
4. If unchanged >30s → `idle`
5. If last line matches shell prompt pattern AND unchanged >60s → `done`
6. If content contains error patterns → `error` (overrides idle/done)

**Error patterns** (configurable in config):
```yaml
dash:
  error_patterns:
    - "FAIL"
    - "panic:"
    - "Error:"
    - "error\\["
  prompt_pattern: "\\$\\s*$"  # Shell prompt detection
```

### Data Flow

```
┌─────────────┐    tick     ┌──────────────┐    compare    ┌────────────┐
│ tmux         │ ────────── │ capture-pane  │ ──────────── │ status     │
│ sessions     │  (500ms)   │ -t sess:0.N  │   (prev vs   │ inference  │
│              │            │ -p -e         │    current)  │            │
└─────────────┘            └──────────────┘              └────────────┘
       │                                                        │
       │         ┌────────────┐         ┌───────────────┐       │
       └──────── │ list-sessions│ ────── │ worktree list │ ──────┘
                 │ -F format   │        │ --porcelain   │
                 └────────────┘         └───────────────┘
```

### Implementation Notes
- Extends the existing `peek` model pattern (tick-based refresh, two-panel layout)
- Reuse `tmux.AttachOrSwitch()` for the jump action
- Worktree detection: match session names against `git worktree list` output
- Session list: `tmux list-sessions -F "#{session_name}:#{session_path}"`

---

## Command 2: `tsp spawn` — Agent Deployer

### Purpose
Create multiple worktree+session+agent combos in one command, each with a task prompt sent to claude.

### Interface
```
tsp spawn [flags] "task1" "task2" "task3"
tsp spawn --file tasks.txt [flags]
```

**Flags:**
- `--file / -f`: Read tasks from file (one per line, blank lines and `#` comments ignored)
- `--base / -b`: Base branch for worktrees (default: current branch)
- `--dash`: Open `tsp dash` after all agents are deployed
- `--setup`: Command to run in each worktree after dependency install (e.g., `"cp ../.env .env"`)
- `--no-install`: Skip automatic dependency installation
- `--dry-run`: Show what would be created without doing it
- `--agent`: Agent command for right pane (default: `claude --dangerously-skip-permissions`)

### Branch Naming

Auto-generate branch names from task descriptions:

```go
func taskToBranch(task string) string {
    // "fix the auth token expiry bug" → "spawn/fix-auth-token-expiry-bug"
    // Lowercase, replace spaces with hyphens, strip non-alphanumeric
    // Prefix with "spawn/"
    // Truncate to 50 chars
}
```

### Task File Format
```
# tasks.txt - one task per line
fix the authentication token expiry bug
add dark mode support to the settings page
refactor the database connection pooling layer

# blank lines and comments are ignored
```

### Execution Flow

For each task (parallel where possible):

```
1. Generate branch name from task description
2. Create branch from --base if it doesn't exist
3. Create git worktree at ~/work/code/{repo}-{branch}
4. Detect package manager, run install (unless --no-install)
5. Run --setup command if provided
6. Create tmux session: {repo}-{branch}
   - Left pane: nvim
   - Right pane: claude --dangerously-skip-permissions
7. Send task prompt to claude pane via tmux send-keys
8. (After all tasks) Open dash if --dash flag set
```

**Step 7 detail — sending the prompt:**
```go
// Send the task description to the claude pane
prompt := fmt.Sprintf("%s\n", task)
exec.Command("tmux", "send-keys", "-t", sessionName+":0.1", prompt, "Enter").Run()
```

### Progress Output (non-TUI)

Since spawning can take time (dependency installs), show progress in the terminal:

```
Spawning 3 agents from branch main...

[1/3] fix-auth-token-expiry-bug
      ✓ branch created
      ✓ worktree created at ~/work/code/myapp-spawn/fix-auth-token-expiry-bug
      ✓ pnpm install (4.2s)
      ✓ session created, claude prompted

[2/3] add-dark-mode-support
      ✓ branch created
      ✓ worktree created
      ◌ pnpm install...

[3/3] refactor-db-connection-pooling
      ✓ branch created
      ◌ creating worktree...

All agents deployed. Run `tsp dash` to monitor.
```

### Error Handling
- If branch already exists: reuse it (with warning)
- If worktree path already exists: skip with warning
- If tmux session exists: skip with warning
- Partial failures: continue with remaining tasks, report failures at the end

### Config Extension
```yaml
spawn:
  worktree_base: ~/work/code  # Base path for worktrees (default: ~/work/code)
  agent_command: "claude --dangerously-skip-permissions"
  default_setup: ""  # Optional default setup command
```

---

## Command 3: `tsp harvest` — Collect & Review

### Purpose
Review diffs from all active worktrees, take action (PR, merge, discard, continue) without switching sessions.

### Interface
```
tsp harvest [--all] [session-name...]
```

**Flags:**
- `--all / -a`: Include worktrees with no changes
- Positional args: filter to specific session names

### TUI Layout

Two-panel layout:

**Left panel (35% width):** Worktree summary list
- Each row: branch name, files changed count, +insertions/-deletions, status
- Status: `ready` (has commits ahead of base), `wip` (uncommitted changes), `clean` (no changes)
- PR indicators (shown inline after status):
  - `PR #42` — has an open PR
  - `CI ✓` / `CI ✗` / `CI ◌` — CI passing / failing / pending
  - `3 comments` — unresolved review comments count
- Color: green=ready, yellow=wip, dim=clean, red=CI failing, blue=has comments

**Right panel (65% width):** Diff viewer
- Scrollable diff output for selected worktree
- File-by-file navigation
- Syntax-highlighted (via lipgloss)
- Shows both staged and unstaged changes, plus commits ahead of base

### Key Bindings
| Key | Action |
|-----|--------|
| `j/k` or arrows | Navigate worktrees (left panel focused) |
| `tab` | Switch focus between worktree list and diff viewer |
| `J/K` (shift) | Navigate files within diff (right panel focused) |
| `enter` | Expand/collapse file diff |
| `p` | Create PR for selected worktree |
| `m` | Merge branch to base and cleanup |
| `x` | Discard changes and remove worktree |
| `c` | Send follow-up prompt to agent |
| `f` | Fix CI — fetch failing logs, send to agent |
| `r` | Review — fetch PR comments, send to agent |
| `o` | Open/jump to session |
| `q/esc` | Quit |

### Actions Detail

**`p` — Create PR:**
1. Ensure all changes are committed (prompt if uncommitted)
2. Push branch to remote
3. Run `gh pr create --title "{branch}" --body "auto-generated from tsp harvest"`
4. Show PR URL in status bar
5. Move worktree status to "PR created"

**`m` — Merge to base:**
1. Ensure all changes are committed
2. Switch to base branch
3. `git merge {branch}`
4. Remove worktree + session (same as `wtx-rm`)
5. Remove from list, show success

**`x` — Discard:**
1. Confirmation prompt ("Discard all changes in {branch}? y/n")
2. Remove worktree with `--force`
3. Delete branch
4. Kill tmux session
5. Remove from list

**`c` — Continue (send feedback):**
1. Open textinput overlay
2. User types follow-up prompt
3. Send to the claude pane via `tmux send-keys`
4. Return to harvest view

**`f` — Fix CI:**
1. Detect PR for this branch via `gh pr list --head {branch} --json number`
2. Fetch failing check runs via `gh pr checks {number} --json name,status,conclusion`
3. For each failing check, fetch logs via `gh run view {run-id} --log-failed`
4. Compose a prompt: "The CI pipeline failed. Here are the failing logs:\n\n{logs}\n\nPlease fix the issues and push."
5. Send to the claude pane via `tmux send-keys`
6. Show status bar confirmation: "CI logs sent to agent"
7. If no PR exists yet, show message: "No PR found — create one first with [p]"

**`r` — Address PR review comments:**
1. Detect PR for this branch via `gh pr list --head {branch} --json number`
2. Fetch review comments via `gh api repos/{owner}/{repo}/pulls/{number}/comments`
3. Fetch PR review threads via `gh pr view {number} --json reviews,comments`
4. Format comments grouped by file:
   ```
   ## PR Review Comments

   ### src/auth/jwt.go (line 45)
   @reviewer: "This doesn't handle the case where token is empty"

   ### src/auth/jwt.go (line 78)
   @reviewer: "Consider using a constant for the expiry duration"
   ```
5. Compose a prompt: "Please address these PR review comments:\n\n{formatted_comments}"
6. Send to the claude pane via `tmux send-keys`
7. Show status bar confirmation: "Review comments sent to agent ({N} comments)"
8. If no PR exists, show message: "No PR found — create one first with [p]"

### Diff Data

```go
type worktreeInfo struct {
    sessionName   string
    branch        string
    baseBranch    string
    worktreePath  string
    filesChanged  int
    insertions    int
    deletions     int
    status        string // "ready", "wip", "clean"
    diffOutput    string // full diff text
    aheadCount    int    // commits ahead of base
    // PR & CI state (populated via gh CLI)
    prNumber      int    // 0 if no PR
    prURL         string
    ciStatus      string // "pass", "fail", "pending", "" (no PR)
    reviewCount   int    // number of unresolved review comments
}
```

**Data collection:**
```bash
# For each worktree:
git -C {path} diff --stat          # uncommitted changes
git -C {path} diff HEAD --stat     # staged + unstaged
git -C {path} log --oneline {base}..HEAD  # commits ahead
git -C {path} diff {base}..HEAD    # full diff vs base

# PR & CI state (requires gh CLI):
gh pr list --head {branch} --json number,url          # find PR
gh pr checks {number} --json name,status,conclusion   # CI status
gh api repos/{owner}/{repo}/pulls/{number}/comments   # review comments
gh run view {run-id} --log-failed                     # failing CI logs (on-demand for 'f')
```

---

## Consolidation Changes

### `sandbox` + `project` → `tsp new`

```
tsp new [--sandbox | --project] [name]
```

- Default behavior: infer from context or prompt user
- `--sandbox`: use `config.Sandbox.Path`
- `--project`: use `config.Projects.Path`
- If neither flag: show selector (sandbox / project)
- Keep `tsp sandbox` and `tsp project` as hidden aliases for backward compat

### `wtx-rm` + `txrm` → `tsp rm`

```
tsp rm [--sessions-only]
```

- Default: show all sessions, smart-detect which are worktrees
- Worktree sessions: full cleanup (kill session + remove worktree + delete branch)
- Plain sessions: just kill session
- `--sessions-only`: skip worktree cleanup, just kill tmux sessions
- Keep `wtx-rm` and `txrm` as hidden aliases

### `list` → hidden alias for `dash`

- `tsp list` and `txl` become hidden aliases that launch `tsp dash`
- Or keep `list` as a lightweight non-refreshing session picker for quick use

---

## Config Additions

```yaml
# New sections added to ~/.tmux-super-powers.yaml

dash:
  refresh_ms: 500
  error_patterns:
    - "FAIL"
    - "panic:"
    - "Error:"
  prompt_pattern: "\\$\\s*$"

spawn:
  worktree_base: ~/work/code
  agent_command: "claude --dangerously-skip-permissions"
  default_setup: ""
```

---

## Implementation Priority

1. **`tsp dash`** — Highest impact, extends existing `peek` pattern
2. **`tsp spawn`** — Enables the full parallel workflow
3. **`tsp harvest`** — Completes the review loop
4. **Consolidation** — Can be done incrementally alongside or after

---

## The Full Workflow

```
tsp spawn "task1" "task2" "task3" --dash
    ↓ (agents start working)
tsp dash
    ↓ (monitor until agents finish)
tsp harvest
    ↓ (review diffs, create PRs or merge)
tsp rm
    ↓ (clean up any remaining sessions)
done
```

This gives the complete Conductor lifecycle — deploy, monitor, review, cleanup — in 4 CLI commands, all terminal-native with zero GUI dependency.
