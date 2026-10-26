# tmux-super-powers agent guide

`CLAUDE.md` imports this file. Edit this one.

## Product

`tsp` is a Go CLI for project navigation, tmux sessions, git worktrees, and
local coding-agent management. It intentionally has no mobile app, HTTP server,
device pairing, CI watcher, or background polling.

The product loop is:

1. Find or create a project with `tsp dir`, dashboard Open, or `tsp project`.
2. Spawn isolated coding agents with `tsp spawn` or dashboard New.
3. Jump between sessions and agents from the on-demand `tsp dash` roster.
4. Attach, interrupt, or clean tmux and worktree resources explicitly.

## Architecture

```text
cmd/tsp/main.go
└── internal/cmd/            Cobra commands and Bubble Tea TUIs
    ├── dash.go              Session and agent roster with direct controls
    ├── spawn.go             CLI adapter for agent creation
    ├── dir.go/project.go    Project navigation and creation
    └── config.go            Config editing and repair

config/config.go             ~/.tsp/config.yaml load/default/repair
internal/service/
├── spawn.go                 Branch, worktree, and session creation
├── agentrun.go              Durable agent run registry
├── sessions.go              One-shot tmux/process/git inspection and actions
└── worktreetrash.go         Staged, detached worktree removal
internal/tmux/tmux.go        Tmux command boundary
internal/pathutil/           Path expansion helpers
```

The TUIs use Charm v2: `charm.land/bubbletea/v2`, `charm.land/bubbles/v2`,
and `charm.land/lipgloss/v2`.

Use `.interface-design/system.md` as the source of truth for dashboard layout,
tokens, hierarchy, reusable controls, and keyboard interaction patterns.

The dashboard does not own a timer. It lists every tmux session, collapsed by
default; `l` expands a session to show its agents and `h` folds it. `/` opens an
inline filter that narrows sessions by name, agent task, branch, or path.
Retained managed runs sit under their closed session. Session rows are not
registry runs. It captures output once at startup and once per explicit
refresh/action.

## CLI surface

| Command | Purpose |
|---|---|
| `tsp dir` | Select and open a configured directory. |
| `tsp project` | Create a project and tmux session. |
| `tsp spawn` | Create managed coding agents. |
| `tsp dash` | Open projects; jump to sessions; spawn, attach, interrupt, clean, and refresh agents. |
| `tsp config` | Edit or repair active configuration. |
| `tsp -v` | Print the build version. |

See `docs/command-audit.md` before adding back a removed command. It records
what Matt uses and why each removed command went.

## API endpoints

None. `tsp` exposes no network server or remote-control API.

## State

| Path | Purpose |
|---|---|
| `~/.tsp/config.yaml` | User configuration. |
| `~/.tsp/agent-runs.json` | Durable metadata for agents created or observed by the dashboard. |
| `spawn.worktree_base` | Parent directory for generated agent worktrees. |
| `spawn.agent_command` | Primary agent used in the left 80% of new sessions. |
| `spawn.claude_command` / `spawn.codex_command` | Provider choices in dashboard New. |

Unknown legacy YAML keys are ignored, so old configurations containing
`dash`, `serve`, `watcher`, `sandbox`, or `manager` sections still load.

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
- The split must survive its agent. `shellCommandArgs` traps SIGINT and
  `exec`s the shell after the command, so an exited or interrupted agent leaves
  a shell in the same pane. Without the trap, Ctrl-C closes the pane.
- `tsp spawn` passes prompts as shell-quoted CLI arguments. Keep quoting in
  `internal/service/spawn.go`; do not inject prompts with `tmux send-keys`.
- Tmux pane indices move when panes open and close. Persist and target the
  stable `#{pane_id}` for managed runs; keep the index only for display and
  legacy registry entries.
- A pane id identifies a run. A managed agent still running its chained
  install/setup command looks like a plain shell, so an observation must never
  mint a second run for a pane a managed run already holds, in either
  direction. `MergePaneDuplicates` folds records left by older versions.
  An agent restarted in the same pane keeps its run. A managed run whose pane
  closed moves onto the agent now running in its session.
- Older registries can hold delegated child runs, marked by `parentRunId`.
  `readAgentRunFile` drops them on load. Loaded as normal runs, they would
  claim their parent's worktree and live pane.
- The registry file is shared. Every process merges the file back in before
  writing, so a long-lived `tsp dash` cannot overwrite runs a concurrent
  `tsp spawn` created.
- Roster snapshots are bounded (`BuildPreviewCaptureArgs`, `capture-pane -S -400`).
  The dashboard reads every pane of every session on each refresh and renders
  only a tail, so never restore `-S -` there.
  Keep command output attached to the pane by passing `$SHELL -c` as explicit
  tmux command arguments.
- A managed agent can outlive its process in the roster. This is intentional so
  its worktree can be cleaned explicitly.
- Process names vary: Claude may appear as a semantic version, Codex may use a
  platform-suffixed binary, and agents run as descendants of the pane shell
  (npm Codex is shell, then node, then the native binary).
- `tsp dash` must remain on-demand. Do not add Bubble Tea ticks, CI polling, or
  content-change status inference.
- Worktree cleanup is destructive. Preserve the confirmation step and resolve
  exact session/worktree/branch targets before calling it.
- Worktree removal never unlinks in the foreground. An agent worktree is
  hundreds of thousands of files, so `KillSession` renames it to a
  `.tsp-trash-*` sibling (constant time) and unlinks it in a detached process.
  Keep the rename on the same filesystem or it silently becomes a copy.
  `tsp dash` sweeps staged directories left by an interrupted run when it opens.
- Cleanup removes the directory whenever the entry sits in a git worktree, not
  only for managed runs. `agentEntry.isWorktree` comes from
  `DetectSessionGitInfoFull`; never infer a removable worktree from a pane cwd,
  or `x` would delete a plain checkout.
- Session rows never own a workspace. `x` on a session row kills only the tmux
  session; worktree removal stays on the agent row.
- `tsp spawn` must not block on dependency install or setup. Dependency
  install and `spawn.default_setup` are chained ahead of the agent inside its
  own pane, so spawning returns once the worktree and session exist. A failing
  step stops the agent and drops the pane to a shell for inspection.
- The default branch is protected by local workflow guidance. Create a feature
  branch before committing changes unless explicitly told to commit on main.
