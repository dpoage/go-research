// Package config defines the research configuration schema and loading.
package config

import (
	"fmt"
	"os"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

const (
	DefaultMaxTokens = 16384
	DefaultMaxRounds = 8
)

// Config is the top-level research configuration loaded from research.yaml.
type Config struct {
	Program  string         `yaml:"program"`
	Files    []string       `yaml:"files"`
	Eval     EvalConfig     `yaml:"eval"`
	Provider ProviderConfig `yaml:"provider"`
	Git      GitConfig      `yaml:"git"`
	Debug    DebugConfig    `yaml:"debug"`
}

// DebugConfig controls diagnostic logging for prompt analysis.
type DebugConfig struct {
	Enabled bool   `yaml:"enabled"`
	Dir     string `yaml:"dir"`
}

// EvalConfig defines how experiments are evaluated.
type EvalConfig struct {
	Command   string    `yaml:"command"`
	Metric    string    `yaml:"metric"`
	Source    Source    `yaml:"source"`
	Direction Direction `yaml:"direction"`
	Timeout   Duration  `yaml:"timeout"`
}

// ProviderConfig selects and configures the LLM backend.
type ProviderConfig struct {
	Backend   Backend `yaml:"backend"`
	Model     string  `yaml:"model"`
	URL       string  `yaml:"url"`
	APIKeyEnv string  `yaml:"api_key_env"`
	MaxTokens int     `yaml:"max_tokens"`
	MaxRounds int     `yaml:"max_rounds"`
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
	if c.Eval.Timeout.Duration == 0 {
		c.Eval.Timeout = Duration{5 * time.Minute}
	}
	if c.Provider.MaxTokens == 0 {
		c.Provider.MaxTokens = DefaultMaxTokens
	}
	if c.Provider.MaxRounds == 0 {
		c.Provider.MaxRounds = DefaultMaxRounds
	}
	if c.Git.BranchPrefix == "" {
		c.Git.BranchPrefix = "research/"
	}
	if c.Debug.Enabled && c.Debug.Dir == "" {
		c.Debug.Dir = "/tmp/go-research-debug"
	}
	if c.Eval.Source.Kind == "" {
		c.Eval.Source = NewSourceStdout()
	}
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
	if !c.Eval.Direction.Valid() {
		return fmt.Errorf("eval.direction must be %q or %q", DirectionMinimize, DirectionMaximize)
	}
	if !c.Provider.Backend.Valid() {
		return fmt.Errorf("provider.backend must be %q or %q", BackendAnthropic, BackendOpenAI)
	}
	if c.Provider.Model == "" {
		return fmt.Errorf("provider.model is required")
	}
	if c.Provider.MaxRounds < 1 {
		return fmt.Errorf("provider.max_rounds must be at least 1")
	}
	if c.Eval.Source.IsFile() && strings.HasPrefix(c.Eval.Metric, sourceFilePrefix) {
		return fmt.Errorf("eval.source and eval.metric cannot both use %q prefix", sourceFilePrefix)
	}
	return nil
}
