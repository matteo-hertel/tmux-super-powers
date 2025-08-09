package cmd

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

var rootCmd = &cobra.Command{
	Use:   "tsp",
	Short: "tmux super powers - Enhanced tmux functionality",
	Long:  `tmux-super-powers (tsp) provides enhanced functionality for tmux users including session management, quick directory access, and sandbox project creation.`,
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
	rootCmd.AddCommand(txrmCmd)
	rootCmd.AddCommand(dirCmd)
	rootCmd.AddCommand(sandboxCmd)
	rootCmd.AddCommand(projectCmd)
	rootCmd.AddCommand(configCmd)
	rootCmd.AddCommand(wtxNewCmd)
	rootCmd.AddCommand(wtxHereCmd)
	rootCmd.AddCommand(wtxRmCmd)
	rootCmd.AddCommand(versionCmd)

	// Add version flag
	rootCmd.Flags().BoolP("version", "v", false, "Show version information")
}