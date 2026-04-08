package cmd

import (
	"fmt"
	"os"
	"path/filepath"

	flag "github.com/spf13/pflag"
)

const defaultConfigYAML = `# go-research configuration
# See: https://github.com/dpoage/go-research

# Markdown file with agent instructions (the "program").
program: program.md

# Files the agent is allowed to edit.
files:
  - train.py

# Evaluation settings.
eval:
  # Shell command to run for each experiment.
  command: "python train.py"

  # Regex with a named group 'metric' to extract the score.
  # For JSON output, prefix with "jq:" e.g. "jq:.results.val_bpb"
  metric: 'val_bpb:\s+(?P<metric>[0-9.]+)'

  # "minimize" or "maximize"
  direction: minimize

  # Wall-clock time budget per experiment.
  timeout: 5m

# LLM provider configuration.
provider:
  # Backend: "anthropic" or "openai"
  backend: anthropic
  model: claude-sonnet-4-20250514
  api_key_env: ANTHROPIC_API_KEY
  max_tokens: 16384

# Git integration.
git:
  enabled: true
  branch_prefix: "research/"
`

const defaultProgramMD = `# Research Program

You are an autonomous research agent. Your goal is to iteratively improve
the target metric by modifying the allowed files.

## Rules

1. Make one focused change per experiment.
2. Read the current code before modifying it.
3. After each change, the eval command will run automatically.
4. If the metric improves, your change is kept. Otherwise it is reverted.
5. Think step by step about what to try next based on previous results.

## Strategy

- Start by reading and understanding the existing code.
- Try small, targeted improvements first.
- If a change doesn't help, try a different approach rather than the same thing again.
- Keep changes minimal and reviewable.
`

func runInit(args []string) error {
	fs := flag.NewFlagSet("init", flag.ContinueOnError)
	dir := fs.String("dir", ".", "directory to initialize")
	if err := fs.Parse(args); err != nil {
		return err
	}

	configPath := filepath.Join(*dir, "research.yaml")
	programPath := filepath.Join(*dir, "program.md")

	wrote := 0
	for _, f := range []struct {
		path    string
		content string
	}{
		{configPath, defaultConfigYAML},
		{programPath, defaultProgramMD},
	} {
		if _, err := os.Stat(f.path); err == nil {
			fmt.Fprintf(os.Stderr, "skipping %s (already exists)\n", f.path)
			continue
		}
		if err := os.WriteFile(f.path, []byte(f.content), 0o644); err != nil {
			return fmt.Errorf("write %s: %w", f.path, err)
		}
		fmt.Fprintf(os.Stderr, "created %s\n", f.path)
		wrote++
	}

	if wrote == 0 {
		fmt.Fprintln(os.Stderr, "nothing to do (files already exist)")
	}
	return nil
}
