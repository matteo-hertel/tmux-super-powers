package cmd

import (
	"fmt"
	"os"

	tmuxpkg "github.com/matteo-hertel/tmux-super-powers/internal/tmux"
	"github.com/spf13/cobra"
)

var middleCmd = &cobra.Command{
	Use:   "middle [command]",
	Short: "Run a command in a centered popup overlay",
	Long: `Opens a centered tmux popup overlay and runs the specified command.
The popup disappears when the command exits.

Requires tmux 3.3+ and must be run inside a tmux session.

Examples:
  tsp middle htop
  tsp middle lazydocker
  tsp middle "npm run test" --size 90
  tsp middle claude --width 85 --height 70`,
	Args: cobra.ExactArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		if !tmuxpkg.IsInsideTmux() {
			fmt.Fprintf(os.Stderr, "Error: must be run inside a tmux session\n")
			os.Exit(1)
		}

		size, _ := cmd.Flags().GetInt("size")
		width, _ := cmd.Flags().GetInt("width")
		height, _ := cmd.Flags().GetInt("height")
		wait, _ := cmd.Flags().GetBool("wait")

		w, h := resolveMiddleSize(size, width, height)

		if err := tmuxpkg.RunPopup(args[0], w, h, !wait); err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}
	},
}

func init() {
	middleCmd.Flags().Int("size", 75, "Popup size as percentage of terminal (width and height)")
	middleCmd.Flags().Int("width", 0, "Override popup width percentage (0 = use --size)")
	middleCmd.Flags().Int("height", 0, "Override popup height percentage (0 = use --size)")
	middleCmd.Flags().Bool("wait", false, "Wait for popup to close before returning")
}

func resolveMiddleSize(size, width, height int) (int, int) {
	w := size
	h := size
	if width > 0 {
		w = width
	}
	if height > 0 {
		h = height
	}
	return w, h
}

func buildMiddleArgs(command string, width, height int) []string {
	return tmuxpkg.BuildPopupArgs(command, width, height)
}
