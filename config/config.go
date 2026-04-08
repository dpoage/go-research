// Package config defines the research configuration schema and loading.
package config

import (
	"fmt"
	"os"
	"time"

	"gopkg.in/yaml.v3"
)

// Config is the top-level research configuration loaded from research.yaml.
type Config struct {
	Program  string         `yaml:"program"`
	Files    []string       `yaml:"files"`
	Eval     EvalConfig     `yaml:"eval"`
	Provider ProviderConfig `yaml:"provider"`
	Git      GitConfig      `yaml:"git"`
}

// EvalConfig defines how experiments are evaluated.
type EvalConfig struct {
	Command   string        `yaml:"command"`
	Metric    string        `yaml:"metric"`
	Direction string        `yaml:"direction"`
	Timeout   time.Duration `yaml:"timeout"`
}

// ProviderConfig selects and configures the LLM backend.
type ProviderConfig struct {
	Backend   string `yaml:"backend"`
	Model     string `yaml:"model"`
	URL       string `yaml:"url"`
	APIKeyEnv string `yaml:"api_key_env"`
	MaxTokens int    `yaml:"max_tokens"`
}

// GitConfig controls git integration for experiment tracking.
type GitConfig struct {
	Enabled      bool   `yaml:"enabled"`
	BranchPrefix string `yaml:"branch_prefix"`
}

// Load reads and validates a Config from the given YAML file path.
func Load(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read config: %w", err)
	}

	var cfg Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("parse config: %w", err)
	}

	cfg.applyDefaults()

	if err := cfg.validate(); err != nil {
		return nil, fmt.Errorf("invalid config: %w", err)
	}

	return &cfg, nil
}

func (c *Config) applyDefaults() {
	if c.Eval.Timeout == 0 {
		c.Eval.Timeout = 5 * time.Minute
	}
	if c.Provider.MaxTokens == 0 {
		c.Provider.MaxTokens = 16384
	}
	if c.Git.BranchPrefix == "" {
		c.Git.BranchPrefix = "research/"
	}
	// Git enabled by default — only disabled if explicitly set to false.
	// Since zero-value bool is false, we need the yaml tag to handle this.
	// We use a pointer approach in applyDefaults isn't needed; the yaml
	// decoder will set it to true if "enabled: true" is present, and leave
	// it false if "enabled: false" or omitted. We treat omitted as true:
	// this is handled by the default YAML template in init, not here.
}

func (c *Config) validate() error {
	if c.Program == "" {
		return fmt.Errorf("program is required")
	}
	if len(c.Files) == 0 {
		return fmt.Errorf("at least one file must be specified")
	}
	if c.Eval.Command == "" {
		return fmt.Errorf("eval.command is required")
	}
	if c.Eval.Metric == "" {
		return fmt.Errorf("eval.metric is required")
	}
	switch c.Eval.Direction {
	case "minimize", "maximize":
		// ok
	case "":
		return fmt.Errorf("eval.direction is required (minimize or maximize)")
	default:
		return fmt.Errorf("eval.direction must be 'minimize' or 'maximize', got %q", c.Eval.Direction)
	}
	if c.Provider.Backend == "" {
		return fmt.Errorf("provider.backend is required")
	}
	if c.Provider.Model == "" {
		return fmt.Errorf("provider.model is required")
	}
	return nil
}
