package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestLoad_Valid(t *testing.T) {
	yaml := `
program: program.md
files:
  - train.py
eval:
  command: "python train.py"
  metric: 'val_bpb:\s+(?P<metric>[0-9.]+)'
  direction: minimize
  timeout: 5m
provider:
  backend: anthropic
  model: claude-sonnet-4-20250514
  api_key_env: ANTHROPIC_API_KEY
git:
  enabled: true
  branch_prefix: "research/"
`
	path := writeTemp(t, yaml)

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if cfg.Program != "program.md" {
		t.Errorf("program = %q, want %q", cfg.Program, "program.md")
	}
	if len(cfg.Files) != 1 || cfg.Files[0] != "train.py" {
		t.Errorf("files = %v, want [train.py]", cfg.Files)
	}
	if cfg.Eval.Direction != DirectionMinimize {
		t.Errorf("direction = %q, want %q", cfg.Eval.Direction, DirectionMinimize)
	}
	if cfg.Eval.Timeout.Duration != 5*time.Minute {
		t.Errorf("timeout = %v, want 5m", cfg.Eval.Timeout)
	}
	if cfg.Provider.Backend != BackendAnthropic {
		t.Errorf("backend = %q, want %q", cfg.Provider.Backend, BackendAnthropic)
	}
	if cfg.Provider.MaxTokens != 16384 {
		t.Errorf("max_tokens = %d, want 16384 (default)", cfg.Provider.MaxTokens)
	}
}

func TestLoad_Defaults(t *testing.T) {
	yaml := `
program: program.md
files: [main.go]
eval:
  command: "go test ./..."
  metric: '(?P<metric>\d+) passed'
  direction: maximize
provider:
  backend: anthropic
  model: claude-sonnet-4-20250514
`
	path := writeTemp(t, yaml)

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if cfg.Eval.Timeout.Duration != 5*time.Minute {
		t.Errorf("default timeout = %v, want 5m", cfg.Eval.Timeout)
	}
	if cfg.Provider.MaxTokens != 16384 {
		t.Errorf("default max_tokens = %d, want 16384", cfg.Provider.MaxTokens)
	}
	if cfg.Git.BranchPrefix != "research/" {
		t.Errorf("default branch_prefix = %q, want %q", cfg.Git.BranchPrefix, "research/")
	}
	if cfg.Provider.MaxRounds != DefaultMaxRounds {
		t.Errorf("default max_rounds = %d, want %d", cfg.Provider.MaxRounds, DefaultMaxRounds)
	}
}

func TestLoad_ValidationErrors(t *testing.T) {
	tests := []struct {
		name string
		yaml string
		want string
	}{
		{
			name: "missing program",
			yaml: `
files: [main.go]
eval:
  command: "go test"
  metric: '(?P<metric>\d+)'
  direction: maximize
provider:
  backend: anthropic
  model: claude-sonnet-4-20250514
`,
			want: "program is required",
		},
		{
			name: "missing files",
			yaml: `
program: program.md
eval:
  command: "go test"
  metric: '(?P<metric>\d+)'
  direction: maximize
provider:
  backend: anthropic
  model: claude-sonnet-4-20250514
`,
			want: "at least one file",
		},
		{
			name: "missing eval command",
			yaml: `
program: program.md
files: [main.go]
eval:
  metric: '(?P<metric>\d+)'
  direction: maximize
provider:
  backend: anthropic
  model: claude-sonnet-4-20250514
`,
			want: "eval.command is required",
		},
		{
			name: "invalid direction",
			yaml: `
program: program.md
files: [main.go]
eval:
  command: "go test"
  metric: '(?P<metric>\d+)'
  direction: sideways
provider:
  backend: anthropic
  model: claude-sonnet-4-20250514
`,
			want: "direction must be",
		},
		{
			name: "missing provider backend",
			yaml: `
program: program.md
files: [main.go]
eval:
  command: "go test"
  metric: '(?P<metric>\d+)'
  direction: maximize
provider:
  model: claude-sonnet-4-20250514
`,
			want: "provider.backend must be",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path := writeTemp(t, tt.yaml)
			_, err := Load(path)
			if err == nil {
				t.Fatal("expected error, got nil")
			}
			if got := err.Error(); !strings.Contains(got, tt.want) {
				t.Errorf("error = %q, want substring %q", got, tt.want)
			}
		})
	}
}

func TestLoad_WithSource(t *testing.T) {
	yaml := `
program: program.md
files: [main.go]
eval:
  command: "go test ./..."
  metric: '(?P<metric>\d+)'
  source: "file:results.json"
  direction: maximize
provider:
  backend: anthropic
  model: claude-sonnet-4-20250514
`
	path := writeTemp(t, yaml)

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !cfg.Eval.Source.IsFile() || cfg.Eval.Source.Path != "results.json" {
		t.Errorf("source = %v, want file:results.json", cfg.Eval.Source)
	}
}

func TestLoad_InvalidSource(t *testing.T) {
	yaml := `
program: program.md
files: [main.go]
eval:
  command: "go test ./..."
  metric: '(?P<metric>\d+)'
  source: "ftp:somewhere"
  direction: maximize
provider:
  backend: anthropic
  model: claude-sonnet-4-20250514
`
	path := writeTemp(t, yaml)

	_, err := Load(path)
	if err == nil {
		t.Fatal("expected error for invalid source")
	}
	if got := err.Error(); !strings.Contains(got, "eval.source") {
		t.Errorf("error = %q, want substring %q", got, "eval.source")
	}
}

func TestLoad_DoubleFileSourceAndMetric(t *testing.T) {
	yaml := `
program: program.md
files: [main.go]
eval:
  command: "go test ./..."
  metric: 'file:a.json:jq:.loss'
  source: "file:b.json"
  direction: maximize
provider:
  backend: anthropic
  model: claude-sonnet-4-20250514
`
	path := writeTemp(t, yaml)

	_, err := Load(path)
	if err == nil {
		t.Fatal("expected error for double file: in source and metric")
	}
	if got := err.Error(); !strings.Contains(got, "cannot both") {
		t.Errorf("error = %q, want substring %q", got, "cannot both")
	}
}

func TestLoad_FileNotFound(t *testing.T) {
	_, err := Load("/nonexistent/research.yaml")
	if err == nil {
		t.Fatal("expected error for missing file")
	}
}

func TestLoad_InvalidYAML(t *testing.T) {
	path := writeTemp(t, "{{invalid yaml")
	_, err := Load(path)
	if err == nil {
		t.Fatal("expected error for invalid YAML")
	}
}

func writeTemp(t *testing.T, content string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "research.yaml")
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}
