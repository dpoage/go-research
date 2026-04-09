package cmd

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dpoage/go-research/config"
	"github.com/dpoage/go-research/experiment"
	"github.com/dpoage/go-research/llm"
	"github.com/dpoage/go-research/tools"
)

// --- parseRunFlags ---

func TestParseRunFlags_Defaults(t *testing.T) {
	maxIter, resultFile, verbose, err := parseRunFlags(nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if maxIter != 0 {
		t.Errorf("maxIter = %d, want 0", maxIter)
	}
	if resultFile != defaultResultsFile {
		t.Errorf("resultFile = %q, want %q", resultFile, defaultResultsFile)
	}
	if verbose {
		t.Error("verbose = true, want false")
	}
}

func TestParseRunFlags_Custom(t *testing.T) {
	maxIter, resultFile, verbose, err := parseRunFlags([]string{
		"--max-iter", "10",
		"--results", "custom.tsv",
		"--verbose",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if maxIter != 10 {
		t.Errorf("maxIter = %d, want 10", maxIter)
	}
	if resultFile != "custom.tsv" {
		t.Errorf("resultFile = %q, want %q", resultFile, "custom.tsv")
	}
	if !verbose {
		t.Error("verbose = false, want true")
	}
}

func TestParseRunFlags_BadFlag(t *testing.T) {
	_, _, _, err := parseRunFlags([]string{"--bogus"})
	if err == nil {
		t.Fatal("expected error for unknown flag")
	}
}

// --- buildRunDeps ---

func runDepsConfig(apiKeyEnv string) string {
	return `program: program.md
files:
  - train.py
eval:
  command: "echo 'score: 42'"
  metric: 'score:\s+(\d+)'
  direction: minimize
provider:
  backend: anthropic
  model: test-model
  api_key_env: ` + apiKeyEnv + `
`
}

func setupRunProject(t *testing.T, apiKeyEnv string) string {
	t.Helper()
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "research.yaml"), []byte(runDepsConfig(apiKeyEnv)), 0o644)
	os.WriteFile(filepath.Join(dir, "program.md"), []byte("# prog"), 0o644)
	os.WriteFile(filepath.Join(dir, "train.py"), []byte("x"), 0o644)
	return dir
}

func TestBuildRunDeps_Success(t *testing.T) {
	dir := setupRunProject(t, "RUN_TEST_KEY")
	t.Setenv("RUN_TEST_KEY", "fake-key")

	origDir, _ := os.Getwd()
	os.Chdir(dir)
	defer os.Chdir(origDir)

	d, err := buildRunDeps(globalFlags{config: "research.yaml"}, 5, "results.tsv", true)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if d.cfg == nil {
		t.Error("cfg is nil")
	}
	if d.provider == nil {
		t.Error("provider is nil")
	}
	if d.executor == nil {
		t.Error("executor is nil")
	}
	if d.eval == nil {
		t.Error("eval is nil")
	}
	if d.git == nil {
		t.Error("git is nil")
	}
	if d.logger == nil {
		t.Error("logger is nil")
	}
	if d.maxIter != 5 {
		t.Errorf("maxIter = %d, want 5", d.maxIter)
	}
	if !d.verbose {
		t.Error("verbose = false, want true")
	}
}

func TestBuildRunDeps_MissingConfig(t *testing.T) {
	_, err := buildRunDeps(globalFlags{config: "/nonexistent/config.yaml"}, 0, "results.tsv", false)
	if err == nil {
		t.Fatal("expected error for missing config")
	}
}

func TestBuildRunDeps_BadProvider(t *testing.T) {
	dir := setupRunProject(t, "RUN_TEST_KEY_MISSING")
	// Don't set the env var.

	origDir, _ := os.Getwd()
	os.Chdir(dir)
	defer os.Chdir(origDir)

	_, err := buildRunDeps(globalFlags{config: "research.yaml"}, 0, "results.tsv", false)
	if err == nil {
		t.Fatal("expected error for missing API key")
	}
	if !strings.Contains(err.Error(), "create provider") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestBuildRunDeps_BadEval(t *testing.T) {
	dir := t.TempDir()
	cfg := `program: program.md
files:
  - train.py
eval:
  command: "echo 1"
  metric: '(unclosed'
  direction: minimize
provider:
  backend: anthropic
  model: test-model
  api_key_env: RUN_TEST_KEY
`
	os.WriteFile(filepath.Join(dir, "research.yaml"), []byte(cfg), 0o644)
	os.WriteFile(filepath.Join(dir, "program.md"), []byte("# prog"), 0o644)
	os.WriteFile(filepath.Join(dir, "train.py"), []byte("x"), 0o644)
	t.Setenv("RUN_TEST_KEY", "fake-key")

	origDir, _ := os.Getwd()
	os.Chdir(dir)
	defer os.Chdir(origDir)

	_, err := buildRunDeps(globalFlags{config: "research.yaml"}, 0, "results.tsv", false)
	if err == nil {
		t.Fatal("expected error for bad regex")
	}
	if !strings.Contains(err.Error(), "create eval") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestBuildRunDeps_BadResultPath(t *testing.T) {
	dir := setupRunProject(t, "RUN_TEST_KEY")
	t.Setenv("RUN_TEST_KEY", "fake-key")

	origDir, _ := os.Getwd()
	os.Chdir(dir)
	defer os.Chdir(origDir)

	_, err := buildRunDeps(globalFlags{config: "research.yaml"}, 0, "/nonexistent/dir/results.tsv", false)
	if err == nil {
		t.Fatal("expected error for bad result path")
	}
	if !strings.Contains(err.Error(), "create result logger") {
		t.Errorf("unexpected error: %v", err)
	}
}

// --- executeRun ---

// mockRunProvider returns a single end_turn response so the loop exits after one iteration.
type mockRunProvider struct{}

func (m mockRunProvider) Complete(_ context.Context, _ *llm.Request) (*llm.Response, error) {
	return &llm.Response{
		Content:    []llm.ContentBlock{{Type: llm.BlockText, Text: "done"}},
		StopReason: llm.StopEndTurn,
	}, nil
}

func TestExecuteRun_Verbose(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "program.md"), []byte("# prog"), 0o644)
	os.WriteFile(filepath.Join(dir, "train.py"), []byte("x"), 0o644)

	origDir, _ := os.Getwd()
	os.Chdir(dir)
	defer os.Chdir(origDir)

	eval, _ := experiment.NewEval(config.EvalConfig{
		Command:   "echo 'score: 42'",
		Metric:    `score:\s+(\d+)`,
		Direction: config.DirectionMaximize,
	})
	logger, _ := experiment.NewResultLogger(filepath.Join(dir, "results.tsv"))
	sandbox, _ := tools.NewSandbox(".", []string{"train.py"})

	d := &runDeps{
		cfg: &config.Config{
			Program:  "program.md",
			Files:    []string{"train.py"},
			Eval:     config.EvalConfig{Command: "echo 'score: 42'", Metric: `score:\s+(\d+)`, Direction: config.DirectionMaximize},
			Provider: config.ProviderConfig{Backend: config.BackendAnthropic, Model: "test", MaxTokens: 1024, MaxRounds: config.DefaultMaxRounds},
		},
		provider: mockRunProvider{},
		executor: tools.NewExecutor(sandbox, defaultToolTimeout),
		eval:     eval,
		git:      experiment.NewGit(false, ".", []string{"train.py"}),
		logger:   logger,
		maxIter:  1,
		verbose:  true,
	}

	err := executeRun(context.Background(), d, false)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestExecuteRun_Quiet(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "program.md"), []byte("# prog"), 0o644)
	os.WriteFile(filepath.Join(dir, "train.py"), []byte("x"), 0o644)

	origDir, _ := os.Getwd()
	os.Chdir(dir)
	defer os.Chdir(origDir)

	eval, _ := experiment.NewEval(config.EvalConfig{
		Command:   "echo 'score: 42'",
		Metric:    `score:\s+(\d+)`,
		Direction: config.DirectionMaximize,
	})
	logger, _ := experiment.NewResultLogger(filepath.Join(dir, "results.tsv"))
	sandbox, _ := tools.NewSandbox(".", []string{"train.py"})

	d := &runDeps{
		cfg: &config.Config{
			Program:  "program.md",
			Files:    []string{"train.py"},
			Eval:     config.EvalConfig{Command: "echo 'score: 42'", Metric: `score:\s+(\d+)`, Direction: config.DirectionMaximize},
			Provider: config.ProviderConfig{Backend: config.BackendAnthropic, Model: "test", MaxTokens: 1024, MaxRounds: config.DefaultMaxRounds},
		},
		provider: mockRunProvider{},
		executor: tools.NewExecutor(sandbox, defaultToolTimeout),
		eval:     eval,
		git:      experiment.NewGit(false, ".", []string{"train.py"}),
		logger:   logger,
		maxIter:  1,
		verbose:  false,
	}

	err := executeRun(context.Background(), d, true)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestExecuteRun_WithMaxIterPrint(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "program.md"), []byte("# prog"), 0o644)
	os.WriteFile(filepath.Join(dir, "train.py"), []byte("x"), 0o644)

	origDir, _ := os.Getwd()
	os.Chdir(dir)
	defer os.Chdir(origDir)

	eval, _ := experiment.NewEval(config.EvalConfig{
		Command:   "echo 'score: 42'",
		Metric:    `score:\s+(\d+)`,
		Direction: config.DirectionMaximize,
	})
	logger, _ := experiment.NewResultLogger(filepath.Join(dir, "results.tsv"))
	sandbox, _ := tools.NewSandbox(".", []string{"train.py"})

	d := &runDeps{
		cfg: &config.Config{
			Program:  "program.md",
			Files:    []string{"train.py"},
			Eval:     config.EvalConfig{Command: "echo 'score: 42'", Metric: `score:\s+(\d+)`, Direction: config.DirectionMaximize},
			Provider: config.ProviderConfig{Backend: config.BackendAnthropic, Model: "test", MaxTokens: 1024, MaxRounds: config.DefaultMaxRounds},
		},
		provider: mockRunProvider{},
		executor: tools.NewExecutor(sandbox, defaultToolTimeout),
		eval:     eval,
		git:      experiment.NewGit(false, ".", []string{"train.py"}),
		logger:   logger,
		maxIter:  5,
		verbose:  false,
	}

	// Not quiet — should print config and max iterations.
	err := executeRun(context.Background(), d, false)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

// --- runRun integration ---

func TestRunRun_ViaRootRun(t *testing.T) {
	dir := t.TempDir()
	origDir, _ := os.Getwd()
	os.Chdir(dir)
	defer os.Chdir(origDir)

	code := Run(context.Background(), []string{"run"})
	if code != 1 {
		t.Errorf("exit code = %d, want 1", code)
	}
}
