package cmd

import (
	"fmt"
	"os"
	"path/filepath"

	tmuxpkg "github.com/matteo-hertel/tmux-super-powers/internal/tmux"
	"github.com/spf13/cobra"
)

var wtxHereCmd = &cobra.Command{
	Use:   "wtx-here",
	Short: "Create tmux session in current directory",
	Long: `Create a tmux session in the current directory with the naming convention: ${repo_name}-${branch}

The session will have two panes:
- Left pane: neovim
- Right pane: claude`,
	Run: func(cmd *cobra.Command, args []string) {
		if !isGitRepo() {
			fmt.Fprintf(os.Stderr, "Error: Not in a git repository\n")
			os.Exit(1)
		}

		repoRoot, err := getRepoRoot()
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error: Cannot determine repository root: %v\n", err)
			os.Exit(1)
		}

		currentBranch, err := getCurrentBranch()
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error: Cannot determine current branch: %v\n", err)
			os.Exit(1)
		}

		repoName := filepath.Base(repoRoot)
		sessionName := tmuxpkg.SanitizeSessionName(fmt.Sprintf("%s-%s", repoName, currentBranch))
		currentDir, _ := os.Getwd()

		fmt.Printf("Creating tmux session '%s' in current directory...\n", sessionName)

		createGitWorktreeSession(sessionName, currentDir)

		fmt.Printf("Tmux session '%s' created successfully.\n", sessionName)
		fmt.Printf("Attach with: tmux attach-session -t '%s'\n", sessionName)
	},
}