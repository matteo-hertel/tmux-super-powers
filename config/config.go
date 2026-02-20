package config

import (
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

type Config struct {
	Directories       []string `yaml:"directories"`
	IgnoreDirectories []string `yaml:"ignore_directories"`
	Sandbox           Sandbox  `yaml:"sandbox"`
	Projects          Projects `yaml:"projects"`
	Editor            string   `yaml:"editor"`
}

type Sandbox struct {
	Path string `yaml:"path"`
}

type Projects struct {
	Path string `yaml:"path"`
}

// Load loads config from the default path (~/.tmux-super-powers.yaml).
func Load() (*Config, error) {
	return LoadFrom(ConfigPath())
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

	return &cfg, nil
}

// Save saves config to the default path.
func Save(cfg *Config) error {
	return SaveTo(cfg, ConfigPath())
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
	}
}

// ConfigPath returns the default config file path.
func ConfigPath() string {
	homeDir, _ := os.UserHomeDir()
	return filepath.Join(homeDir, ".tmux-super-powers.yaml")
}
