package cmd

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

var serveRestartCmd = &cobra.Command{
	Use:   "restart",
	Short: "Restart the tsp server daemon",
	Long:  `Stop and start the tsp server daemon. Always regenerates the plist to pick up binary or PATH changes.`,
	Run: func(cmd *cobra.Command, args []string) {
		// Stop if running (ignore error if not loaded)
		if isServiceLoaded() {
			_ = unloadService()
		}

		path, err := writePlist()
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}

		if err := loadService(path); err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}

		fmt.Println("tsp server restarted.")
		fmt.Printf("  Logs: %s/serve.log\n", logDir())
	},
}
