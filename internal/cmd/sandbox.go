package cmd

import (
	"fmt"
	"os"

	"github.com/matteo-hertel/tmux-super-powers/config"
	"github.com/spf13/cobra"
)

var sandboxCmd = &cobra.Command{
	Use:   "sandbox",
	Short: "Create a new sandbox project",
	Run: func(cmd *cobra.Command, args []string) {
		cfg, err := config.Load()
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error loading config: %v\n", err)
			os.Exit(1)
		}

		runProjectCreator(projectCreatorConfig{
			Title:         "Create a new sandbox project",
			Placeholder:   "Enter project name",
			BasePath:      cfg.Sandbox.Path,
			SessionPrefix: "sandbox",
		})
	},
}
