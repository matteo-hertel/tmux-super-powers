# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project Overview

tmux-super-powers is a Go CLI tool that enhances tmux functionality using bubbletea for interactive TUIs. The project follows the Just-In-Time Development principle - building only what's needed when it's needed, avoiding speculative features.

## Development Commands

### Build
```bash
go build -o tsp ./cmd/tsp
```

### Install
```bash
go install ./cmd/tsp
```

### Install from remote
```bash
go install github.com/matteo-hertel/tmux-super-powers/cmd/tsp@latest
```

### Dependencies
```bash
go mod tidy
```

### Test (add tests as needed)
```bash
go test ./...
```

## Architecture

### Command Structure
The CLI uses cobra for command management with the following structure:
- `tsp` (default) - Shows help/usage information
- `tsp list` (alias: `txl`) - List and select tmux sessions
- `tsp dir` - Directory selection (supports globs and ** for multi-level depth)
- `tsp sandbox` - Sandbox project creation
- `tsp project` - New project creation
- `tsp config` - Configuration editing
- `tsp wtx-new branch1 branch2 ...` - Create git worktrees with tmux sessions
- `tsp wtx-here` - Create tmux session in current git repository
- `tsp wtx-rm` - Interactive worktree removal with multi-select
- `tsp dash` - Real-time session dashboard (mission control)
- `tsp spawn` - Deploy multiple AI agents in parallel worktrees
- `tsp harvest` - Review diffs, create PRs, fix CI, address review comments
- `tsp new` - Create new project (consolidates sandbox + project)
- `tsp rm` - Remove sessions with smart worktree detection

### Key Components

**Main Entry Point**: `main.go` calls `cmd.Execute()`

**Command Router**: `internal/cmd/root.go` defines the root command and registers all subcommands. The default behavior shows help/usage information.

**Configuration System**: `config/config.go` handles YAML configuration stored at `~/.tmux-super-powers.yaml`. The config automatically creates defaults if the file doesn't exist and falls back to environment variables (like `$EDITOR`).

**Interactive TUI Pattern**: Each command that requires user interaction follows the same bubbletea pattern:
1. Create a model struct with the necessary state
2. Implement `Init()`, `Update()`, and `View()` methods
3. Use appropriate bubbles components (list, textinput)
4. Handle tea.KeyMsg for navigation and selection

### Tmux Integration
- **Session Management**: Uses `switch-client` when inside tmux, `attach-session` when outside
- **Directory Command**: Creates new tmux sessions with a two-pane layout (nvim on left, terminal on right) similar to the `twosplit` function
- **Git Worktree Commands**: Create sessions with neovim (left) and claude (right) panes
- **Session Detection**: Checks `$TMUX` environment variable to determine if running inside tmux
- **Session Creation**: Uses `has-session` to check if session exists before creating
- **Session Naming**: Git worktree sessions use `{repo-name}-{branch}` format

### Path Handling
The application handles tilde expansion (`~/`) in configuration paths and uses `filepath.Join()` for cross-platform compatibility.

## Configuration Format

User configuration is stored in `~/.tmux-super-powers.yaml`:

```yaml
directories:
  - ~/projects
  - ~/work
  - ~/personal
  - ~/code/sandbox/*  # Glob patterns supported
  - ~/deep-dirs/**   # Multi-level depth with **

sandbox:
  path: ~/sandbox

projects:
  path: ~/projects
  
editor: $EDITOR  # Falls back to vim if not set
```

### Directory Configuration
- Supports glob patterns with `*` to match multiple directories (one level deep)
- Supports `**` for multi-level directory traversal (up to 2 levels deep)
- Mixed static paths, single-level globs (`*`), and multi-level globs (`**`) are supported

## Development Principles

- **Just-In-Time Development**: Add features only when needed, avoid speculative complexity
- **Single Purpose Commands**: Each command does one thing well
- **Consistent TUI Patterns**: All interactive elements use bubbletea consistently
- **Minimal Dependencies**: Keep the dependency tree small and focused
- **Fast Execution**: Commands should be responsive and quick to start

### Parallel Agent Workflow
1. `tsp spawn "task1" "task2" --dash` — deploy agents
2. `tsp dash` — monitor in real-time
3. `tsp harvest` — review diffs, create PRs, fix CI, merge
4. `tsp rm` — clean up remaining sessions

## Key Dependencies

- `github.com/charmbracelet/bubbletea` - TUI framework
- `github.com/charmbracelet/bubbles` - Pre-built TUI components
- `github.com/charmbracelet/lipgloss` - Terminal styling
- `github.com/spf13/cobra` - CLI framework
- `gopkg.in/yaml.v3` - YAML configuration parsing