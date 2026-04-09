package cmd

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"text/template"
	"time"

	"github.com/dpoage/go-research/config"
	flag "github.com/spf13/pflag"
)

// Default flag values for init.
const (
	defaultModel   = "claude-sonnet-4-20250514"
	defaultTimeout = "5m"
)

var (
	defaultDirection = config.DirectionMinimize
	defaultBackend   = config.BackendAnthropic
)

// Counter example content scaffolded when no --file flags are given.
const counterTxt = "0\n"

const counterEvalSh = `#!/bin/bash
value=$(cat counter.txt)
echo "count: $value"
`

const counterProgramMD = `# Counter Research Program

You are an autonomous research agent. Your goal is to maximize the counter value
in counter.txt.

## Rules

1. Read counter.txt to see the current value.
2. Increment the value by a small amount.
3. Write the updated value back to counter.txt.
4. The eval command will check the new value automatically.

## Strategy

- Read the current value first.
- Increment by 1 each iteration.
- Keep changes minimal.
`

const customProgramMDTemplate = `# Research Program

You are an autonomous research agent. Your goal is to iteratively improve
the target metric by modifying the allowed files.

## Evaluation

The eval command is: {{.Eval}}
The metric extractor is: {{.Metric}}
The goal is to {{.Direction}} the metric.

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

const configYAMLTemplate = `program: program.md
files:
{{- range .Files}}
  - {{.}}
{{- end}}
eval:
  command: "{{.Eval}}"
  # source: stdout             # or file:<path>
  metric: '{{.Metric}}'
  direction: {{.Direction}}
  timeout: {{.Timeout}}
provider:
  backend: {{.Backend}}
  model: {{.Model}}
  api_key_env: {{.APIKeyEnv}}
  max_tokens: {{.MaxTokens}}
git:
  enabled: {{.GitEnabled}}
  branch_prefix: "research/"
`

// initParams holds the template parameters for generating config and program files.
type initParams struct {
	Files      []string
	Eval       string
	Metric     string
	Direction  config.Direction
	Timeout    string
	Backend    config.Backend
	Model      string
	APIKeyEnv  string
	MaxTokens  int
	GitEnabled bool
}

func runInit(args []string) error {
	fs := flag.NewFlagSet("init", flag.ContinueOnError)
	dir := fs.String("dir", ".", "directory to initialize")
	files := fs.StringSlice("file", nil, "editable file(s)")
	eval := fs.String("eval", "", "eval command")
	metric := fs.String("metric", "", "metric regex")
	direction := fs.String("direction", string(defaultDirection), "minimize or maximize")
	backend := fs.String("backend", string(defaultBackend), "LLM backend (anthropic or openai)")
	model := fs.String("model", defaultModel, "LLM model name")
	timeout := fs.String("timeout", defaultTimeout, "eval timeout (e.g. 5m, 30s)")
	if err := fs.Parse(args); err != nil {
		return err
	}

	if len(*files) == 0 {
		return scaffoldCounter(*dir)
	}
	return scaffoldCustom(*dir, initParams{
		Files:     *files,
		Eval:      *eval,
		Metric:    *metric,
		Direction: config.Direction(*direction),
		Timeout:   *timeout,
		Backend:   config.Backend(*backend),
		Model:     *model,
	})
}

// scaffoldCounter writes the self-contained counter example.
func scaffoldCounter(dir string) error {
	params := initParams{
		Files:      []string{"counter.txt"},
		Eval:       "bash eval.sh",
		Metric:     `count: (\d+)`,
		Direction:  config.DirectionMaximize,
		Timeout:    "30s",
		Backend:    defaultBackend,
		Model:      defaultModel,
		APIKeyEnv:  "ANTHROPIC_API_KEY",
		MaxTokens:  config.DefaultMaxTokens,
		GitEnabled: false,
	}

	configYAML, err := renderTemplate(configYAMLTemplate, params)
	if err != nil {
		return fmt.Errorf("render config: %w", err)
	}

	return writeFiles([]fileToWrite{
		{filepath.Join(dir, "counter.txt"), counterTxt, 0o644},
		{filepath.Join(dir, "eval.sh"), counterEvalSh, 0o755},
		{filepath.Join(dir, "research.yaml"), configYAML, 0o644},
		{filepath.Join(dir, "program.md"), counterProgramMD, 0o644},
	})
}

// scaffoldCustom writes a custom research.yaml and program.md from flag values.
func scaffoldCustom(dir string, params initParams) error {
	for _, f := range params.Files {
		path := filepath.Join(dir, f)
		if _, err := os.Stat(path); os.IsNotExist(err) {
			fmt.Fprintf(os.Stderr, "warning: file %s does not exist\n", f)
		}
	}

	if params.Eval == "" {
		return fmt.Errorf("--eval is required when --file is specified")
	}
	if params.Metric == "" {
		return fmt.Errorf("--metric is required when --file is specified")
	}

	if _, err := time.ParseDuration(params.Timeout); err != nil {
		return fmt.Errorf("invalid timeout %q: %w", params.Timeout, err)
	}

	params.APIKeyEnv = apiKeyEnvForBackend(params.Backend)
	params.MaxTokens = config.DefaultMaxTokens
	params.GitEnabled = true

	configYAML, err := renderTemplate(configYAMLTemplate, params)
	if err != nil {
		return fmt.Errorf("render config: %w", err)
	}

	programMD, err := renderTemplate(customProgramMDTemplate, params)
	if err != nil {
		return fmt.Errorf("render program: %w", err)
	}

	return writeFiles([]fileToWrite{
		{filepath.Join(dir, "research.yaml"), configYAML, 0o644},
		{filepath.Join(dir, "program.md"), programMD, 0o644},
	})
}

func renderTemplate(tmpl string, data any) (string, error) {
	t, err := template.New("").Parse(tmpl)
	if err != nil {
		return "", err
	}
	var buf bytes.Buffer
	if err := t.Execute(&buf, data); err != nil {
		return "", err
	}
	return buf.String(), nil
}

func apiKeyEnvForBackend(backend config.Backend) string {
	switch backend {
	case config.BackendOpenAI:
		return "OPENAI_API_KEY"
	default:
		return "ANTHROPIC_API_KEY"
	}
}

type fileToWrite struct {
	path    string
	content string
	mode    os.FileMode
}

// writeFiles writes each file, skipping those that already exist.
func writeFiles(files []fileToWrite) error {
	wrote := 0
	for _, f := range files {
		if _, err := os.Stat(f.path); err == nil {
			fmt.Fprintf(os.Stderr, "skipping %s (already exists)\n", f.path)
			continue
		}
		if err := os.WriteFile(f.path, []byte(f.content), f.mode); err != nil {
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
