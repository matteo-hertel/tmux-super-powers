package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/matteo-hertel/tmux-super-powers/config"
	"github.com/matteo-hertel/tmux-super-powers/internal/service"
	tmuxpkg "github.com/matteo-hertel/tmux-super-powers/internal/tmux"
	"github.com/spf13/cobra"
)

var spawnCmd = &cobra.Command{
	Use:   "spawn [flags] task1 task2 ...",
	Short: "Spawn coding agents in isolated worktrees",
	Long: `Create one tmux-hosted coding agent per task.

Git repositories get a unique branch and worktree for every agent. Other
directories get isolated tmux sessions without a worktree. Agents are recorded
in the local roster used by tsp dash.

Examples:
  tsp spawn "fix the auth bug" "add dark mode"
  tsp spawn --agent "codex --full-auto" "refactor the parser"
  tsp spawn --dir ~/code/project --file tasks.txt
  tsp spawn --base main --dash "implement user avatars"
  tsp spawn --dry-run "test task"`,
	Args: cobra.ArbitraryArgs,
	Run: func(cmd *cobra.Command, args []string) {
		taskFile, _ := cmd.Flags().GetString("file")
		baseBranch, _ := cmd.Flags().GetString("base")
		openDash, _ := cmd.Flags().GetBool("dash")
		setup, _ := cmd.Flags().GetString("setup")
		noInstall, _ := cmd.Flags().GetBool("no-install")
		dryRun, _ := cmd.Flags().GetBool("dry-run")
		repoDir, _ := cmd.Flags().GetString("dir")
		agentCommand, _ := cmd.Flags().GetString("agent")

		var tasks []string
		if taskFile != "" {
			data, err := os.ReadFile(taskFile)
			if err != nil {
				fmt.Fprintf(os.Stderr, "Error reading task file: %v\n", err)
				os.Exit(1)
			}
			tasks = parseTaskFile(string(data))
		}
		tasks = append(tasks, args...)
		if len(tasks) == 0 {
			fmt.Fprintln(os.Stderr, "Error: no tasks provided")
			os.Exit(1)
		}

		if repoDir == "" {
			var err error
			repoDir, err = os.Getwd()
			if err != nil {
				fmt.Fprintf(os.Stderr, "Error determining current directory: %v\n", err)
				os.Exit(1)
			}
		}
		repoDir, _ = filepath.Abs(repoDir)

		cfg, err := config.Load()
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error loading config: %v\n", err)
			os.Exit(1)
		}
		if setup != "" {
			cfg.Spawn.DefaultSetup = setup
		}
		if agentCommand != "" {
			cfg.Spawn.AgentCommand = agentCommand
		}

		if dryRun {
			fmt.Printf("Would spawn %d agent(s)\n", len(tasks))
			fmt.Printf("  project: %s\n", repoDir)
			fmt.Printf("  base:    %s\n", firstNonEmpty(baseBranch, "current branch"))
			fmt.Printf("  command: %s\n", cfg.Spawn.AgentCommand)
			for index, task := range tasks {
				fmt.Printf("  %d. %s\n", index+1, task)
			}
			fmt.Println("Each agent receives a unique spawn/* branch, worktree, and tmux session.")
			return
		}

		fmt.Printf("Spawning %d agent(s) with %s…\n\n", len(tasks), cfg.Spawn.AgentCommand)
		results, err := service.SpawnAgents(tasks, baseBranch, noInstall, cfg, repoDir)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error spawning agents: %v\n", err)
			os.Exit(1)
		}

		registry, err := service.NewAgentRunRegistry(filepath.Join(config.TspDir(), "agent-runs.json"))
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error loading agent registry: %v\n", err)
			os.Exit(1)
		}
		provider := providerFromCommand(cfg.Spawn.AgentCommand)
		failures := 0
		for _, result := range results {
			if result.Status != "ok" {
				failures++
				fmt.Printf("✗ %s\n  %s\n", result.Task, result.Error)
				continue
			}
			run, registerErr := registry.RegisterManaged(result, provider, result.PaneIndex, time.Now().UTC())
			if registerErr != nil {
				failures++
				fmt.Printf("✗ %s\n  registry: %v\n", result.Task, registerErr)
				continue
			}
			fmt.Printf("✓ %s\n", result.Task)
			fmt.Printf("  session: %s\n", result.Session)
			if result.Branch != "" {
				fmt.Printf("  branch:  %s\n", result.Branch)
			}
			fmt.Printf("  run:     %s\n\n", run.ID)
		}
		if failures > 0 {
			fmt.Fprintf(os.Stderr, "%d agent(s) failed to start\n", failures)
		}

		if openDash {
			if !tmuxpkg.IsInsideTmux() {
				fmt.Println("Dashboard requires tmux; run `tsp dash` from a tmux session.")
				return
			}
			dashCmd.Run(cmd, nil)
			return
		}
		fmt.Println("Run `tsp dash` to manage the roster.")
	},
}

func init() {
	spawnCmd.Flags().StringP("file", "f", "", "Read tasks from file (one per line)")
	spawnCmd.Flags().StringP("base", "b", "", "Base branch for worktrees (default: current branch)")
	spawnCmd.Flags().StringP("dir", "C", "", "Project directory (default: current directory)")
	spawnCmd.Flags().String("agent", "", "Override the configured agent command")
	spawnCmd.Flags().Bool("dash", false, "Open tsp dash after spawning")
	spawnCmd.Flags().String("setup", "", "Command to run in each worktree before the agent starts")
	spawnCmd.Flags().Bool("no-install", false, "Skip dependency installation")
	spawnCmd.Flags().Bool("dry-run", false, "Show what would be created without doing it")
}
