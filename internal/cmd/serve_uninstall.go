package cmd

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

var serveUninstallCmd = &cobra.Command{
	Use:   "uninstall",
	Short: "Stop the daemon and remove the launchd plist",
	Long:  `Stop the tsp server daemon and remove the launchd plist file completely.`,
	Run: func(cmd *cobra.Command, args []string) {
		if isServiceLoaded() {
			if err := unloadService(); err != nil {
				fmt.Fprintf(os.Stderr, "Error stopping service: %v\n", err)
				os.Exit(1)
			}
		}

		if err := removePlist(); err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}

		fmt.Println("tsp server uninstalled.")
	},
}
