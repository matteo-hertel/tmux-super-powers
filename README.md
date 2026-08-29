# tmux-super-powers (`tsp`)

`tsp` is a focused CLI for moving between projects, managing tmux sessions and
git worktrees, and running local coding agents.

It does not run a web server, pair mobile devices, or monitor CI. Claude Code,
Codex, and other terminal agents stay in their native clients while `tsp`
manages the local tmux processes and workspaces around them.

## Install

```bash
go install github.com/matteo-hertel/tmux-super-powers/cmd/tsp@latest
```

Or build from source:

```bash
git clone https://github.com/matteo-hertel/tmux-super-powers.git
cd tmux-super-powers
go install ./cmd/tsp
```

## Everyday commands

```bash
tsp dir                 # Find a configured directory and open its tmux session
tsp project             # Create a project and tmux session
tsp list                # Select any tmux session
tsp rm                  # Remove sessions, with optional worktree cleanup
tsp config              # Edit ~/.tsp/config.yaml
```

## Agent manager

Spawn one or more agents in isolated worktrees:

```bash
tsp spawn "fix the auth bug" "add dark mode"
tsp spawn --agent "codex --full-auto" "refactor the parser"
tsp spawn --dir ~/code/project --file tasks.txt
tsp spawn --base main --dash "implement user avatars"
```

Then open the manager from inside tmux:

```bash
tsp dash
```

The manager shows every live tmux session and marks sessions without an agent
as idle. It takes a snapshot only when it opens or when you press `r`; it does
not poll pane output or CI. Delegation starts a new child pane in the selected
session and retained workspace instead of injecting keystrokes into the old
process. The task panel lets you pick Claude or Codex and edit the model for
that run. When the delegated agent exits, its pane becomes a live shell and its
full tmux scrollback stays available to the dashboard.
Natural-language lifecycle requests such as `delete this worktree` resolve to
the same exact-target confirmation used by the native Clean control; the model
never owns workspace deletion.

| Key | Action |
|---|---|
| `n` | Spawn an agent in a project |
| `d` | Delegate follow-up work to a child agent |
| `Enter` | Attach to the selected tmux session and pane |
| `s` | Interrupt the agent process with `Ctrl-C` |
| `x` | Remove the session and its managed worktree/branch |
| `r` | Refresh the agent and output snapshot |
| `j` / `k` | Move through the roster |
| `?` | Show help |

Dashboard New lets you choose Claude or Codex and set the base branch before
the worktree is created.

New sessions use the same layout as `twosplit`: the configured primary agent
runs in the left 80% and an empty shell runs in the right 20%. `tsp` recognizes
Claude Code, Codex, and Aider processes. Agents created by
`tsp spawn` are recorded in `~/.tsp/agent-runs.json`, so completed agents remain
available for delegation and deliberate cleanup. Delegated children appear
directly beneath their parent and may run concurrently in the same retained
workspace and tmux session; only the owning root run can remove its worktree,
branch, and session.

## Manual worktrees

```bash
tsp wtx-new feat/auth feat/billing
tsp wtx-here
tsp wtx-rm
```

These remain useful when you want direct worktree control. Their tmux sessions
use the same configured agent and 80/20 layout as the other launch commands.

## Configuration

`tsp` reads `~/.tsp/config.yaml`:

```yaml
directories:
  - ~/projects
  - ~/work/**

ignore_directories:
  - node_modules

projects:
  path: ~/projects

editor: $EDITOR

spawn:
  worktree_base: ~/work/code
  agent_command: claude --dangerously-skip-permissions
  claude_command: claude --dangerously-skip-permissions
  codex_command: codex --full-auto
  default_setup: ""

manager:
  default_agent: claude
  claude:
    command: claude -p --permission-mode auto
    model: haiku
  codex:
    command: codex exec --ephemeral --sandbox workspace-write
    model: gpt-5.6-luna
```

`spawn.agent_command` is the default primary agent used by `dir`, `project`,
`spawn`, dashboard New, `wtx-new`, and `wtx-here`. The Claude and Codex spawn
commands power the provider switch in dashboard New. The `manager` section
controls delegated agents and their default models. The delegation panel can
override the manager agent and model for one run.

Use `tsp config repair` to fill missing active settings. Old `dash`, `serve`,
`watcher`, and `sandbox` keys are safely ignored by the YAML loader.

## Requirements

- Go 1.24+
- tmux
- Git for worktree-based agents
- At least one configured agent CLI, such as `claude` or `codex`

## Develop

```bash
go build -o tsp ./cmd/tsp
go test ./...
go vet ./...
```

See [the command audit](docs/command-audit.md) for the retained and removed CLI
surface.
