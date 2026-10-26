package cmd

import (
	"runtime/debug"

	"github.com/spf13/cobra"
)

var rootCmd = &cobra.Command{
	Use:     "tsp",
	Short:   "Manage tmux projects, worktrees, and coding agents",
	Long:    `tmux-super-powers (tsp) keeps project navigation, tmux sessions, git worktrees, and local coding agents in one focused CLI.`,
	Version: getVersion(),
}

func Execute() error {
	return rootCmd.Execute()
}

func init() {
	rootCmd.AddCommand(dirCmd)
	rootCmd.AddCommand(projectCmd)
	rootCmd.AddCommand(configCmd)
	rootCmd.AddCommand(dashCmd)
	rootCmd.AddCommand(spawnCmd)

	// Cobra adds --version/-v from the Version field.
	rootCmd.SetVersionTemplate("tsp version {{.Version}}\n")
}

func getVersion() string {
	if info, ok := debug.ReadBuildInfo(); ok {
		if info.Main.Version != "(devel)" && info.Main.Version != "" {
			return info.Main.Version
		}
	}
	return "dev"
}
