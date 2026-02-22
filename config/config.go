package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

type Config struct {
	Directories       []string    `yaml:"directories"`
	IgnoreDirectories []string    `yaml:"ignore_directories"`
	Sandbox           Sandbox     `yaml:"sandbox"`
	Projects          Projects    `yaml:"projects"`
	Editor            string      `yaml:"editor"`
	Dash              DashConfig  `yaml:"dash"`
	Spawn             SpawnConfig `yaml:"spawn"`
	Serve             ServeConfig `yaml:"serve"`
}

type DashConfig struct {
	RefreshMs     int      `yaml:"refresh_ms"`
	ErrorPatterns []string `yaml:"error_patterns"`
	PromptPattern string   `yaml:"prompt_pattern"`
}

type SpawnConfig struct {
	WorktreeBase string `yaml:"worktree_base"`
	AgentCommand string `yaml:"agent_command"`
	DefaultSetup string `yaml:"default_setup"`
}

type ServeConfig struct {
	Port      int    `yaml:"port"`
	Bind      string `yaml:"bind"`
	RefreshMs int    `yaml:"refresh_ms"`
}

type Sandbox struct {
	Path string `yaml:"path"`
}

type Projects struct {
	Path string `yaml:"path"`
}

// Load loads config from the new path (~/.tsp/config.yaml), migrating from the
// old path (~/.tmux-super-powers.yaml) if necessary.
func Load() (*Config, error) {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return nil, err
	}
	cfg, _, err := LoadWithMigration(homeDir)
	return cfg, err
}

// LoadWithMigration loads config with automatic migration from the old path.
// It checks ~/.tsp/config.yaml first; if missing, falls back to
// ~/.tmux-super-powers.yaml and copies it to the new location.
// Returns the loaded config, the path it was loaded from, and any error.
func LoadWithMigration(homeDir string) (*Config, string, error) {
	newPath := filepath.Join(homeDir, ".tsp", "config.yaml")
	oldPath := filepath.Join(homeDir, ".tmux-super-powers.yaml")

	// New path takes priority
	if _, err := os.Stat(newPath); err == nil {
		cfg, err := LoadFrom(newPath)
		return cfg, newPath, err
	}

	// Check old path
	if _, err := os.Stat(oldPath); err == nil {
		// Read old config
		data, err := os.ReadFile(oldPath)
		if err != nil {
			return nil, "", err
		}

		// Create new directory
		newDir := filepath.Join(homeDir, ".tsp")
		if err := os.MkdirAll(newDir, 0755); err != nil {
			return nil, "", err
		}

		// Copy to new location
		if err := os.WriteFile(newPath, data, 0644); err != nil {
			return nil, "", err
		}

		fmt.Fprintf(os.Stderr, "Migrated config from %s to %s\n", oldPath, newPath)

		cfg, err := LoadFrom(newPath)
		return cfg, newPath, err
	}

	// Neither exists, return defaults
	cfg, err := LoadFrom(newPath)
	return cfg, newPath, err
}

// LoadFrom loads config from a specific file path.
func LoadFrom(configPath string) (*Config, error) {
	data, err := os.ReadFile(configPath)
	if err != nil {
		if os.IsNotExist(err) {
			return defaultConfig(), nil
		}
		return nil, err
	}

	var cfg Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, err
	}

	// Expand $VAR style editor values
	if strings.HasPrefix(cfg.Editor, "$") {
		cfg.Editor = os.Getenv(cfg.Editor[1:])
	}

	if cfg.Editor == "" {
		cfg.Editor = os.Getenv("EDITOR")
		if cfg.Editor == "" {
			cfg.Editor = "vim"
		}
	}

	// Dash defaults
	if cfg.Dash.RefreshMs == 0 {
		cfg.Dash.RefreshMs = 500
	}
	if cfg.Dash.PromptPattern == "" {
		cfg.Dash.PromptPattern = `\$\s*$`
	}
	if len(cfg.Dash.ErrorPatterns) == 0 {
		cfg.Dash.ErrorPatterns = []string{"FAIL", "panic:", "Error:"}
	}

	// Spawn defaults
	homeDir, _ := os.UserHomeDir()
	if cfg.Spawn.AgentCommand == "" {
		cfg.Spawn.AgentCommand = "claude --dangerously-skip-permissions"
	}
	if cfg.Spawn.WorktreeBase == "" {
		cfg.Spawn.WorktreeBase = filepath.Join(homeDir, "work", "code")
	}

	// Serve defaults
	if cfg.Serve.Port == 0 {
		cfg.Serve.Port = 7777
	}
	if cfg.Serve.RefreshMs == 0 {
		cfg.Serve.RefreshMs = cfg.Dash.RefreshMs
	}

	return &cfg, nil
}

// Save saves config to the default path (~/.tsp/config.yaml),
// creating the ~/.tsp/ directory if needed.
func Save(cfg *Config) error {
	configPath := ConfigPath()
	if err := os.MkdirAll(filepath.Dir(configPath), 0755); err != nil {
		return err
	}
	return SaveTo(cfg, configPath)
}

// SaveTo saves config to a specific file path.
func SaveTo(cfg *Config, configPath string) error {
	data, err := yaml.Marshal(cfg)
	if err != nil {
		return err
	}
	return os.WriteFile(configPath, data, 0644)
}

func defaultConfig() *Config {
	homeDir, _ := os.UserHomeDir()
	return &Config{
		Directories: []string{
			filepath.Join(homeDir, "projects"),
			filepath.Join(homeDir, "work"),
		},
		Sandbox: Sandbox{
			Path: filepath.Join(homeDir, "sandbox"),
		},
		Projects: Projects{
			Path: filepath.Join(homeDir, "projects"),
		},
		Editor: os.Getenv("EDITOR"),
		Dash: DashConfig{
			RefreshMs:     500,
			ErrorPatterns: []string{"FAIL", "panic:", "Error:"},
			PromptPattern: `\$\s*$`,
		},
		Spawn: SpawnConfig{
			WorktreeBase: filepath.Join(homeDir, "work", "code"),
			AgentCommand: "claude --dangerously-skip-permissions",
		},
		Serve: ServeConfig{
			Port:      7777,
			RefreshMs: 500,
		},
	}
}

// TspDir returns the path to the ~/.tsp directory.
func TspDir() string {
	homeDir, _ := os.UserHomeDir()
	return filepath.Join(homeDir, ".tsp")
}

// ConfigPath returns the default config file path (~/.tsp/config.yaml).
func ConfigPath() string {
	return filepath.Join(TspDir(), "config.yaml")
}

// OldConfigPath returns the legacy config file path (~/.tmux-super-powers.yaml).
func OldConfigPath() string {
	homeDir, _ := os.UserHomeDir()
	return filepath.Join(homeDir, ".tmux-super-powers.yaml")
}
