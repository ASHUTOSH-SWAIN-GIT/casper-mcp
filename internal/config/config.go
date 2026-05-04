package config

import (
	"errors"
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
)

type Config struct {
	Database DatabaseConfig `yaml:"database"`
	States   []StateConfig  `yaml:"states"`
	IAC      IACConfig      `yaml:"iac"`
	Cloud    CloudConfig    `yaml:"cloud"`
}

type CloudConfig struct {
	AWS AWSConfig `yaml:"aws"`
}

type AWSConfig struct {
	RoleARN string   `yaml:"role_arn"`
	Regions []string `yaml:"regions"`
}

type DatabaseConfig struct {
	URL string `yaml:"url"`
}

type StateConfig struct {
	Type  string   `yaml:"type"`
	Paths []string `yaml:"paths"`
}

type IACConfig struct {
	Paths      []string `yaml:"paths"`
	ModuleDirs []string `yaml:"module_dirs"`
}

func Load(path string) (Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Config{}, fmt.Errorf("read config: %w", err)
	}

	var cfg Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return Config{}, fmt.Errorf("parse config: %w", err)
	}

	if err := cfg.Validate(); err != nil {
		return Config{}, err
	}

	return cfg, nil
}

func (cfg Config) Validate() error {
	if cfg.Database.URL == "" {
		return errors.New("database.url is required")
	}

	if len(cfg.States) == 0 {
		return errors.New("at least one state source is required")
	}

	for i, state := range cfg.States {
		if state.Type == "" {
			return fmt.Errorf("states[%d].type is required", i)
		}
		if state.Type != "local" {
			return fmt.Errorf("states[%d].type %q is not supported yet", i, state.Type)
		}
		if len(state.Paths) == 0 {
			return fmt.Errorf("states[%d].paths must contain at least one path", i)
		}
	}

	return nil
}
