# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project Overview

tmux-super-powers (`tsp`) is a Go CLI tool for managing tmux sessions, deploying parallel AI agents across git worktrees, and providing remote access via an HTTP/WebSocket API with device pairing. Built with bubbletea for interactive TUIs and cobra for CLI structure.

## Development Commands

```bash
go build -o tsp ./cmd/tsp          # Build
go install ./cmd/tsp               # Install locally
go test ./...                      # Run tests
go mod tidy                        # Clean dependencies
```

## Architecture

### Command Structure (`internal/cmd/`)

| Command | File | Purpose |
|---------|------|---------|
| `tsp list` (alias: `txl`) | `list.go` | Interactive session picker |
| `tsp dir` | `dir.go` | Directory selection with glob support |
| `tsp new` | `new.go` | Project creation (consolidates sandbox + project) |
| `tsp sandbox` | `sandbox.go` | Sandbox project creation |
| `tsp project` | `project.go` | Project creation |
| `tsp config` | `config.go` | Open config in editor |
| `tsp wtx-new` | `wtx_new.go` | Create git worktrees with tmux sessions |
| `tsp wtx-here` | `wtx_here.go` | Create session in current repo |
| `tsp wtx-rm` | `wtx_rm.go` | Interactive worktree removal |
| `tsp spawn` | `spawn.go` | Deploy AI agents in parallel worktrees |
| `tsp dash` | `dash.go` | Real-time session dashboard |
| `tsp harvest` | `harvest.go` | Review diffs, PRs, CI, reviews |
| `tsp rm` | `rm.go` | Session removal with worktree detection |
| `tsp serve` | `serve.go` | HTTP/WebSocket API server |
| `tsp serve status` | `serve_status.go` | Daemon health check |
| `tsp device pair` | `device_pair.go` | QR-code device pairing |
| `tsp device list` | `device_list.go` | List paired devices |
| `tsp device revoke` | `device_revoke.go` | Revoke device access |
| `tsp version` | `version.go` | Version info |

### Key Packages

| Package | Purpose |
|---------|---------|
| `config/` | YAML config at `~/.tsp/config.yaml`, auto-migration from old path, defaults |
| `internal/cmd/` | Cobra command definitions, TUI models |
| `internal/tmux/` | Tmux operations (sessions, panes, send-keys, capture) |
| `internal/service/` | Business logic (spawn, sessions, monitor, PR/CI, diff) |
| `internal/server/` | HTTP handlers, WebSocket, embedded web UI |
| `internal/auth/` | Token middleware, admin token management |
| `internal/device/` | Device store, pairing flow, token generation |
| `internal/pathutil/` | Tilde expansion, path helpers |

### API Endpoints (`internal/server/`)

Routes registered in `server.go`, handlers in `handlers.go`:

| Method | Path | Purpose |
|--------|------|---------|
| GET | `/api/health` | System health (tmux, gh) |
| GET | `/api/config` | Current configuration |
| GET | `/api/directories` | Resolved directory list |
| GET | `/api/sessions` | List all sessions with live data |
| GET | `/api/sessions/{name}` | Session detail with PR/diff |
| POST | `/api/sessions` | Create session (name, dir, leftCmd, rightCmd) |
| DELETE | `/api/sessions/{name}` | Delete session (optional worktree cleanup) |
| POST | `/api/sessions/{name}/send` | Send text to pane |
| POST | `/api/spawn` | Deploy agents (tasks, base, dir) |
| GET | `/api/sessions/{name}/pr` | Get PR info |
| POST | `/api/sessions/{name}/pr` | Create PR |
| POST | `/api/sessions/{name}/fix-ci` | Fetch failing CI logs, send to agent |
| POST | `/api/sessions/{name}/fix-reviews` | Fetch review comments, send to agent |
| POST | `/api/sessions/{name}/merge` | Merge PR |
| POST | `/api/pair/initiate` | Start pairing (returns 6-char code) |
| POST | `/api/pair/complete` | Complete pairing (returns token) |
| GET | `/api/pair/status` | Poll pairing status |
| GET | `/api/ws` | WebSocket for real-time session streaming |

Auth: Bearer token in `Authorization` header or `?token=` query param for WebSocket. Pairing endpoints are unauthenticated.

### Spawn Flow

1. Resolves git repo root and base branch
2. For each task: creates branch (`spawn/{task-slug}-{suffix}`), worktree, installs deps
3. Creates two-pane tmux session (nvim left, agent right)
4. Task passed as CLI argument to agent command — no send-keys needed

### Session Monitoring (`internal/service/monitor.go`)

- Polls tmux sessions and captures pane content
- Infers status: **active** (content changing), **idle** (no changes), **done** (prompt detected), **error** (error pattern matched)
- Broadcasts updates to WebSocket subscribers
- Configurable patterns in `dash` config section

### Tmux Integration (`internal/tmux/`)

- `SendKeys(target, text)` — chunked `send-keys -l` with newline collapsing, then Enter
- `CreateTwoPaneSession(name, dir, leftCmd, rightCmd)` — session + horizontal split
- `AttachOrSwitch(name)` — `switch-client` inside tmux, `attach-session` outside
- Session naming: `{repo-name}-{branch}` with `.` and `:` replaced by `-`

### Device Pairing (`internal/device/`, `internal/auth/`)

- 6-character pairing codes (charset of 30, 5-minute expiry)
- Tokens generated with `crypto/rand`, stored in `~/.tsp/devices.json` (mode 0600)
- Admin token in `~/.tsp/admin-token` (mode 0600)
- QR codes via `skip2/go-qrcode`

### Embedded Web UI (`internal/server/web/index.html`)

Single-page vanilla JS app with tabs: Sessions (live monitoring), Create (new sessions), Spawn (deploy agents). Includes pairing screen, WebSocket real-time updates, and full session management actions.

## Configuration

Stored at `~/.tsp/config.yaml` (auto-created, migrated from `~/.tmux-super-powers.yaml`):

```yaml
directories:
  - ~/projects
  - ~/work/**                    # ** for multi-level glob

ignore_directories:
  - node_modules

sandbox:
  path: ~/sandbox

projects:
  path: ~/projects

editor: $EDITOR

dash:
  refresh_ms: 500
  error_patterns: [FAIL, panic:, Error:]
  prompt_pattern: '\$\s*$'

spawn:
  worktree_base: ~/work/code
  agent_command: claude --dangerously-skip-permissions

serve:
  port: 7777
  bind: ""                       # Auto-detects Tailscale IP
  refresh_ms: 500
```

## Development Principles

- **Just-In-Time Development**: Build only what's needed, avoid speculative features
- **Single Purpose Commands**: Each command does one thing well
- **Consistent TUI Patterns**: All interactive elements use bubbletea
- **Minimal Dependencies**: Keep the dependency tree small
- **Fast Execution**: Commands should be responsive and quick to start

## Key Dependencies

- `charmbracelet/bubbletea` + `bubbles` + `lipgloss` — TUI framework and styling
- `spf13/cobra` — CLI framework
- `gorilla/websocket` — WebSocket support
- `gopkg.in/yaml.v3` — Configuration parsing
- `skip2/go-qrcode` — QR code generation for device pairing
