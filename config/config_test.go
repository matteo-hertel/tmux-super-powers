package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoad_MissingFile_ReturnsDefaults(t *testing.T) {
	tmpDir := t.TempDir()
	cfg, err := LoadFrom(filepath.Join(tmpDir, ".tmux-super-powers.yaml"))
	if err != nil {
		t.Fatalf("LoadFrom() error = %v", err)
	}
	if cfg == nil {
		t.Fatal("LoadFrom() returned nil config")
	}
	if len(cfg.Directories) == 0 {
		t.Error("expected default directories, got empty")
	}
}

func TestLoad_ValidYAML(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.yaml")
	content := []byte("directories:\n  - /tmp/projects\neditor: nano\n")
	if err := os.WriteFile(configPath, content, 0644); err != nil {
		t.Fatal(err)
	}

	cfg, err := LoadFrom(configPath)
	if err != nil {
		t.Fatalf("LoadFrom() error = %v", err)
	}
	if len(cfg.Directories) != 1 || cfg.Directories[0] != "/tmp/projects" {
		t.Errorf("Directories = %v, want [/tmp/projects]", cfg.Directories)
	}
	if cfg.Editor != "nano" {
		t.Errorf("Editor = %q, want \"nano\"", cfg.Editor)
	}
}

func TestLoad_EditorEnvExpansion(t *testing.T) {
	t.Setenv("EDITOR", "nvim")
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.yaml")
	content := []byte("editor: $EDITOR\n")
	if err := os.WriteFile(configPath, content, 0644); err != nil {
		t.Fatal(err)
	}

	cfg, err := LoadFrom(configPath)
	if err != nil {
		t.Fatalf("LoadFrom() error = %v", err)
	}
	if cfg.Editor != "nvim" {
		t.Errorf("Editor = %q, want \"nvim\"", cfg.Editor)
	}
}

func TestLoad_EditorFallback(t *testing.T) {
	t.Setenv("EDITOR", "")
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.yaml")
	content := []byte("directories:\n  - /tmp\n")
	if err := os.WriteFile(configPath, content, 0644); err != nil {
		t.Fatal(err)
	}

	cfg, err := LoadFrom(configPath)
	if err != nil {
		t.Fatalf("LoadFrom() error = %v", err)
	}
	if cfg.Editor != "vim" {
		t.Errorf("Editor = %q, want \"vim\"", cfg.Editor)
	}
}

func TestLoadDashConfig(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "config.yaml")
	content := []byte(`
directories:
  - ~/projects
dash:
  refresh_ms: 300
  error_patterns:
    - "FAIL"
    - "panic:"
  prompt_pattern: "\\$\\s*$"
spawn:
  worktree_base: ~/work/code
  agent_command: "claude --dangerously-skip-permissions"
  default_setup: "cp ../.env .env"
`)
	os.WriteFile(configPath, content, 0644)

	cfg, err := LoadFrom(configPath)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.Dash.RefreshMs != 300 {
		t.Errorf("expected refresh_ms 300, got %d", cfg.Dash.RefreshMs)
	}
	if len(cfg.Dash.ErrorPatterns) != 2 {
		t.Errorf("expected 2 error patterns, got %d", len(cfg.Dash.ErrorPatterns))
	}
	if cfg.Dash.PromptPattern != "\\$\\s*$" {
		t.Errorf("unexpected prompt pattern: %s", cfg.Dash.PromptPattern)
	}
	if cfg.Spawn.AgentCommand != "claude --dangerously-skip-permissions" {
		t.Errorf("unexpected agent command: %s", cfg.Spawn.AgentCommand)
	}
	if cfg.Spawn.DefaultSetup != "cp ../.env .env" {
		t.Errorf("unexpected default setup: %s", cfg.Spawn.DefaultSetup)
	}
}

func TestDashConfigDefaults(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "config.yaml")
	os.WriteFile(configPath, []byte("directories:\n  - ~/projects\n"), 0644)

	cfg, err := LoadFrom(configPath)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.Dash.RefreshMs != 500 {
		t.Errorf("expected default refresh_ms 500, got %d", cfg.Dash.RefreshMs)
	}
	if cfg.Spawn.AgentCommand != "claude --dangerously-skip-permissions" {
		t.Errorf("expected default agent command, got: %s", cfg.Spawn.AgentCommand)
	}
}

func TestSaveAndLoad_RoundTrip(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.yaml")

	original := &Config{
		Directories: []string{"/tmp/a", "/tmp/b"},
		Editor:      "code",
		Sandbox:     Sandbox{Path: "/tmp/sandbox"},
		Projects:    Projects{Path: "/tmp/projects"},
	}

	if err := SaveTo(original, configPath); err != nil {
		t.Fatalf("SaveTo() error = %v", err)
	}

	loaded, err := LoadFrom(configPath)
	if err != nil {
		t.Fatalf("LoadFrom() error = %v", err)
	}

	if loaded.Editor != original.Editor {
		t.Errorf("Editor = %q, want %q", loaded.Editor, original.Editor)
	}
	if len(loaded.Directories) != len(original.Directories) {
		t.Errorf("Directories length = %d, want %d", len(loaded.Directories), len(original.Directories))
	}
}
