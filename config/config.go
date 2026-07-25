package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

const defaultManagerAgentCommand = "claude -p --model haiku --permission-mode auto --max-budget-usd 1"

type Config struct {
	Directories       []string      `yaml:"directories"`
	IgnoreDirectories []string      `yaml:"ignore_directories"`
	Projects          Projects      `yaml:"projects"`
	Editor            string        `yaml:"editor"`
	Spawn             SpawnConfig   `yaml:"spawn"`
	Manager           ManagerConfig `yaml:"manager"`
}

type SpawnConfig struct {
	WorktreeBase string `yaml:"worktree_base"`
	AgentCommand string `yaml:"agent_command"`
	DefaultSetup string `yaml:"default_setup"`
}

// ManagerConfig controls short-lived agents delegated from tsp dash. The
// default is deliberately cheaper than the primary spawn agent and exits when
// the delegated task is complete.
type ManagerConfig struct {
	AgentCommand string `yaml:"agent_command"`
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

	homeDir, _ := os.UserHomeDir()
	if len(cfg.Directories) == 0 {
		cfg.Directories = []string{
			filepath.Join(homeDir, "projects"),
			filepath.Join(homeDir, "work"),
		}
	}
	if cfg.Projects.Path == "" {
		cfg.Projects.Path = filepath.Join(homeDir, "projects")
	}

	// Spawn defaults
	if cfg.Spawn.AgentCommand == "" {
		cfg.Spawn.AgentCommand = "claude --dangerously-skip-permissions"
	}
	if cfg.Spawn.WorktreeBase == "" {
		cfg.Spawn.WorktreeBase = filepath.Join(homeDir, "work", "code")
	}
	if cfg.Manager.AgentCommand == "" {
		cfg.Manager.AgentCommand = defaultManagerAgentCommand
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
		Projects: Projects{
			Path: filepath.Join(homeDir, "projects"),
		},
		Editor: os.Getenv("EDITOR"),
		Spawn: SpawnConfig{
			WorktreeBase: filepath.Join(homeDir, "work", "code"),
			AgentCommand: "claude --dangerously-skip-permissions",
		},
		Manager: ManagerConfig{
			AgentCommand: defaultManagerAgentCommand,
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

// Repair compares a config against defaults and fills in missing fields.
// Returns the list of changes made and the updated config.
func Repair(cfg *Config) ([]string, *Config) {
	defaults := defaultConfig()
	var changes []string

	if len(cfg.Directories) == 0 {
		cfg.Directories = defaults.Directories
		changes = append(changes, "directories: set to defaults")
	}
	if cfg.Projects.Path == "" {
		cfg.Projects.Path = defaults.Projects.Path
		changes = append(changes, "projects.path: set to default")
	}
	if cfg.Spawn.AgentCommand == "" {
		cfg.Spawn.AgentCommand = defaults.Spawn.AgentCommand
		changes = append(changes, "spawn.agent_command: set to default")
	}
	if cfg.Spawn.WorktreeBase == "" {
		cfg.Spawn.WorktreeBase = defaults.Spawn.WorktreeBase
		changes = append(changes, "spawn.worktree_base: set to default")
	}
	if cfg.Manager.AgentCommand == "" {
		cfg.Manager.AgentCommand = defaults.Manager.AgentCommand
		changes = append(changes, "manager.agent_command: set to default")
	}
	return changes, cfg
}

// oldConfigPath returns the legacy config file path (~/.tmux-super-powers.yaml).
func oldConfigPath() string {
	homeDir, _ := os.UserHomeDir()
	return filepath.Join(homeDir, ".tmux-super-powers.yaml")
}
