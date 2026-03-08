package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"strconv"

	duckpkg "github.com/matteo-hertel/tmux-super-powers/internal/duck"
	tmuxpkg "github.com/matteo-hertel/tmux-super-powers/internal/tmux"
	"github.com/spf13/cobra"
)

var duckCmd = &cobra.Command{
	Use:   "duck",
	Short: "Manage your tmux ducks 🦆",
	Long:  `Spawn ducks that waddle around a tmux popup. Run 'tsp duck' to view, 'tsp duck new' to hatch, 'tsp duck cook' to remove.`,
	Run:   runDuck,
}

var duckNewCmd = &cobra.Command{
	Use:   "new [count]",
	Short: "Hatch new ducks",
	Args:  cobra.MaximumNArgs(1),
	Run:   runDuckNew,
}

var duckCookCmd = &cobra.Command{
	Use:   "cook",
	Short: "Cook (remove) the last duck",
	Run:   runDuckCook,
}

func init() {
	duckCmd.AddCommand(duckNewCmd)
	duckCmd.AddCommand(duckCookCmd)
	duckCmd.Flags().Bool("daemon", false, "Run duck daemon (internal)")
	duckCmd.Flags().Bool("viewer", false, "Run duck viewer (internal)")
	duckCmd.Flags().MarkHidden("daemon")
	duckCmd.Flags().MarkHidden("viewer")
	duckCookCmd.Flags().Bool("all", false, "Cook all ducks")
}

func runDuck(cmd *cobra.Command, args []string) {
	daemonFlag, _ := cmd.Flags().GetBool("daemon")
	if daemonFlag {
		if err := duckpkg.StartDaemon(); err != nil {
			fmt.Fprintf(os.Stderr, "Error starting duck daemon: %v\n", err)
			os.Exit(1)
		}
		return
	}

	viewerFlag, _ := cmd.Flags().GetBool("viewer")
	if viewerFlag {
		if err := duckpkg.RunViewer(); err != nil {
			fmt.Fprintf(os.Stderr, "Error running duck viewer: %v\n", err)
			os.Exit(1)
		}
		return
	}

	if !tmuxpkg.IsInsideTmux() {
		fmt.Fprintln(os.Stderr, "Duck pond requires tmux. Run inside a tmux session.")
		os.Exit(1)
	}

	if err := tmuxpkg.RunPopup("tsp duck --viewer", 75, 75, false); err != nil {
		fmt.Fprintf(os.Stderr, "Error opening duck pond: %v\n", err)
		os.Exit(1)
	}
}

func runDuckNew(cmd *cobra.Command, args []string) {
	count := 1
	if len(args) > 0 {
		n, err := strconv.Atoi(args[0])
		if err != nil || n < 1 {
			fmt.Fprintln(os.Stderr, "Count must be a positive integer")
			os.Exit(1)
		}
		count = n
	}

	if err := duckpkg.EnsureDaemon(); err != nil {
		fmt.Fprintf(os.Stderr, "Error starting duck daemon: %v\n", err)
		os.Exit(1)
	}

	var total int
	for i := 0; i < count; i++ {
		resp, err := duckpkg.SendCommand("add")
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error adding duck: %v\n", err)
			os.Exit(1)
		}

		var result struct {
			OK    bool `json:"ok"`
			Count int  `json:"count"`
		}
		if err := json.Unmarshal([]byte(resp), &result); err != nil {
			fmt.Fprintf(os.Stderr, "Error parsing response: %v\n", err)
			os.Exit(1)
		}
		total = result.Count
	}

	if count == 1 {
		fmt.Printf("🦆 Duck hatched! (%d ducks total)\n", total)
	} else {
		fmt.Printf("🦆 %d ducks hatched! (%d ducks total)\n", count, total)
	}
}

func runDuckCook(cmd *cobra.Command, args []string) {
	allFlag, _ := cmd.Flags().GetBool("all")

	if allFlag {
		cooked := 0
		for {
			resp, err := duckpkg.SendCommand("remove")
			if err != nil {
				if cooked == 0 {
					fmt.Fprintln(os.Stderr, "No ducks to cook!")
				}
				break
			}
			var result struct {
				OK    bool `json:"ok"`
				Count int  `json:"count"`
			}
			if err := json.Unmarshal([]byte(resp), &result); err != nil {
				break
			}
			cooked++
			if result.Count == 0 {
				break
			}
		}
		if cooked > 0 {
			fmt.Printf("🍗 All %d ducks cooked!\n", cooked)
		}
		return
	}

	resp, err := duckpkg.SendCommand("remove")
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error removing duck: %v\n", err)
		os.Exit(1)
	}

	var result struct {
		OK    bool `json:"ok"`
		Count int  `json:"count"`
	}
	if err := json.Unmarshal([]byte(resp), &result); err != nil {
		fmt.Fprintf(os.Stderr, "Error parsing response: %v\n", err)
		os.Exit(1)
	}

	if result.Count == 0 {
		fmt.Println("No ducks to cook!")
	} else {
		fmt.Printf("🍗 Duck cooked! (%d ducks remaining)\n", result.Count)
	}
}
