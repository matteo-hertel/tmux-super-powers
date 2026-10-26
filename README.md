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
tsp dash                # Jump between sessions and manage agents
tsp config              # Edit ~/.tsp/config.yaml
tsp -v                  # Print the version
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

The manager lists every tmux session collapsed, previewing its first pane.
Press `l` on a session to see its agents and `h` to fold it again. Press `/` to
filter the roster by session name, agent task, branch, or path; `Enter` keeps
the query and `Esc` clears it. Agents `tsp` spawned stay listed after they exit
so you can clean their worktree.
It takes a snapshot only when it opens or when you press `r`; it does
not poll pane output or CI. When it opens, it also finishes any worktree
removal an earlier run started but did not complete.

| Key | Action |
|---|---|
| `o` | Open a configured project in its tmux session |
| `n` | Spawn an agent in a project |
| `Enter` | Attach to the selected tmux session and pane |
| `s` | Interrupt the agent process with `Ctrl-C` |
| `x` | Remove the session and its managed worktree/branch |
| `r` | Refresh the agent and output snapshot |
| `j` / `k` | Move through the roster |
| `h` / `l` | Collapse or expand a session's agents |
| `/` | Filter sessions by name, agent task, branch, or path |
| `?` | Show help |

Dashboard Open uses the directories configured for `tsp dir`. Dashboard New
lets you choose Claude or Codex and set the base branch before the worktree is
created.

New sessions use the same layout as `twosplit`: the configured primary agent
runs in the left 80% and an empty shell runs in the right 20%. When the agent
exits or you interrupt it, its pane stays open as a shell. `tsp` recognizes
Claude Code, Codex, and Aider processes. Agents created by
`tsp spawn` are recorded in `~/.tsp/agent-runs.json`, so completed agents remain
listed until you clean their worktree, branch, and session with `x`.

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
```

`spawn.agent_command` is the default primary agent used by `dir`, `project`,
`spawn`, and dashboard New. The Claude and Codex spawn commands power the
provider switch in dashboard New.

Old `dash`, `serve`, `watcher`, `sandbox`, and `manager` keys are safely
ignored by the YAML loader.

## Requirements

- Go 1.26+
- tmux
- Git for worktree-based agents
- At least one configured agent CLI, such as `claude` or `codex`

## Develop

```bash
go build -o tsp ./cmd/tsp
go test ./...
go vet ./...
```

See [the command audit](docs/command-audit.md) for what is used, what was
removed, and why.
