package cmd

import (
	"fmt"
	"os"

	"github.com/matteo-hertel/tmux-super-powers/config"
	"github.com/spf13/cobra"
)

var projectCmd = &cobra.Command{
	Use:   "project",
	Short: "Create a new project",
	Run: func(cmd *cobra.Command, args []string) {
		cfg, err := config.Load()
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error loading config: %v\n", err)
			os.Exit(1)
		}

		runProjectCreator(projectCreatorConfig{
			Title:         "Create a new project",
			Placeholder:   "Enter project name",
			BasePath:      cfg.Projects.Path,
			SessionPrefix: "project",
		})
	},
}
