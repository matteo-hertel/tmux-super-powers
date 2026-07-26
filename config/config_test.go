package config

import (
	"os"
	"path/filepath"
	"strings"
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

func TestLoadSpawnConfig(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "config.yaml")
	content := []byte(`
directories:
  - ~/projects
spawn:
  worktree_base: ~/work/code
  agent_command: "claude --dangerously-skip-permissions"
  default_setup: "cp ../.env .env"
manager:
  agent_command: "codex exec --ephemeral --sandbox workspace-write"
`)
	os.WriteFile(configPath, content, 0644)

	cfg, err := LoadFrom(configPath)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.Spawn.AgentCommand != "claude --dangerously-skip-permissions" {
		t.Errorf("unexpected agent command: %s", cfg.Spawn.AgentCommand)
	}
	if cfg.Spawn.DefaultSetup != "cp ../.env .env" {
		t.Errorf("unexpected default setup: %s", cfg.Spawn.DefaultSetup)
	}
	if cfg.Manager.AgentCommand != "codex exec --ephemeral --sandbox workspace-write" {
		t.Errorf("unexpected manager agent command: %s", cfg.Manager.AgentCommand)
	}
}

func TestSpawnConfigDefaults(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "config.yaml")
	os.WriteFile(configPath, []byte("directories:\n  - ~/projects\n"), 0644)

	cfg, err := LoadFrom(configPath)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.Spawn.AgentCommand != "claude --dangerously-skip-permissions" {
		t.Errorf("expected default agent command, got: %s", cfg.Spawn.AgentCommand)
	}
	if cfg.Manager.AgentCommand != "claude -p --model haiku --permission-mode auto" {
		t.Errorf("expected default manager command, got: %s", cfg.Manager.AgentCommand)
	}
	if cfg.Projects.Path == "" {
		t.Error("expected project path default, got empty")
	}
}

func TestSaveAndLoad_RoundTrip(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.yaml")

	original := &Config{
		Directories: []string{"/tmp/a", "/tmp/b"},
		Editor:      "code",
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

func TestConfigPath_PrefersTspDir(t *testing.T) {
	path := ConfigPath()
	if !strings.HasSuffix(path, filepath.Join(".tsp", "config.yaml")) {
		t.Errorf("ConfigPath() = %q, want suffix .tsp/config.yaml", path)
	}
}

func TestLoad_MigratesOldConfig(t *testing.T) {
	tmpDir := t.TempDir()
	oldPath := filepath.Join(tmpDir, ".tmux-super-powers.yaml")
	content := []byte("directories:\n  - /tmp/migrated\neditor: nano\n")
	if err := os.WriteFile(oldPath, content, 0644); err != nil {
		t.Fatal(err)
	}
	cfg, newPath, err := LoadWithMigration(tmpDir)
	if err != nil {
		t.Fatalf("LoadWithMigration() error = %v", err)
	}
	expectedNew := filepath.Join(tmpDir, ".tsp", "config.yaml")
	if newPath != expectedNew {
		t.Errorf("new path = %q, want %q", newPath, expectedNew)
	}
	if len(cfg.Directories) != 1 || cfg.Directories[0] != "/tmp/migrated" {
		t.Errorf("config not migrated correctly: %+v", cfg)
	}
	if _, err := os.Stat(expectedNew); os.IsNotExist(err) {
		t.Error("new config file was not created")
	}
}

func TestLoad_NewPathTakesPriority(t *testing.T) {
	tmpDir := t.TempDir()
	oldPath := filepath.Join(tmpDir, ".tmux-super-powers.yaml")
	os.WriteFile(oldPath, []byte("editor: old\n"), 0644)
	newDir := filepath.Join(tmpDir, ".tsp")
	os.MkdirAll(newDir, 0755)
	newPath := filepath.Join(newDir, "config.yaml")
	os.WriteFile(newPath, []byte("editor: new\n"), 0644)
	cfg, _, err := LoadWithMigration(tmpDir)
	if err != nil {
		t.Fatalf("error = %v", err)
	}
	if cfg.Editor != "new" {
		t.Errorf("Editor = %q, want \"new\"", cfg.Editor)
	}
}

func TestLoad_IgnoresRemovedLegacySections(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "config.yaml")
	content := []byte(`
projects:
  path: /tmp/projects
dash:
  refresh_ms: 100
serve:
  port: 7777
watcher:
  enabled: true
sandbox:
  path: /tmp/sandbox
`)
	if err := os.WriteFile(configPath, content, 0644); err != nil {
		t.Fatal(err)
	}
	cfg, err := LoadFrom(configPath)
	if err != nil {
		t.Fatalf("legacy keys should be ignored, got error: %v", err)
	}
	if cfg.Projects.Path != "/tmp/projects" {
		t.Fatalf("active project config was not loaded: %#v", cfg.Projects)
	}
}
