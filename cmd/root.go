package cmd

import (
	"github.com/spf13/cobra"
)

var rootCmd = &cobra.Command{
	Use:   "tsp",
	Short: "tmux super powers - Enhanced tmux functionality",
	Long:  `tmux-super-powers (tsp) provides enhanced functionality for tmux users including session management, quick directory access, and sandbox project creation.`,
	Run: func(cmd *cobra.Command, args []string) {
		listCmd.Run(cmd, args)
	},
}

func Execute() error {
	return rootCmd.Execute()
}

func init() {
	rootCmd.AddCommand(listCmd)
	rootCmd.AddCommand(dirCmd)
	rootCmd.AddCommand(sandboxCmd)
	rootCmd.AddCommand(configCmd)
	rootCmd.AddCommand(wtxNewCmd)
	rootCmd.AddCommand(wtxHereCmd)
	rootCmd.AddCommand(wtxRmCmd)
}