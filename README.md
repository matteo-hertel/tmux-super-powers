# tmux-super-powers

Enhanced tmux functionality with interactive TUI using bubbletea.

## Installation

```bash
go install github.com/matteo-hertel/tmux-super-powers@latest
```

Or build from source:

```bash
git clone https://github.com/matteo-hertel/tmux-super-powers.git
cd tmux-super-powers
go build -o tsp
```

## Usage

### List and attach to tmux sessions
```bash
tsp
# or
tsp list
```

### Open directory from configured list (with filtering)
```bash
tsp dir
```

### Create new sandbox project
```bash
tsp sandbox
```

### Git worktree commands
```bash
tsp wtx-new branch1 branch2    # Create worktrees with tmux sessions
tsp wtx-here                   # Create session in current repo
tsp wtx-rm                     # Remove worktrees interactively
```

### Edit configuration
```bash
tsp config
```

## Configuration

Configuration is stored in `~/.tmux-super-powers.yaml`:

```yaml
directories:
  - ~/projects
  - ~/work
  - ~/personal
  - ~/code/sandbox/*  # Glob patterns supported

sandbox:
  path: ~/sandbox
  
editor: $EDITOR  # Falls back to vim if not set
```

## Features

- **Session Management**: List all tmux sessions with interactive selection
- **Quick Directory Access**: Open configured directories with filtering support
- **Git Worktree Integration**: Create, manage, and remove git worktrees with tmux sessions
- **Sandbox Projects**: Quickly create new sandbox projects with dedicated sessions
- **Interactive Filtering**: Type to filter directories and worktrees
- **Configuration Management**: Edit configuration with your preferred editor

## Requirements

- Go 1.20+
- tmux installed and running
- Terminal with UTF-8 support