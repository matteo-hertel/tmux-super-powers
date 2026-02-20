# TDD Refactor + New Commands Design

**Date:** 2026-02-20
**Status:** Approved

## Overview

Bottom-up approach: extract testable utilities, fix bugs with tests first, deduplicate, then add `middle` and `peek` commands. All tests must be CI-safe (no real tmux, git repos, or home directories — use `t.TempDir()`, `t.Setenv`, mock data).

## Phase 1: Extract Shared Utilities + Tests

### 1a. `internal/tmux/tmux.go`

Tmux interaction helpers:
- `SanitizeSessionName(name string) string` — replace `.`, `:` with `-`
- `SessionExists(name string) bool`
- `AttachOrSwitch(name string) error` — check `$TMUX`, switch-client or attach-session
- `CreateSession(name, dir, leftCmd, rightCmd string) error` — use `-c` flag, no shell injection
- `KillSession(name string) error`

Tests in `internal/tmux/tmux_test.go`:
- `SanitizeSessionName` — pure function, test with `.`, `:`, spaces, normal strings, empty string

### 1b. `internal/pathutil/pathutil.go`

Path helpers:
- `ExpandPath(path string) string` — handle `~/`, empty string, single char safely
- `ExpandEnvVar(s string) string` — expand `$VAR` style strings from environment

Tests in `internal/pathutil/pathutil_test.go`:
- `ExpandPath`: empty string, `"/"`, `"~/"`, `"~/projects"`, normal absolute path
- `ExpandEnvVar`: `"$EDITOR"` with env set, `"$EDITOR"` with env unset, literal string, empty

All tests use `t.Setenv("HOME", t.TempDir())` — no real home dir.

### 1c. `config/config.go` fixes

- Replace all `os.Getenv("HOME")` with `os.UserHomeDir()`
- Add `$EDITOR` expansion via `pathutil.ExpandEnvVar`
- Accept optional config path parameter for testability

Tests in `config/config_test.go`:
- Load missing file → returns defaults
- Load valid YAML → parses correctly
- Editor `$EDITOR` expansion works
- Save + Load round-trip
- All use temp dirs, never touch real `~/.tmux-super-powers.yaml`

### 1d. `internal/cmd/helpers.go`

Move pure functions out of command files:
- `detectPackageManager(repoRoot string) string` — from gtmux.go
- `buildIgnoreSet(patterns []string) map[string]bool` — from dir.go
- `shouldIgnoreDir(name string, ignoreSet map[string]bool) bool` — from dir.go

Tests in `internal/cmd/helpers_test.go`:
- `detectPackageManager`: temp dirs with various lock files, no lock file, package.json only
- `shouldIgnoreDir`: hidden dirs, user ignores, normal dirs
- `buildIgnoreSet`: empty list, list with entries

## Phase 2: Bug Fixes (Test First)

### 2a. `expandPath` panic

- **Test first:** `ExpandPath("")` and `ExpandPath("/")` don't panic, return input unchanged
- **Fix:** `strings.HasPrefix(path, "~/")` instead of `path[:2] == "~/"`

### 2b. Shell injection in `createSession`

- **Fix:** replace `"cd "+dir+";nvim"` with `tmux new-session -d -s name -c dir "nvim"` (the `-c` flag pattern)
- **Verify:** grep codebase for `"cd "+` — zero instances must remain
- Uses the new `tmux.CreateSession` helper

### 2c. Session name sanitization

- **Test first:** `SanitizeSessionName("my.project")` → `"my-project"`, `"foo:bar"` → `"foo-bar"`
- **Fix:** apply `tmux.SanitizeSessionName` everywhere session names come from `filepath.Base()` or branch names

### 2d. Git lock race in `wtx-rm`

- **Fix:** restructure `removeSelectedWorktrees`:
  1. Parallel phase: kill tmux sessions + remove directories (goroutines)
  2. Sequential phase: `git worktree remove` + `git branch -D` for each (single loop after wg.Wait)
- Output collection stays the same (strings.Builder per worktree)

### 2e. Flawed main worktree detection

- **Test first:** parse mock porcelain output with hyphens in path, verify first entry always skipped
- **Fix:** skip first worktree by index, remove `strings.Contains(line, "-")` heuristic

### 2f. Error propagation

- Change `AttachOrSwitch`, `CreateSession`, `KillSession` to return `error`
- Callers check and report errors to the user
- No test needed — signature change verified by compiler

## Phase 3: Deduplicate

### 3a. Merge sandbox.go and project.go

Extract into `internal/cmd/project_creator.go`:
```go
type projectCreatorConfig struct {
    Title         string
    Placeholder   string
    BasePath      string
    SessionPrefix string
}
func runProjectCreator(cfg projectCreatorConfig) // shared TUI + creation logic
```
Both commands become thin wrappers calling `runProjectCreator`.

### 3b. Centralize attachToSession

Replace 4 inline copies (list.go, dir.go, sandbox.go, project.go) with `tmux.AttachOrSwitch(name)`.

### 3c. Remove dead code

- Delete original `cleanupEmptyParents` (replaced by `cleanupEmptyParentsCollect`)
- Let compiler find any other unreferenced functions after refactoring

### 3d. Fix doc drift

- Rename `txl` to `list` (or add alias)
- Update CLAUDE.md default behavior description to match `root.go`

## Phase 4: New Commands

### 4a. `tsp middle "command"`

File: `internal/cmd/middle.go`

```
tsp middle "htop"
tsp middle "lazydocker" --size 80
tsp middle --width 90 --height 70 "command"
```

Implementation:
- Cobra command, `Args: cobra.ExactArgs(1)`
- `--size` flag (default 75): percentage for both width and height
- `--width`, `--height` flags: override individual dimensions
- Check `$TMUX` is set, error if not inside tmux
- Execute: `tmux display-popup -E -w <width>% -h <height>% "<command>"`
- Pass through stdin/stdout/stderr for interactive commands
- No TUI needed

Tests in `internal/cmd/middle_test.go`:
- Flag parsing: default size, custom size, width/height overrides
- Command args building: verify the tmux args array is correct
- Error when not in tmux (mock `$TMUX` as empty)

### 4b. `tsp peek`

File: `internal/cmd/peek.go`

```
tsp peek              # interactive dashboard
tsp peek backend      # direct peek at named session
```

Implementation:
- Two-panel bubbletea TUI using lipgloss
- Left panel (30% width): session list via `list.Model`
- Right panel (70% width): live pane content in bordered box
- `tea.Tick` every 500ms refreshes preview via `tmux capture-pane -t <session> -p -e`
- Metadata line below preview: current directory, running command, pane count
- Key bindings:
  - `j/k` or arrows: navigate sessions
  - `enter`: `tmux.AttachOrSwitch` to highlighted session
  - `tab`: cycle panes within previewed session
  - `q/esc`: quit
- Direct mode (`tsp peek backend`): skip TUI, capture + print once and exit
- Check `$TMUX` is set for interactive mode

Tests in `internal/cmd/peek_test.go`:
- Model `Update` with synthetic `tea.KeyMsg` (navigation, quit, enter)
- Model `View` renders two panels at given dimensions
- `tea.WindowSizeMsg` correctly sizes panels
- All tests use mock data, no real tmux

## File Summary

New files:
- `internal/tmux/tmux.go` + `tmux_test.go`
- `internal/pathutil/pathutil.go` + `pathutil_test.go`
- `config/config_test.go`
- `internal/cmd/helpers.go` + `helpers_test.go`
- `internal/cmd/project_creator.go`
- `internal/cmd/middle.go` + `middle_test.go`
- `internal/cmd/peek.go` + `peek_test.go`

Modified files:
- `config/config.go` — use `os.UserHomeDir()`, env var expansion, testable config path
- `internal/cmd/dir.go` — use pathutil, tmux package, remove inlined attach logic
- `internal/cmd/list.go` — use tmux package, remove `attachToSession`
- `internal/cmd/sandbox.go` — thin wrapper around project_creator
- `internal/cmd/project.go` — thin wrapper around project_creator
- `internal/cmd/gtmux.go` — use tmux package, configurable worktree path
- `internal/cmd/gtwremove.go` — fix parallelization, fix worktree detection, use tmux package
- `internal/cmd/root.go` — register `middle` and `peek` commands
- `CLAUDE.md` — fix doc drift
