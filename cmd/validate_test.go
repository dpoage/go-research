package cmd

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

// validConfig returns a config YAML that references the given program and file paths.
func validConfig(program string, files []string) string {
	fileList := ""
	for _, f := range files {
		fileList += "\n  - " + f
	}
	return `program: ` + program + `
files:` + fileList + `
eval:
  command: "echo 'score: 0.42'"
  metric: 'score:\s+([0-9.]+)'
  direction: minimize
provider:
  backend: anthropic
  model: test-model
  api_key_env: GO_RESEARCH_TEST_KEY
`
}

// setupValidProject creates a temp dir with a valid config, program, and editable file.
// Returns the directory path.
func setupValidProject(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()

	programPath := filepath.Join(dir, "program.md")
	filePath := filepath.Join(dir, "train.py")
	configPath := filepath.Join(dir, "research.yaml")

	os.WriteFile(programPath, []byte("# Program"), 0o644)
	os.WriteFile(filePath, []byte("print('hello')"), 0o644)
	os.WriteFile(configPath, []byte(validConfig("program.md", []string{"train.py"})), 0o644)

	return dir
}

func TestValidate_AllPass(t *testing.T) {
	dir := setupValidProject(t)
	t.Setenv("GO_RESEARCH_TEST_KEY", "fake-key")

	origDir, _ := os.Getwd()
	os.Chdir(dir)
	defer os.Chdir(origDir)

	code := Run(context.Background(), []string{"validate"})
	if code != 0 {
		t.Errorf("exit code = %d, want 0", code)
	}
}

func TestValidate_MissingFile(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("GO_RESEARCH_TEST_KEY", "fake-key")

	programPath := filepath.Join(dir, "program.md")
	configPath := filepath.Join(dir, "research.yaml")

	os.WriteFile(programPath, []byte("# Program"), 0o644)
	// train.py is deliberately NOT created
	os.WriteFile(configPath, []byte(validConfig("program.md", []string{"train.py"})), 0o644)

	origDir, _ := os.Getwd()
	os.Chdir(dir)
	defer os.Chdir(origDir)

	code := Run(context.Background(), []string{"validate"})
	if code != 1 {
		t.Errorf("exit code = %d, want 1", code)
	}
}

func TestValidate_MissingProgram(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("GO_RESEARCH_TEST_KEY", "fake-key")

	filePath := filepath.Join(dir, "train.py")
	configPath := filepath.Join(dir, "research.yaml")

	// program.md is deliberately NOT created
	os.WriteFile(filePath, []byte("print('hello')"), 0o644)
	os.WriteFile(configPath, []byte(validConfig("program.md", []string{"train.py"})), 0o644)

	origDir, _ := os.Getwd()
	os.Chdir(dir)
	defer os.Chdir(origDir)

	code := Run(context.Background(), []string{"validate"})
	if code != 1 {
		t.Errorf("exit code = %d, want 1", code)
	}
}

func TestValidate_FailingEvalCommand(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("GO_RESEARCH_TEST_KEY", "fake-key")

	programPath := filepath.Join(dir, "program.md")
	filePath := filepath.Join(dir, "train.py")
	configPath := filepath.Join(dir, "research.yaml")

	os.WriteFile(programPath, []byte("# Program"), 0o644)
	os.WriteFile(filePath, []byte("print('hello')"), 0o644)

	cfg := `program: program.md
files:
  - train.py
eval:
  command: "exit 1"
  metric: 'score:\s+([0-9.]+)'
  direction: minimize
provider:
  backend: anthropic
  model: test-model
  api_key_env: GO_RESEARCH_TEST_KEY
`
	os.WriteFile(configPath, []byte(cfg), 0o644)

	origDir, _ := os.Getwd()
	os.Chdir(dir)
	defer os.Chdir(origDir)

	code := Run(context.Background(), []string{"validate"})
	if code != 1 {
		t.Errorf("exit code = %d, want 1", code)
	}
}

func TestValidate_MetricNoMatch(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("GO_RESEARCH_TEST_KEY", "fake-key")

	programPath := filepath.Join(dir, "program.md")
	filePath := filepath.Join(dir, "train.py")
	configPath := filepath.Join(dir, "research.yaml")

	os.WriteFile(programPath, []byte("# Program"), 0o644)
	os.WriteFile(filePath, []byte("print('hello')"), 0o644)

	// Command succeeds but output doesn't match the metric regex.
	cfg := `program: program.md
files:
  - train.py
eval:
  command: "echo 'no metric here'"
  metric: 'score:\s+([0-9.]+)'
  direction: minimize
provider:
  backend: anthropic
  model: test-model
  api_key_env: GO_RESEARCH_TEST_KEY
`
	os.WriteFile(configPath, []byte(cfg), 0o644)

	origDir, _ := os.Getwd()
	os.Chdir(dir)
	defer os.Chdir(origDir)

	code := Run(context.Background(), []string{"validate"})
	if code != 1 {
		t.Errorf("exit code = %d, want 1", code)
	}
}

func TestValidate_MissingAPIKey(t *testing.T) {
	dir := setupValidProject(t)
	// Deliberately do NOT set GO_RESEARCH_TEST_KEY
	t.Setenv("GO_RESEARCH_TEST_KEY", "")

	origDir, _ := os.Getwd()
	os.Chdir(dir)
	defer os.Chdir(origDir)

	code := Run(context.Background(), []string{"validate"})
	if code != 1 {
		t.Errorf("exit code = %d, want 1", code)
	}
}

func TestValidate_InvalidConfig(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "research.yaml")
	os.WriteFile(configPath, []byte("not: valid: yaml: ["), 0o644)

	origDir, _ := os.Getwd()
	os.Chdir(dir)
	defer os.Chdir(origDir)

	code := Run(context.Background(), []string{"validate"})
	if code != 1 {
		t.Errorf("exit code = %d, want 1", code)
	}
}
