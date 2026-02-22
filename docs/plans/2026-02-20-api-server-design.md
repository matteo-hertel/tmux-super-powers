# TSP API Server Design

**Date:** 2026-02-20
**Status:** Approved

## Purpose

Expose all tsp functionality as HTTP/WebSocket API endpoints so a native mobile app (connected via Tailscale) can:

- Review sessions (live pane content, status, diffs, PR info)
- Start sessions (spawn agents, create worktrees)
- Fix CI / address review comments (fire-and-forget commands to agent panes)

## Decisions

- **HTTP stack:** Go `net/http` (stdlib) + `gorilla/websocket`
- **Security:** Bind to Tailscale interface only (100.x.x.x CGNAT range)
- **Server mode:** Always-on daemon via `tsp serve` with launchd integration
- **Real-time:** WebSocket for live session streaming
- **CI/Review actions:** Fire-and-forget (send prompt to agent pane, monitor via WebSocket)

## Architecture

```
┌──────────────┐     Tailscale      ┌──────────────────────────────────┐
│  Mobile App  │◄──────────────────►│  tsp serve (daemon)              │
│  (native)    │    JSON + WS       │                                  │
└──────────────┘                    │  ┌────────────┐  ┌────────────┐  │
                                    │  │ REST API   │  │ WebSocket  │  │
                                    │  │ (net/http) │  │ (gorilla)  │  │
                                    │  └─────┬──────┘  └─────┬──────┘  │
                                    │        │               │         │
                                    │  ┌─────▼───────────────▼──────┐  │
                                    │  │    Service Layer            │  │
                                    │  │  (business logic, polling)  │  │
                                    │  └─────┬──────────────────────┘  │
                                    │        │                         │
                                    │  ┌─────▼──────┐  ┌───────────┐  │
                                    │  │ tmux pkg   │  │ gh CLI    │  │
                                    │  │ (existing) │  │ (existing) │  │
                                    │  └────────────┘  └───────────┘  │
                                    └──────────────────────────────────┘
```

### New Packages

- `internal/server/` — HTTP server, route registration, WebSocket hub, middleware
- `internal/service/` — Business logic extracted from cmd helpers (session monitoring, git ops, PR ops)

### Service Layer

Extracts business logic from `internal/cmd/` into reusable functions:

- **Session monitoring:** List sessions, capture pane content, infer status
- **Git operations:** Detect repos/worktrees, get diffs, branch management
- **PR operations:** Find PRs, CI status, review comments, create PRs
- **Spawn operations:** Task parsing, worktree creation, agent deployment

Both the TUI commands and API server consume the same service layer.

## API Endpoints

### Sessions

| Method | Path | Description |
|--------|------|-------------|
| `GET` | `/api/sessions` | List all sessions with status, git info, branch, pane types |
| `GET` | `/api/sessions/{name}` | Single session detail (pane content, diff stats, PR info) |
| `POST` | `/api/sessions` | Create a new session (name, directory, type) |
| `DELETE` | `/api/sessions/{name}` | Kill session (optionally cleanup worktree) |
| `POST` | `/api/sessions/{name}/send` | Send keys/command to a specific pane |

### Spawn

| Method | Path | Description |
|--------|------|-------------|
| `POST` | `/api/spawn` | Deploy agents with tasks (same as `tsp spawn`) |

### PR/CI Operations

| Method | Path | Description |
|--------|------|-------------|
| `GET` | `/api/sessions/{name}/pr` | Get PR info (number, URL, CI status, review count) |
| `POST` | `/api/sessions/{name}/pr` | Create PR for session's branch |
| `POST` | `/api/sessions/{name}/fix-ci` | Fetch failing CI logs, send fix prompt to agent |
| `POST` | `/api/sessions/{name}/fix-reviews` | Fetch review comments, send fix prompt to agent |
| `POST` | `/api/sessions/{name}/merge` | Merge the PR |

### System

| Method | Path | Description |
|--------|------|-------------|
| `GET` | `/api/health` | Health check (tmux running, Tailscale connected) |
| `GET` | `/api/config` | Get current tsp config |

### WebSocket

| Path | Description |
|------|-------------|
| `ws://host/api/ws` | Real-time stream of all session states |

#### WebSocket Payload

Pushed every refresh cycle (default 500ms, configurable via `dash.refreshMs`):

```json
{
  "sessions": [
    {
      "name": "my-project-feat-auth",
      "status": "active",
      "branch": "feat-auth",
      "isWorktree": true,
      "lastChanged": "2026-02-20T14:30:00Z",
      "panes": [
        { "index": 0, "type": "editor", "process": "nvim" },
        { "index": 1, "type": "agent", "status": "active", "content": "..." }
      ],
      "diff": { "files": 3, "insertions": 45, "deletions": 12 },
      "pr": { "number": 42, "url": "...", "ciStatus": "pass", "reviewCount": 2 }
    }
  ]
}
```

## Pane Type Detection

Each pane is classified for the mobile app to render appropriately:

| Type | Detection | Mobile App Behavior |
|------|-----------|---------------------|
| `agent` | Right-side pane of worktree session, or content matches AI patterns | Full monitoring: live content, status inference, send commands |
| `editor` | Process is nvim/vim/emacs (detected from pane process or title) | "Editor running" badge, no content streaming |
| `shell` | Idle shell with prompt visible | "Idle shell" with last command, allow sending commands |
| `process` | Running process (npm, tests, build) | Live output, status inference |

## Server Command

```
tsp serve              # Start on default port (7777), bind to Tailscale IP
tsp serve --port 8080  # Custom port
tsp serve --bind 0.0.0.0  # Override bind address
```

### Tailscale IP Detection

On startup:
1. Enumerate network interfaces
2. Find address in 100.64.0.0/10 range (Tailscale CGNAT)
3. If found, bind to that IP
4. If not found, fall back to 127.0.0.1 with warning

### Launchd Integration

```
tsp serve --install    # Write plist + start daemon
tsp serve --uninstall  # Stop daemon + remove plist
```

Plist location: `~/Library/LaunchAgents/com.tsp.serve.plist`

Behavior:
- Starts on login
- Restarts on crash
- Logs to `~/.tsp/serve.log`

### Background Monitoring Loop

A goroutine runs continuously while the server is up:
1. List all tmux sessions every refresh cycle
2. Capture pane content for each session
3. Detect pane types (editor, agent, shell, process)
4. Run `inferStatus()` for relevant panes
5. Lazily enrich with git/PR data on first discovery or on request
6. Push updates to all connected WebSocket clients

## Error Handling

| Condition | Behavior |
|-----------|----------|
| tmux not running | `/api/health` returns 503 `{"tmux": false}`. All other endpoints return 503. |
| Session not found | 404 `{"error": "session not found"}` |
| gh CLI not available | PR endpoints return 501 `{"error": "gh CLI not installed"}` |
| Tailscale not connected | Server warns and binds to localhost |
| WebSocket disconnect | Server cleans up stale connections after 30s timeout |

## Testing Strategy

**Unit tests:**
- Service layer: pure functions (status inference, pane detection, git parsing)
- API handlers: `httptest.NewServer` with mocked service
- WebSocket: connection upgrade, message format

**Integration tests (manual/local):**
- Require running tmux session
- Hit API, verify response matches session state

## Dependencies Added

- `github.com/gorilla/websocket` — WebSocket support (single, focused dependency)
