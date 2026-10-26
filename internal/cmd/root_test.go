package cmd

import "testing"

func TestRootCommandSurface(t *testing.T) {
	commands := make(map[string]bool)
	for _, command := range rootCmd.Commands() {
		commands[command.Name()] = true
	}

	retained := []string{"config", "dash", "dir", "project", "spawn"}
	for _, name := range retained {
		if !commands[name] {
			t.Errorf("retained command %q is not registered", name)
		}
	}

	removed := []string{
		"cleanup", "device", "duck", "list", "middle", "new", "peek", "rm",
		"sandbox", "serve", "txrm", "version", "wtx-here", "wtx-new", "wtx-rm",
	}
	for _, name := range removed {
		if commands[name] {
			t.Errorf("removed command %q is still registered", name)
		}
	}
}

func TestRootVersionFlag(t *testing.T) {
	if rootCmd.Version == "" {
		t.Fatal("root command has no version, so -v is not registered")
	}
}
