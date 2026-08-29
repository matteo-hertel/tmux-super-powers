package cmd

import "testing"

func TestRootCommandSurface(t *testing.T) {
	commands := make(map[string]bool)
	for _, command := range rootCmd.Commands() {
		commands[command.Name()] = true
	}

	retained := []string{
		"cleanup", "config", "dash", "dir", "list", "middle", "project",
		"rm", "spawn", "version", "wtx-here", "wtx-new", "wtx-rm",
	}
	for _, name := range retained {
		if !commands[name] {
			t.Errorf("retained command %q is not registered", name)
		}
	}

	removed := []string{"device", "duck", "new", "peek", "sandbox", "serve", "txrm"}
	for _, name := range removed {
		if commands[name] {
			t.Errorf("removed command %q is still registered", name)
		}
	}
}
