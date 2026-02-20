package cmd

import (
	"fmt"
	"os"

	"github.com/matteo-hertel/tmux-super-powers/config"
	"github.com/spf13/cobra"
)

var newCmd = &cobra.Command{
	Use:   "new [name]",
	Short: "Create a new project (sandbox or project)",
	Long: `Create a new project directory with a tmux session.

Use --sandbox or --project to specify the type.
If neither is specified, defaults to project.

Examples:
  tsp new myapp --sandbox
  tsp new myapp --project
  tsp new                    # interactive, defaults to project`,
	Run: func(cmd *cobra.Command, args []string) {
		isSandbox, _ := cmd.Flags().GetBool("sandbox")

		cfg, err := config.Load()
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error loading config: %v\n", err)
			os.Exit(1)
		}

		var creatorCfg projectCreatorConfig
		if isSandbox {
			creatorCfg = projectCreatorConfig{
				Title:         "Create a new sandbox project",
				Placeholder:   "Enter project name",
				BasePath:      cfg.Sandbox.Path,
				SessionPrefix: "sandbox",
			}
		} else {
			creatorCfg = projectCreatorConfig{
				Title:         "Create a new project",
				Placeholder:   "Enter project name",
				BasePath:      cfg.Projects.Path,
				SessionPrefix: "project",
			}
		}

		runProjectCreator(creatorCfg)
	},
}

func init() {
	newCmd.Flags().Bool("sandbox", false, "Create in sandbox directory")
	newCmd.Flags().Bool("project", false, "Create in projects directory (default)")
}
