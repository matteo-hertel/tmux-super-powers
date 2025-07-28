package cmd

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"
)

var wtxNewCmd = &cobra.Command{
	Use:   "wtx-new [branch1] [branch2] ...",
	Short: "Create git worktrees with tmux sessions",
	Long: `Create git worktrees for specified branches and set up tmux sessions with neovim and claude.

For each branch:
1. Creates the branch from current branch if it doesn't exist
2. Creates worktree under ~/work/code/<repo-name>-<branch>
3. Runs yarn in the worktree
4. Creates tmux session with neovim (left) and claude (right)`,
	Args: cobra.MinimumNArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		branches := args

		if !isGitRepo() {
			fmt.Fprintf(os.Stderr, "Error: Not a git repository\n")
			os.Exit(1)
		}

		currentBranch, err := getCurrentBranch()
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error: Cannot determine current branch: %v\n", err)
			os.Exit(1)
		}

		repoRoot, err := getRepoRoot()
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error: Cannot determine repository root: %v\n", err)
			os.Exit(1)
		}

		repoName := filepath.Base(repoRoot)

		for _, branch := range branches {
			fmt.Printf("Processing branch: %s\n", branch)
			
			if !branchExists(branch) {
				fmt.Printf("Branch '%s' does not exist. Creating it from '%s'...\n", branch, currentBranch)
				if err := createBranch(branch, currentBranch); err != nil {
					fmt.Fprintf(os.Stderr, "Error: Failed to create branch '%s': %v. Skipping.\n", branch, err)
					continue
				}
			}

			worktreePath := filepath.Join(os.Getenv("HOME"), "work", "code", fmt.Sprintf("%s-%s", repoName, branch))

			if _, err := os.Stat(worktreePath); err == nil {
				fmt.Printf("Worktree for branch '%s' already exists at '%s'. Skipping creation.\n", branch, worktreePath)
			} else {
				fmt.Printf("Creating worktree for branch '%s' at '%s'...\n", branch, worktreePath)
				if err := createWorktree(worktreePath, branch); err != nil {
					fmt.Fprintf(os.Stderr, "Error: Failed to create worktree for branch '%s': %v. Skipping.\n", branch, err)
					continue
				}
			}

			fmt.Printf("Running yarn in '%s'...\n", worktreePath)
			if err := runYarn(worktreePath); err != nil {
				fmt.Printf("Warning: yarn failed in '%s': %v\n", worktreePath, err)
			}

			sessionName := fmt.Sprintf("%s-%s", repoName, branch)
			fmt.Printf("Creating tmux session '%s' with neovim and claude...\n", sessionName)
			createGitWorktreeSession(sessionName, worktreePath)

			fmt.Printf("Tmux session '%s' created successfully.\n", sessionName)
		}

		fmt.Println("All worktrees and tmux sessions created successfully.")
	},
}

func isGitRepo() bool {
	cmd := exec.Command("git", "rev-parse", "--is-inside-work-tree")
	return cmd.Run() == nil
}

func getCurrentBranch() (string, error) {
	cmd := exec.Command("git", "rev-parse", "--abbrev-ref", "HEAD")
	output, err := cmd.Output()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(output)), nil
}

func getRepoRoot() (string, error) {
	cmd := exec.Command("git", "rev-parse", "--show-toplevel")
	output, err := cmd.Output()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(output)), nil
}

func branchExists(branch string) bool {
	cmd := exec.Command("git", "show-ref", "--verify", "--quiet", fmt.Sprintf("refs/heads/%s", branch))
	return cmd.Run() == nil
}

func createBranch(branch, fromBranch string) error {
	cmd := exec.Command("git", "branch", branch, fromBranch)
	return cmd.Run()
}

func createWorktree(path, branch string) error {
	cmd := exec.Command("git", "worktree", "add", path, branch)
	return cmd.Run()
}

func runYarn(path string) error {
	cmd := exec.Command("yarn")
	cmd.Dir = path
	return cmd.Run()
}

func createGitWorktreeSession(sessionName, path string) {
	exec.Command("tmux", "kill-session", "-t", sessionName).Run()
	
	exec.Command("tmux", "new-session", "-d", "-s", sessionName, "-c", path, "nvim").Run()
	
	exec.Command("tmux", "split-window", "-h", "-t", sessionName, "-c", path, "claude").Run()
	
	exec.Command("tmux", "select-pane", "-t", sessionName+":0.0").Run()
}