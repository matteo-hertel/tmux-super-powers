package cmd

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/matteo-hertel/tmux-super-powers/config"
	"github.com/matteo-hertel/tmux-super-powers/internal/pathutil"
	tmuxpkg "github.com/matteo-hertel/tmux-super-powers/internal/tmux"
	"github.com/spf13/cobra"
)

var spawnCmd = &cobra.Command{
	Use:   "spawn [flags] task1 task2 ...",
	Short: "Deploy multiple AI agents in parallel worktrees",
	Long: `Create worktrees with tmux sessions for each task and send the task prompt to claude.

Each task gets:
1. A branch auto-named from the task description (spawn/fix-auth-bug)
2. A git worktree
3. Dependencies installed
4. A tmux session with nvim (left) + claude (right)
5. The task prompt sent to claude automatically

Examples:
  tsp spawn "fix the auth bug" "add dark mode" "refactor db layer"
  tsp spawn --file tasks.txt
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

		if !isGitRepo() {
			fmt.Fprintf(os.Stderr, "Error: not a git repository\n")
			os.Exit(1)
		}

		// Collect tasks
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
			fmt.Fprintf(os.Stderr, "Error: no tasks provided\n")
			os.Exit(1)
		}

		// Resolve base branch
		if baseBranch == "" {
			var err error
			baseBranch, err = getCurrentBranch()
			if err != nil {
				fmt.Fprintf(os.Stderr, "Error: cannot determine current branch: %v\n", err)
				os.Exit(1)
			}
		}

		repoRoot, err := getRepoRoot()
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error: cannot determine repo root: %v\n", err)
			os.Exit(1)
		}
		repoName := filepath.Base(repoRoot)

		cfg, _ := config.Load()
		worktreeBase := pathutil.ExpandPath(cfg.Spawn.WorktreeBase)
		agentCmd := cfg.Spawn.AgentCommand
		if setup == "" {
			setup = cfg.Spawn.DefaultSetup
		}

		fmt.Printf("Spawning %d agents from branch %s...\n\n", len(tasks), baseBranch)

		for i, task := range tasks {
			branch := taskToBranch(task)
			branchShort := strings.TrimPrefix(branch, "spawn/")
			sessionName := tmuxpkg.SanitizeSessionName(fmt.Sprintf("%s-%s", repoName, branchShort))
			worktreePath := filepath.Join(worktreeBase, fmt.Sprintf("%s-%s", repoName, branchShort))

			fmt.Printf("[%d/%d] %s\n", i+1, len(tasks), branchShort)

			if dryRun {
				fmt.Printf("      branch:    %s\n", branch)
				fmt.Printf("      worktree:  %s\n", worktreePath)
				fmt.Printf("      session:   %s\n", sessionName)
				fmt.Printf("      prompt:    %s\n\n", task)
				continue
			}

			// Create branch
			if !branchExists(branch) {
				if err := createBranch(branch, baseBranch); err != nil {
					fmt.Printf("      ✗ branch creation failed: %v\n", err)
					continue
				}
				fmt.Printf("      ✓ branch created\n")
			} else {
				fmt.Printf("      ✓ branch exists\n")
			}

			// Create worktree
			if _, err := os.Stat(worktreePath); err == nil {
				fmt.Printf("      ✓ worktree exists at %s\n", worktreePath)
			} else {
				if err := createWorktree(worktreePath, branch); err != nil {
					fmt.Printf("      ✗ worktree creation failed: %v\n", err)
					continue
				}
				fmt.Printf("      ✓ worktree created\n")
			}

			// Install dependencies
			if !noInstall {
				pm := detectPackageManager(repoRoot)
				if pm != "" {
					if pm == "yarn" {
						copyYarnCache(repoRoot, worktreePath)
					}
					fmt.Printf("      ◌ %s install...\n", pm)
					if err := runPackageManager(pm, worktreePath); err != nil {
						fmt.Printf("      ⚠ %s install failed: %v\n", pm, err)
					} else {
						fmt.Printf("      ✓ %s install\n", pm)
					}
				}
			}

			// Run setup command
			if setup != "" {
				fmt.Printf("      ◌ running setup...\n")
				setupCmd := exec.Command("sh", "-c", setup)
				setupCmd.Dir = worktreePath
				if err := setupCmd.Run(); err != nil {
					fmt.Printf("      ⚠ setup failed: %v\n", err)
				} else {
					fmt.Printf("      ✓ setup complete\n")
				}
			}

			// Create tmux session
			if tmuxpkg.SessionExists(sessionName) {
				tmuxpkg.KillSession(sessionName)
			}
			tmuxpkg.CreateTwoPaneSession(sessionName, worktreePath, "nvim", agentCmd)
			fmt.Printf("      ✓ session created\n")

			// Send task prompt to claude pane
			target := fmt.Sprintf("%s:0.1", sessionName)
			tmuxpkg.SendKeys(target, task)
			fmt.Printf("      ✓ prompt sent to agent\n\n")
		}

		if dryRun {
			fmt.Println("Dry run complete. No changes made.")
			return
		}

		fmt.Printf("All %d agents deployed.", len(tasks))
		if openDash {
			fmt.Println(" Opening dashboard...")
			dashExec := exec.Command(os.Args[0], "dash")
			dashExec.Stdin = os.Stdin
			dashExec.Stdout = os.Stdout
			dashExec.Stderr = os.Stderr
			dashExec.Run()
		} else {
			fmt.Println(" Run `tsp dash` to monitor.")
		}
	},
}

func init() {
	spawnCmd.Flags().StringP("file", "f", "", "Read tasks from file (one per line)")
	spawnCmd.Flags().StringP("base", "b", "", "Base branch for worktrees (default: current branch)")
	spawnCmd.Flags().Bool("dash", false, "Open tsp dash after deploying all agents")
	spawnCmd.Flags().String("setup", "", "Command to run in each worktree after install")
	spawnCmd.Flags().Bool("no-install", false, "Skip dependency installation")
	spawnCmd.Flags().Bool("dry-run", false, "Show what would be created without doing it")
}
