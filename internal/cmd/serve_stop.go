package cmd

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

var serveStopCmd = &cobra.Command{
	Use:   "stop",
	Short: "Stop the tsp server daemon",
	Long: `Stop the running tsp server daemon via launchctl.

The launchd plist is kept in place so 'tsp serve start' can reload it.`,
	Run: func(cmd *cobra.Command, args []string) {
		if !isServiceLoaded() {
			fmt.Println("tsp server is not running.")
			return
		}

		if err := unloadService(); err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}

		fmt.Println("tsp server stopped.")
	},
}
