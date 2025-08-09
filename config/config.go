package config

import (
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

type Config struct {
	Directories []string `yaml:"directories"`
	Sandbox     Sandbox  `yaml:"sandbox"`
	Projects    Projects `yaml:"projects"`
	Editor      string   `yaml:"editor"`
}

type Sandbox struct {
	Path string `yaml:"path"`
}

type Projects struct {
	Path string `yaml:"path"`
}

func Load() (*Config, error) {
	configPath := filepath.Join(os.Getenv("HOME"), ".tmux-super-powers.yaml")
	
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

	if cfg.Editor == "" {
		cfg.Editor = os.Getenv("EDITOR")
		if cfg.Editor == "" {
			cfg.Editor = "vim"
		}
	}

	return &cfg, nil
}

func Save(cfg *Config) error {
	configPath := filepath.Join(os.Getenv("HOME"), ".tmux-super-powers.yaml")
	
	data, err := yaml.Marshal(cfg)
	if err != nil {
		return err
	}

	return os.WriteFile(configPath, data, 0644)
}

func defaultConfig() *Config {
	return &Config{
		Directories: []string{
			filepath.Join(os.Getenv("HOME"), "projects"),
			filepath.Join(os.Getenv("HOME"), "work"),
		},
		Sandbox: Sandbox{
			Path: filepath.Join(os.Getenv("HOME"), "sandbox"),
		},
		Projects: Projects{
			Path: filepath.Join(os.Getenv("HOME"), "projects"),
		},
		Editor: os.Getenv("EDITOR"),
	}
}

func ConfigPath() string {
	return filepath.Join(os.Getenv("HOME"), ".tmux-super-powers.yaml")
}