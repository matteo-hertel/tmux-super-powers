package cmd

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

var serveStartCmd = &cobra.Command{
	Use:   "start",
	Short: "Start the tsp server as a background daemon",
	Long: `Install the launchd plist (if needed) and start the tsp server daemon.

The daemon runs in the background and auto-starts on login.
Use 'tsp serve stop' to stop it, 'tsp serve status' to check health.`,
	Run: func(cmd *cobra.Command, args []string) {
		if isServiceLoaded() {
			fmt.Println("tsp server is already running.")
			fmt.Println("Use 'tsp serve restart' to restart it.")
			return
		}

		path, err := ensurePlist()
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}

		if err := loadService(path); err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}

		fmt.Println("tsp server started.")
		fmt.Printf("  Plist: %s\n", path)
		fmt.Printf("  Logs:  %s/serve.log\n", logDir())
	},
}
