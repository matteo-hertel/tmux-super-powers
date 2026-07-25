# tmux-super-powers agent guide

Keep this file and `CLAUDE.md` aligned.

## Product

`tsp` is a Go CLI for project navigation, tmux sessions, git worktrees, and
local coding-agent management. It intentionally has no mobile app, HTTP server,
device pairing, CI watcher, or background polling.

The product loop is:

1. Find or create a project with `tsp dir` / `tsp project`.
2. Spawn isolated coding agents with `tsp spawn`.
3. Delegate follow-up jobs from the on-demand `tsp dash` roster.
4. Attach, interrupt, or clean parent/child tmux and worktree resources explicitly.

## Architecture

```text
cmd/tsp/main.go
└── internal/cmd/            Cobra commands and Bubble Tea TUIs
    ├── dash.go              Parent/child agent roster and direct controls
    ├── spawn.go             CLI adapter for agent creation
    ├── dir.go/project.go    Project navigation and creation
    └── gtmux*.go/rm.go      Worktree and tmux lifecycle

config/config.go             ~/.tsp/config.yaml load/default/repair
internal/service/
├── spawn.go                 Branch/worktree/session and in-place delegation
├── agentrun.go              Durable parent/child run registry
└── sessions.go              One-shot tmux/process/git inspection and actions
internal/tmux/tmux.go        Tmux command boundary
internal/pathutil/           Path expansion and directory helpers
```

The dashboard does not own a timer. It discovers processes and captures output
once at startup and once per explicit refresh/action.

## CLI surface

| Command | Purpose |
|---|---|
| `tsp dir` | Select and open a configured directory. |
| `tsp project` | Create a project and tmux session. |
| `tsp list` | Select any tmux session. |
| `tsp spawn` | Create managed coding agents. |
| `tsp dash` | Spawn, delegate, attach, interrupt, clean, and refresh agents. |
| `tsp rm` | Remove sessions with worktree awareness. |
| `tsp cleanup` | Remove orphaned worktree-base entries. |
| `tsp wtx-new` | Create manual worktree sessions. |
| `tsp wtx-here` | Create a session for the current repository. |
| `tsp wtx-rm` | Remove manual worktrees. |
| `tsp middle` | Run a command in a tmux popup. |
| `tsp config` | Edit or repair active configuration. |
| `tsp version` | Print build version information. |

See `docs/command-audit.md` before adding back overlapping commands.

## API endpoints

None. `tsp` exposes no network server or remote-control API.

## State

| Path | Purpose |
|---|---|
| `~/.tsp/config.yaml` | User configuration. |
| `~/.tsp/agent-runs.json` | Durable metadata for agents created or observed by the dashboard. |
| `spawn.worktree_base` | Parent directory for generated agent worktrees. |
| `manager.agent_command` | Inexpensive non-interactive agent used for delegated jobs. |

Unknown legacy YAML keys are ignored, so old configurations containing
`dash`, `serve`, `watcher`, or `sandbox` sections still load.

## Development

```bash
go build -o tsp ./cmd/tsp
go install ./cmd/tsp
go test ./...
go vet ./...
go mod tidy
```

Do not start a local server; the project has no server runtime.

## Gotchas

- `tsp dash` must run inside tmux because `Enter` switches the active client.
- `tsp spawn` and dashboard delegation pass prompts as shell-quoted CLI
  arguments. Keep quoting in `internal/service/spawn.go`; do not restore
  follow-up prompt injection with `tmux send-keys`.
- Delegated runs share their parent's retained workspace and never own it.
  Enforce one active writer per workspace, and let only the owning root run
  remove the worktree and branch.
- Manager tasks with clear stop/cleanup intent route to native confirmed TSP
  actions. Do not let a delegated model delete its own workspace.
- One-shot delegated agent panes use tmux `remain-on-exit`; discovery must skip
  dead panes as live processes while preserving their captured final output.
- A managed agent can outlive its process in the roster. This is intentional so
  its worktree can be cleaned explicitly.
- Process names vary: Claude may appear as a semantic version, Codex may use a
  platform-suffixed binary, and agents may be shell children.
- `tsp dash` must remain on-demand. Do not add Bubble Tea ticks, CI polling, or
  content-change status inference.
- Worktree cleanup is destructive. Preserve the confirmation step and resolve
  exact session/worktree/branch targets before calling it.
- `spawn.default_setup` runs through `sh -c` in the new worktree before the
  agent starts; failures leave the workspace for inspection and return an error.
- The default branch is protected by local workflow guidance. Create a feature
  branch before committing changes unless explicitly told to commit on main.
