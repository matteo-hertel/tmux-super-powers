package cmd

import (
	"fmt"
	"os"
	"os/exec"

	"github.com/matteo-hertel/tmux-super-powers/config"
	"github.com/spf13/cobra"
)

var configCmd = &cobra.Command{
	Use:   "config",
	Short: "Edit configuration file",
	Run: func(cmd *cobra.Command, args []string) {
		cfg, err := config.Load()
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error loading config: %v\n", err)
			os.Exit(1)
		}

		configPath := config.ConfigPath()
		
		if _, err := os.Stat(configPath); os.IsNotExist(err) {
			if err := config.Save(cfg); err != nil {
				fmt.Fprintf(os.Stderr, "Error creating config file: %v\n", err)
				os.Exit(1)
			}
		}

		editor := cfg.Editor
		if editor == "" {
			editor = "vim"
		}

		editorCmd := exec.Command(editor, configPath)
		editorCmd.Stdin = os.Stdin
		editorCmd.Stdout = os.Stdout
		editorCmd.Stderr = os.Stderr

		if err := editorCmd.Run(); err != nil {
			fmt.Fprintf(os.Stderr, "Error opening editor: %v\n", err)
			os.Exit(1)
		}
	},
}

var configRepairCmd = &cobra.Command{
	Use:   "repair",
	Short: "Detect and fill missing config fields with defaults",
	Run: func(cmd *cobra.Command, args []string) {
		configPath := config.ConfigPath()

		cfg, err := config.Load()
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error loading config: %v\n", err)
			fmt.Fprintf(os.Stderr, "Consider backing up and resetting: cp %s %s.bak\n", configPath, configPath)
			os.Exit(1)
		}

		changes, updated := config.Repair(cfg)

		if len(changes) == 0 {
			fmt.Println("Config is up to date. No changes needed.")
			return
		}

		// Backup
		bakPath := configPath + ".bak"
		if data, err := os.ReadFile(configPath); err == nil {
			os.WriteFile(bakPath, data, 0644)
			fmt.Printf("Backup saved to %s\n", bakPath)
		}

		if err := config.Save(updated); err != nil {
			fmt.Fprintf(os.Stderr, "Error saving config: %v\n", err)
			os.Exit(1)
		}

		fmt.Printf("Config updated (%d changes):\n", len(changes))
		for _, c := range changes {
			fmt.Printf("  + %s\n", c)
		}
	},
}

func init() {
	configCmd.AddCommand(configRepairCmd)
}