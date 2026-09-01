# tmux-super-powers agent guide

Keep this file and `CLAUDE.md` aligned.

## Product

`tsp` is a Go CLI for project navigation, tmux sessions, git worktrees, and
local coding-agent management. It intentionally has no mobile app, HTTP server,
device pairing, CI watcher, or background polling.

The product loop is:

1. Find or create a project with `tsp dir`, dashboard Open, or `tsp project`.
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

Use `.interface-design/system.md` as the source of truth for dashboard layout,
tokens, hierarchy, reusable controls, and keyboard interaction patterns.

The dashboard does not own a timer. It lists every live tmux session, discovers
agent processes, and captures output once at startup and once per explicit
refresh/action.

## CLI surface

| Command | Purpose |
|---|---|
| `tsp dir` | Select and open a configured directory. |
| `tsp project` | Create a project and tmux session. |
| `tsp list` | Select any tmux session. |
| `tsp spawn` | Create managed coding agents. |
| `tsp dash` | Open projects; browse sessions; spawn, delegate, attach, interrupt, clean, and refresh agents. |
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
| `~/.tsp/delegate-output/` | Final bounded scrollback from completed delegated panes. |
| `spawn.worktree_base` | Parent directory for generated agent worktrees. |
| `spawn.agent_command` | Primary agent used in the left 80% of new sessions. |
| `spawn.claude_command` / `spawn.codex_command` | Provider choices in dashboard New. |
| `manager.default_agent` | Default Claude or Codex provider for delegated jobs. |
| `manager.claude` / `manager.codex` | Command and default model for each delegated provider. |

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
- Dashboard Open must reuse `expandDirectories` and `ensureDirectorySession`.
  Do not add a second directory discovery or session launch path.
- Every session launcher uses the `twosplit` shape: the configured primary
  agent in pane 0 at 80%, and an empty shell in pane 1 at 20%.
- `tsp spawn` and dashboard delegation pass prompts as shell-quoted CLI
  arguments. Keep quoting in `internal/service/spawn.go`; do not restore
  follow-up prompt injection with `tmux send-keys`.
- Delegated runs open as new panes in their parent's tmux session. They share
  the retained workspace and never own either resource. Parent and child agents
  may run concurrently; let only the owning root run remove the worktree,
  branch, and session.
- Manager tasks with clear stop/cleanup intent route to native confirmed TSP
  actions. Do not let a delegated model delete its own workspace.
- A delegated command captures its bounded tmux scrollback before exiting, then
  its temporary pane closes. Keep the stored output until the run is cleaned.
- Tmux pane indices move when panes open and close. Persist and target the
  stable `#{pane_id}` for managed runs; keep the index only for display and
  legacy registry entries.
- Treat a stopped delegated row without a pane ID as output-only. Its old pane
  index may now belong to a different process.
- Roster snapshots are bounded (`BuildPreviewCaptureArgs`, `capture-pane -S -400`).
  The dashboard reads every pane of every session on each refresh and renders
  only a tail, so never restore `-S -` there. A delegated run's final output
  still captures the full scrollback in `internal/service/spawn.go`.
  Keep command output attached to the pane by passing `$SHELL -c` as explicit
  tmux command arguments.
- A managed agent can outlive its process in the roster. This is intentional so
  its worktree can be cleaned explicitly.
- Process names vary: Claude may appear as a semantic version, Codex may use a
  platform-suffixed binary, and agents may be shell children.
- `tsp dash` must remain on-demand. Do not add Bubble Tea ticks, CI polling, or
  content-change status inference.
- Worktree cleanup is destructive. Preserve the confirmation step and resolve
  exact session/worktree/branch targets before calling it.
- Cleanup removes the directory whenever the entry sits in a git worktree, not
  only for managed runs. `agentEntry.isWorktree` comes from
  `DetectSessionGitInfoFull`; never infer a removable worktree from a pane cwd,
  or `x` would delete a plain checkout.
- `tsp spawn` must not block on dependency install or setup. Dependency
  install and `spawn.default_setup` are chained ahead of the agent inside its
  own pane, so spawning returns once the worktree and session exist. A failing
  step stops the agent and drops the pane to a shell for inspection.
- The default branch is protected by local workflow guidance. Create a feature
  branch before committing changes unless explicitly told to commit on main.
