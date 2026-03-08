# Tmux Duck Design

Inspired by [duck.nvim](https://github.com/tamton-aquib/duck.nvim). Spawn 🦆 that wander around a tmux popup.

## Commands

| Command | Purpose |
|---------|---------|
| `tsp duck` | Open tmux popup showing the duck pond |
| `tsp duck new` | Add a duck (auto-starts daemon) |
| `tsp duck cook` | Remove last-added duck (auto-stops daemon when none left) |

Tmux keybinding: `prefix + d` → `run-shell "tsp duck"`

## Architecture

```
┌─────────────┐     unix socket      ┌──────────────┐
│  tsp duck   │◄────────────────────►│  duck daemon  │
│  (popup)    │   stream positions    │  (~/.tsp/     │
│  bubbletea  │                      │   duck.sock)  │
└─────────────┘                      └──────┬───────┘
                                            │
┌─────────────┐     unix socket             │ tick
│ tsp duck new│─── "add" ──────────────────►│ ~200ms
│ tsp duck cook── "remove" ────────────────►│
└─────────────┘                             │
                                       ┌────▼─────┐
                                       │ duck state│
                                       │ in memory │
                                       └──────────┘
```

### Daemon (`internal/duck/daemon.go`)

- Starts on first `tsp duck new`, writes PID to `~/.tsp/duck.pid`
- Listens on `~/.tsp/duck.sock`
- Accepts JSON commands: `{"cmd":"add"}`, `{"cmd":"remove"}`, `{"cmd":"subscribe","width":N,"height":N}`
- Ticks every 200ms, updates duck positions
- Broadcasts positions to subscribers as JSON: `{"ducks":[{"id":1,"x":10,"y":5}]}`
- Auto-exits when last duck removed and no subscribers connected

### Duck State

Each duck:
- `id int` — sequential, used for LIFO removal
- `x, y float64` — position
- `dx, dy float64` — velocity (cells per tick)

Movement:
- Speed: 1-2 cells per tick at 200ms
- Direction changes randomly every 5-15 ticks
- Bounces off canvas edges
- Canvas size set by subscriber's reported dimensions
- Default canvas 80x24 when no subscriber connected
- Emoji `🦆` is 2 cells wide, accounted for in x-boundary

### Popup Viewer (`internal/duck/viewer.go`)

- Bubbletea program connecting to `~/.tsp/duck.sock`
- Sends `subscribe` with terminal dimensions on connect
- Receives position updates, renders `🦆` at each coordinate
- `q` / `Esc` to close (sends unsubscribe, closes connection)
- Blank background

### CLI Commands (`internal/cmd/duck.go`)

- `duckCmd` — parent command, runs `tmux display-popup -E "tsp duck --viewer"` (80% width/height)
- `duckNewCmd` — connects to socket, sends `add`, prints confirmation. If socket missing, fork-execs daemon first
- `duckCookCmd` — connects to socket, sends `remove`, prints confirmation
- Hidden `--viewer` flag on `duckCmd` runs the bubbletea viewer directly (used inside the popup)

### Socket Protocol

Newline-delimited JSON over unix socket.

**Client → Daemon:**
```json
{"cmd":"add"}
{"cmd":"remove"}
{"cmd":"subscribe","width":80,"height":24}
{"cmd":"unsubscribe"}
```

**Daemon → Subscriber:**
```json
{"ducks":[{"id":1,"x":10,"y":5},{"id":2,"x":30,"y":12}]}
```

### File Locations

| File | Purpose |
|------|---------|
| `~/.tsp/duck.sock` | Unix socket |
| `~/.tsp/duck.pid` | Daemon PID file |

### Tmux Config

User adds to `~/.tmux.conf`:
```
bind d run-shell "tsp duck"
```
