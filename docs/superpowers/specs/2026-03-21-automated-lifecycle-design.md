# Automated Post-Spawn Lifecycle — Design Spec

**Date:** 2026-03-21
**Status:** Draft
**Scope:** Event bus, watcher service, worktree fixes, dep caching, pane hardening, config repair

---

## Problem Statement

TSP has all the building blocks for automated agent lifecycle management (CI checking, fix-CI, fix-reviews, merge, cleanup) but wires them up as manual triggers only. After spawning agents, the user must manually monitor each session, trigger fixes, and clean up worktrees. Additionally, worktree deletion is buggy (sessions killed but worktrees left behind), dep installation is slow (full install per worktree), and mobile pane selection is unreliable.

## Goals

1. **Automate the post-spawn lifecycle**: once an agent creates a PR, TSP watches CI/reviews, auto-fixes failures (up to 3 retries), and cleans up after merge.
2. **Fix worktree deletion**: ensure sessions, directories, and git metadata are all cleaned up reliably.
3. **Speed up worktree creation**: hardlink-copy `node_modules` from main repo before install.
4. **Harden pane selection**: eliminate fuzzy matching failures in both server and mobile app.
5. **Config migration**: add `tsp config repair` to detect and fill missing config fields.

## Design Decisions

- **Agent creates PRs** — TSP detects when a PR appears on the branch and starts watching. TSP does not auto-create PRs.
- **3 CI fix retries** then notify and stop — prevents infinite loops.
- **User merges on GitHub** — TSP detects the merge via polling and auto-cleans up.
- **Hardlink copy for node_modules** — Go-native hardlink walk (not `cp -al` which is Linux-only) gives isolation + fast cleanup + near-instant install. No `--prefer-offline` needed since deps are already present.
- **Event bus is comprehensive** — Monitor, Notifier, WebSocket, and Watcher all use it. Migrated incrementally: existing channel mechanism kept during transition, removed once all consumers are on the bus.
- **Event ordering** — handlers run in goroutines so ordering across subscribers is not guaranteed. Each subscriber handles events independently. Handler panics are recovered to avoid crashing the server.

---

## 1. Event Bus

### Location

`internal/service/events.go`

### Event Types

```go
// Core lifecycle
SessionCreated   {Name string}
SessionRemoved   {Name string}
StatusChanged    {Session string, From string, To string}
PaneUpdated      {Session string, PaneIndex int, Content string}

// Agent health
AgentStuck       {Session string, PaneIndex int, IdleDuration time.Duration}
AgentCrashed     {Session string, PaneIndex int, PrevProcess string}
AgentWaiting     {Session string, PaneIndex int, Prompt string}

// PR/CI lifecycle
PRDetected       {Session string, PRNumber int, URL string}
CIStatusChanged  {Session string, PRNumber int, From string, To string}
ReviewsChanged   {Session string, PRNumber int, Count int, PrevCount int}
PRMerged         {Session string, PRNumber int}

// Actions taken (for observability / mobile app)
FixAttempted     {Session string, Type string, Attempt int, MaxAttempts int}
CleanupCompleted {Session string, WorktreePath string, Branch string}
```

### Implementation

- `Bus` struct with `Publish(Event)` and `Subscribe(handler func(Event)) UnsubscribeFunc`.
- `Subscribe` returns an `UnsubscribeFunc` for cleanup. Subscribers can also use `context.Context` for lifecycle management.
- Handlers called in goroutines to avoid blocking the publisher. Panics in handlers are recovered with `log.Printf` — never crash the server.
- Event is an interface with a `Type() string` method. Each event type is a concrete struct.
- Thread-safe: subscriber list protected by `sync.RWMutex`.
- **Event ordering**: not guaranteed across subscribers. Each subscriber processes events independently. Within a single subscriber's handler calls, events arrive in publish order (sequential dispatch per subscriber).

### Integration Points

- **Monitor** (`monitor.go`): publishes `StatusChanged`, `PaneUpdated`, `SessionCreated`, `SessionRemoved`, `AgentStuck`, `AgentCrashed`, `AgentWaiting` during its poll loop. Still maintains `Snapshot()` and `FindSession()` for initial state loads and handler lookups. During migration: existing `Subscribe()`/`Unsubscribe()` channel mechanism is kept alongside the event bus, removed once all consumers are migrated.
- **Notifier** (`notifier.go`): refactored to subscribe to `StatusChanged`, `AgentWaiting`, `CIStatusChanged` events instead of diffing raw session snapshots. Push notification logic stays the same but triggered by events.
- **WebSocket** (`handlers.go`): subscribes to `PaneUpdated` and `StatusChanged`. Sends granular updates to the mobile app. For backwards compatibility: initial connection sends a full snapshot, and the server continues sending full snapshots alongside events until mobile app is updated to handle events. Mobile app versions in the field will continue working.
- **Watcher** (`watcher.go`): new subscriber — see section 2.

---

## 2. Watcher Service

### Location

`internal/service/watcher.go`

### State Machine

Per-session lifecycle states for spawned worktree sessions:

```
working → done → pr_polling → watching ←──────────────┐
    |                |            |                    |
    |                |   +--------+--------+           |
    |                |   |        |        |           |
    |                | fixing_ci  |     green ─────────┤ (CI regresses)
    |                |   |        |        |           |
    |                |   v    fixing_reviews            |
    |                | watching   |                    |
    |                |            v                    |
    |                |         watching                |
    |                |                                 |
    |           (after 3 fails)              (user merges on GitHub)
    |                |                                 |
    |           gave_up ──(still polls merge)──→ merged
    |                                              |
    |                                         cleanup_done
    |
    └──── (SessionRemoved at ANY state) ──→ removed (clean up tracking)
```

**Global transitions (from any state):**
- `SessionRemoved` event → remove from watcher tracking, no cleanup needed (user manually killed it).
- `AgentCrashed` event → log warning, remain in current state (agent may restart).

### State Descriptions

| State | Entry Condition | Behavior |
|-------|----------------|----------|
| `working` | Session created, agent active | No action. Waiting for agent to finish. |
| `done` | `StatusChanged` to "done" | Start polling for PR existence. |
| `pr_polling` | Agent finished, no PR yet | Poll `gh pr list --head <branch>` every `poll_interval_s`. |
| `watching` | PR found or fix completed | Poll CI status + review count every `poll_interval_s`. |
| `fixing_ci` | CI status → "fail" | Fetch failing logs, send fix prompt to agent pane. Publish `FixAttempted`. Increment retry counter. Wait for agent status → "done". |
| `fixing_reviews` | Review count increased | Fetch new comments, send to agent pane. Publish `FixAttempted`. Wait for agent status → "done". |
| `green` | CI passing, no new reviews | Publish `CIStatusChanged` (to "pass"). Notifier sends push. Continue polling — if CI regresses (flaky test), transition back to `watching`. |
| `gave_up` | CI fix retry count >= 3 | Publish notification event. Stop sending fix prompts. **Still polls for merge** so auto-cleanup works if human fixes CI manually. |
| `merged` | PR state is "merged" | Trigger cleanup: kill session, `git worktree remove --force`, `git branch -D`. |
| `cleanup_done` | Cleanup completed | Publish `CleanupCompleted`. Remove from watcher tracking. |

### Key Behaviors

- **Scope**: Only tracks worktree-based sessions (`session.IsWorktree == true`). Regular tmux sessions are ignored.
- **Polling**: CI/review checks happen every `poll_interval_s` (default 30s). Uses existing `GetCIStatus()`, `GetReviewCommentCount()`, `FetchFailingCILogs()`, `FetchPRComments()` functions.
- **Fix sequencing**: After sending a fix prompt, the watcher transitions to `fixing_ci`/`fixing_reviews` and waits for a `StatusChanged` event where `From != "done"` and `To == "done"` — confirming the agent actually started working and finished, not just a stale "done" event. Does not spam the agent.
- **Concurrent triggers**: If CI fails AND reviews arrive simultaneously, CI fix takes priority. Reviews are queued and sent after the CI fix cycle completes (when watcher returns to `watching` state). Only one fix operation at a time per session.
- **Retry tracking**: Per-session counter. Resets when CI transitions from "fail" to "pass" (regardless of subsequent failures — each pass→fail is a new cycle).
- **Merge detection**: Polls `gh pr view <number> --json state` to detect merged state. Same interval as CI polling. Continues in `gave_up` state so manual fixes still get auto-cleanup.
- **Polling error handling**: If `gh` commands fail (network, rate limiting, auth), log the error and retry on next poll interval. Do not transition states on transient failures. After 5 consecutive poll failures, log a warning but keep retrying.
- **State persistence**: `~/.tsp/watcher-state.json`, written on every state transition.
- **Server restart recovery**: On startup, for each persisted session: (1) verify tmux session still exists — if not, remove tracking entry; (2) re-check PR status — if merged, go to `merged` state; (3) re-check CI status — reconcile with persisted state; (4) resume from current reality, not persisted state. In-flight `fixing_ci` sessions restart at `watching` (don't re-send fix prompt — the agent may have already fixed it).
- **Agent pane resolution**: Uses the same agent-pane lookup as `handleFixCI()` — finds first pane with `Type == "agent"`.
- **Session removal**: On `SessionRemoved` event, remove the session from watcher tracking regardless of current state. No cleanup actions — the user chose to kill it manually.

### Config

```yaml
watcher:
  enabled: true
  poll_interval_s: 30
  max_ci_retries: 3
  auto_cleanup: true   # auto-cleanup worktree after merge
```

Added to `config.Config` as `WatcherConfig` struct.

---

## 3. Worktree Deletion Fix

### Files Affected

- `internal/cmd/dash.go` — `discardWorktree()` (lines 461-475), `mergeBranch()` (lines 431-433)
- `internal/cmd/rm.go` — lines 74-85

### Current Bug

```go
// dash.go:461-475
tmuxpkg.KillSession(s.name)
os.RemoveAll(s.worktreePath)                                    // ← removes dir BEFORE git knows about it
exec.Command("git", "worktree", "remove", s.worktreePath, "--force").Run()  // ← fails silently, git metadata left behind
exec.Command("git", "branch", "-D", s.branch).Run()            // ← no error handling
```

### Fix

1. **Remove `os.RemoveAll` call** — `git worktree remove --force` handles directory removal and cleans up git metadata in one step.
2. **Correct git context**: run `git worktree remove` from the **main repo** using `-C <gitPath>`, not from the worktree itself.
3. **Ordering**: kill session → `git -C <gitPath> worktree remove --force <path>` → `git -C <gitPath> branch -D <branch>`.
4. **Error handling**: check return values, surface errors in status message. Continue on non-fatal errors (e.g., branch already deleted).

Same fix pattern applied to all three locations (`dash.go`, `rm.go`, and also `gtwremove.go:201-277` which has the same `os.RemoveAll`-before-`git worktree remove` ordering bug). All worktree removal code paths will be unified to use the corrected ordering.

---

## 4. Dep Caching (Hardlink Copy)

### Location

`internal/service/spawn.go` — `spawnRunPM()` function

### Strategy

Before running `<pm> install` in a new worktree:

1. Check if `node_modules/` exists in the main repo root.
2. If yes, walk the directory tree using Go's `filepath.Walk` and create hardlinks for regular files, creating directories as needed. This is platform-agnostic (works on macOS and Linux — `cp -al` is Linux-only and not available on macOS).
3. Run normal `<pm> install` — this resolves near-instantly since all deps are already present. Any lockfile differences get handled by the install step.

### Why Hardlink Copy

- **Fast creation**: hardlinking is near-instant regardless of `node_modules` size (no data copied, just inode references).
- **Disk efficient**: hardlinks share inode storage, no duplication.
- **Isolated**: each worktree gets its own directory entries — deleting one doesn't affect others.
- **Clean cleanup**: when worktree is deleted, space is reclaimed for any unique files.
- **Works with all package managers**: npm, yarn, pnpm, bun all handle pre-existing `node_modules` correctly.
- **Platform agnostic**: Go's `os.Link()` works on both macOS and Linux.

### Implementation

Add `spawnCopyNodeModules(repoRoot, worktreePath string) error` function using `filepath.Walk` + `os.Link()` for files and `os.MkdirAll()` for directories. Symlinks are re-created as symlinks (not followed). Called before `spawnRunPM()`. Falls back silently if `node_modules` doesn't exist in the repo root.

Also extend the existing yarn-specific cache copy (`.yarn/cache`, `.yarn/install-state.gz`) to be more general — copy `.yarn` directory for yarn, `.pnp.*` for yarn PnP setups.

---

## 5. Pane Selection Hardening

### Server Side

**File:** `internal/service/sessions.go` — `PaneTypeFromProcess()`

Current issue: `isClaudeVersion()` uses brittle semver parsing. Claude Code reports its version string as the process name, which can fail for non-standard versions.

**Fix:** Replace `isClaudeVersion()` with checking the process tree. Use `tmux display-message -p -t <session>:<pane> '#{pane_current_command}'` alongside checking the pane's parent process. If the parent is `node` and the process looks like a version string, classify as "agent". Also add "aider", "codex" to the explicit agent list.

Alternatively (simpler): keep the version check but make it more permissive — any string of digits and dots with 2-3 parts counts as a potential agent, and cross-reference with whether the pane was created by a spawn command (add a `spawned` flag to session metadata).

### Mobile App Side

**Repository:** `~/work/mobileapps/apps/tmux-super-powers` (separate repo from the CLI/server)

**File:** `src/app/(tabs)/(servers)/[serverId]/[sessionName].tsx`

> Note: line numbers below reference the mobile app repo at time of analysis and may drift. Search by code pattern rather than line number during implementation.

1. **Remove `panes[0]` fallbacks** — in the `targetPane` useMemo, return `null` instead of falling back to `panes[0]` (which is typically the editor pane).
2. **Always show pane selector** — change `agentPanes.length > 1` to `agentPanes.length >= 1` so users can always see and verify which pane is selected.
3. **Validate before sending** — before `sendMutation`, check `targetPane?.type === "agent"`. If not, show alert.
4. **Handle null targetPane** — show "No agent pane detected" message with a manual pane override option.

---

## 6. Config Migration & Repair

### New Command

`tsp config repair` — subcommand added to existing `tsp config`.

### Behavior

1. Load current config from `~/.tsp/config.yaml`. If YAML is invalid, report the parse error and offer to reset to defaults (preserving a backup).
2. Compare each field against the default config (`defaultConfig()`).
3. Report missing/new fields and their default values.
4. Back up current config to `~/.tsp/config.yaml.bak`.
5. Write updated config with missing fields filled in, preserving all existing user values.

### Server Startup Check

During `tsp serve` startup, compare loaded config against defaults. If new fields are detected, log: `"New config options available. Run 'tsp config repair' to update your config."`

### Location

`internal/cmd/config.go` — add `repairCmd` cobra subcommand.
`config/config.go` — add `Repair(cfg *Config) (changes []string, updated *Config)` function that returns what changed.

---

## File Summary

| New File | Purpose |
|----------|---------|
| `internal/service/events.go` | Event bus: types, Bus struct, Publish/Subscribe |
| `internal/service/watcher.go` | Watcher service: state machine, PR/CI polling, auto-fix |
| `internal/service/watcher_test.go` | Watcher tests |
| `internal/service/events_test.go` | Event bus tests |

| Modified File | Changes |
|---------------|---------|
| `internal/service/monitor.go` | Replace channel broadcast with event bus publishing |
| `internal/service/notifier.go` | Refactor to subscribe to events instead of diffing snapshots |
| `internal/server/handlers.go` | WebSocket handler uses events; pane filtering support |
| `internal/server/server.go` | Wire up event bus, watcher; pass bus to monitor/notifier |
| `internal/service/sessions.go` | Harden `PaneTypeFromProcess()` |
| `internal/service/spawn.go` | Add `spawnCopyNodeModules()` before install |
| `internal/cmd/dash.go` | Fix `discardWorktree()` and `mergeBranch()` |
| `internal/cmd/rm.go` | Fix worktree deletion error handling |
| `internal/cmd/gtwremove.go` | Fix same worktree deletion ordering bug |
| `internal/cmd/config.go` | Add `tsp config repair` subcommand |
| `config/config.go` | Add `WatcherConfig`, `Repair()` function |

| Mobile App File (repo: `~/work/mobileapps/apps/tmux-super-powers`) | Changes |
|---------------------------------------------------------------------|---------|
| `src/app/(tabs)/(servers)/[serverId]/[sessionName].tsx` | Remove `panes[0]` fallbacks, always show selector, validate before send |
| `src/lib/use-server-websocket.ts` | Handle granular event updates (optional, backwards-compatible) |
