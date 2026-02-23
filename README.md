# tmux-super-powers (`tsp`)

A CLI tool for managing tmux sessions, deploying parallel AI agents across git worktrees, and controlling everything from your phone.

## Installation

```bash
go install github.com/matteo-hertel/tmux-super-powers/cmd/tsp@latest
```

Or build from source:

```bash
git clone https://github.com/matteo-hertel/tmux-super-powers.git
cd tmux-super-powers
go install ./cmd/tsp
```

## What It Does

### Session & Directory Management

```bash
tsp list              # Interactive session picker (attach/switch)
tsp dir               # Open directories from config with filtering
tsp new myapp         # Create a new project with a tmux session
tsp rm                # Interactive multi-select session removal
tsp config            # Edit configuration in your $EDITOR
```

### Git Worktrees

```bash
tsp wtx-new feat auth # Create worktrees + tmux sessions for branches
tsp wtx-here          # Create session in the current repo
tsp wtx-rm            # Interactive worktree removal (session + branch + directory)
```

Each worktree session gets a two-pane layout: nvim (left) and claude (right).

### Parallel AI Agents

Deploy multiple Claude Code agents working on separate tasks in isolated worktrees:

```bash
tsp spawn "fix auth bug" "add dark mode" "write tests" --dash
```

Each task gets its own branch, worktree, and tmux session with Claude Code auto-started on the task. The `--dash` flag opens the dashboard to monitor progress.

```bash
tsp spawn --file tasks.txt --base main    # Read tasks from file
tsp spawn "task" --no-install --dry-run   # Preview without executing
```

### Mission Control Dashboard

```bash
tsp dash
```

Real-time monitoring of all sessions with live status inference (active/idle/done/error). Key bindings:

- `d` toggle diff view, `c` send follow-up prompt, `p` create PR
- `f` fix CI (fetches failing logs, sends to agent), `r` address review comments
- `m` merge PR, `x` kill session, `Enter` jump to session

### API Server & Mobile Access

```bash
tsp serve                # Start HTTP/WebSocket server (auto-detects Tailscale IP)
tsp serve --install      # Install as launchd daemon (auto-start on login)
tsp serve status         # Check daemon and server health
```

Exposes REST API and WebSocket at port 7777 with an embedded web dashboard. Manage sessions, spawn agents, create PRs, and fix CI from your phone.

### Device Pairing

```bash
tsp device pair --name "My iPhone"   # Display QR code for pairing
tsp device list                      # List paired devices
tsp device revoke <id|name>          # Revoke access
```

Token-based auth with secure pairing flow. Paired devices get full API access.

### The Full Workflow

```bash
tsp spawn "task1" "task2" --dash   # 1. Deploy agents
tsp dash                           # 2. Monitor in real-time
# Use dash keys: p (PR), f (fix CI), r (fix reviews), m (merge)
tsp rm                             # 3. Clean up
```

## Configuration

Stored at `~/.tsp/config.yaml` (auto-created with defaults):

```yaml
directories:
  - ~/projects
  - ~/work/**          # ** for multi-level depth

spawn:
  worktree_base: ~/work/code
  agent_command: claude --dangerously-skip-permissions

serve:
  port: 7777

editor: $EDITOR
```

## Requirements

- Go 1.24+
- tmux
- `gh` CLI (for PR/CI features)
