package cmd

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

var rootCmd = &cobra.Command{
	Use:   "tsp",
	Short: "Manage tmux projects, worktrees, and coding agents",
	Long:  `tmux-super-powers (tsp) keeps project navigation, tmux sessions, git worktrees, and local coding agents in one focused CLI.`,
	Run: func(cmd *cobra.Command, args []string) {
		versionFlag, _ := cmd.Flags().GetBool("version")
		if versionFlag {
			fmt.Printf("tsp version %s\n", getVersion())
			os.Exit(0)
		}
		cmd.Help()
	},
}

func Execute() error {
	return rootCmd.Execute()
}

func init() {
	rootCmd.AddCommand(listCmd)
	rootCmd.AddCommand(dirCmd)
	rootCmd.AddCommand(projectCmd)
	rootCmd.AddCommand(configCmd)
	rootCmd.AddCommand(wtxNewCmd)
	rootCmd.AddCommand(wtxHereCmd)
	rootCmd.AddCommand(wtxRmCmd)
	rootCmd.AddCommand(middleCmd)
	rootCmd.AddCommand(versionCmd)
	rootCmd.AddCommand(dashCmd)
	rootCmd.AddCommand(spawnCmd)
	rootCmd.AddCommand(rmCmd)
	rootCmd.AddCommand(cleanupCmd)

	// Add version flag
	rootCmd.Flags().BoolP("version", "v", false, "Show version information")
}
