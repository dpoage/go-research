package cmd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dpoage/go-research/config"
)

func TestRunInit_NoFlags_ScaffoldsCounter(t *testing.T) {
	dir := t.TempDir()

	err := runInit([]string{"--dir", dir})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	for _, name := range []string{"counter.txt", "eval.sh", "research.yaml", "program.md"} {
		path := filepath.Join(dir, name)
		info, err := os.Stat(path)
		if err != nil {
			t.Errorf("%s not created: %v", name, err)
			continue
		}
		if info.Size() == 0 {
			t.Errorf("%s is empty", name)
		}
	}

	// eval.sh should be executable.
	info, _ := os.Stat(filepath.Join(dir, "eval.sh"))
	if info.Mode().Perm()&0o111 == 0 {
		t.Error("eval.sh is not executable")
	}

	// counter.txt should contain "0".
	data, _ := os.ReadFile(filepath.Join(dir, "counter.txt"))
	if strings.TrimSpace(string(data)) != "0" {
		t.Errorf("counter.txt: got %q, want %q", string(data), "0\n")
	}
}

func TestRunInit_NoFlags_ConfigLoads(t *testing.T) {
	dir := t.TempDir()

	if err := runInit([]string{"--dir", dir}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	cfg, err := config.Load(filepath.Join(dir, "research.yaml"))
	if err != nil {
		t.Fatalf("config.Load failed: %v", err)
	}

	if cfg.Eval.Direction != config.DirectionMaximize {
		t.Errorf("direction: got %q, want %q", cfg.Eval.Direction, config.DirectionMaximize)
	}
	if cfg.Eval.Command != "bash eval.sh" {
		t.Errorf("eval command: got %q, want %q", cfg.Eval.Command, "bash eval.sh")
	}
	if len(cfg.Files) != 1 || cfg.Files[0] != "counter.txt" {
		t.Errorf("files: got %v, want [counter.txt]", cfg.Files)
	}
}

func TestRunInit_WithFile_CustomConfig(t *testing.T) {
	dir := t.TempDir()

	// Create the target file so no warning is emitted.
	os.WriteFile(filepath.Join(dir, "train.py"), []byte("# train"), 0o644)

	err := runInit([]string{
		"--dir", dir,
		"--file", "train.py",
		"--eval", "python train.py",
		"--metric", `loss: (\d+\.\d+)`,
		"--direction", "minimize",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Should create research.yaml and program.md but NOT counter.txt or eval.sh.
	for _, name := range []string{"research.yaml", "program.md"} {
		if _, err := os.Stat(filepath.Join(dir, name)); err != nil {
			t.Errorf("%s not created: %v", name, err)
		}
	}
	for _, name := range []string{"counter.txt", "eval.sh"} {
		if _, err := os.Stat(filepath.Join(dir, name)); err == nil {
			t.Errorf("%s should not exist in custom mode", name)
		}
	}

	// Config should load and have correct values.
	cfg, err := config.Load(filepath.Join(dir, "research.yaml"))
	if err != nil {
		t.Fatalf("config.Load failed: %v", err)
	}
	if cfg.Eval.Command != "python train.py" {
		t.Errorf("eval command: got %q, want %q", cfg.Eval.Command, "python train.py")
	}
	if cfg.Eval.Direction != config.DirectionMinimize {
		t.Errorf("direction: got %q, want %q", cfg.Eval.Direction, config.DirectionMinimize)
	}
	if len(cfg.Files) != 1 || cfg.Files[0] != "train.py" {
		t.Errorf("files: got %v, want [train.py]", cfg.Files)
	}
	if cfg.Provider.Backend != config.BackendAnthropic {
		t.Errorf("backend: got %q, want %q", cfg.Provider.Backend, config.BackendAnthropic)
	}
	if cfg.Provider.Model != defaultModel {
		t.Errorf("model: got %q, want %q", cfg.Provider.Model, defaultModel)
	}
}

func TestRunInit_FileWarnsNonexistent(t *testing.T) {
	dir := t.TempDir()

	// Redirect stderr to capture warning.
	oldStderr := os.Stderr
	r, w, _ := os.Pipe()
	os.Stderr = w

	err := runInit([]string{
		"--dir", dir,
		"--file", "nonexistent.py",
		"--eval", "python nonexistent.py",
		"--metric", `loss: (\d+\.\d+)`,
	})

	w.Close()
	os.Stderr = oldStderr

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	buf := make([]byte, 4096)
	n, _ := r.Read(buf)
	stderr := string(buf[:n])

	if !strings.Contains(stderr, "warning: file nonexistent.py does not exist") {
		t.Errorf("expected warning about nonexistent file, got stderr: %q", stderr)
	}
}

func TestRunInit_SkipsExisting(t *testing.T) {
	dir := t.TempDir()

	// Pre-create research.yaml with custom content.
	existing := filepath.Join(dir, "research.yaml")
	os.WriteFile(existing, []byte("custom"), 0o644)

	err := runInit([]string{"--dir", dir})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Existing file should not be overwritten.
	data, _ := os.ReadFile(existing)
	if string(data) != "custom" {
		t.Errorf("existing file was overwritten: got %q", data)
	}

	// Other files should still be created.
	for _, name := range []string{"counter.txt", "eval.sh", "program.md"} {
		if _, err := os.Stat(filepath.Join(dir, name)); err != nil {
			t.Errorf("%s not created: %v", name, err)
		}
	}
}

func TestRunInit_DefaultValues(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "model.py"), []byte("# model"), 0o644)

	err := runInit([]string{
		"--dir", dir,
		"--file", "model.py",
		"--eval", "python model.py",
		"--metric", `acc: (\d+\.\d+)`,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	cfg, err := config.Load(filepath.Join(dir, "research.yaml"))
	if err != nil {
		t.Fatalf("config.Load failed: %v", err)
	}

	// Check defaults.
	if cfg.Eval.Direction != config.DirectionMinimize {
		t.Errorf("default direction: got %q, want %q", cfg.Eval.Direction, config.DirectionMinimize)
	}
	if cfg.Provider.Backend != config.BackendAnthropic {
		t.Errorf("default backend: got %q, want %q", cfg.Provider.Backend, config.BackendAnthropic)
	}
	if cfg.Provider.Model != defaultModel {
		t.Errorf("default model: got %q, want %q", cfg.Provider.Model, defaultModel)
	}
}

func TestRunInit_FileRequiresEvalAndMetric(t *testing.T) {
	dir := t.TempDir()

	err := runInit([]string{"--dir", dir, "--file", "train.py"})
	if err == nil {
		t.Fatal("expected error when --file given without --eval")
	}
	if !strings.Contains(err.Error(), "--eval is required") {
		t.Errorf("unexpected error: %v", err)
	}

	err = runInit([]string{"--dir", dir, "--file", "train.py", "--eval", "python train.py"})
	if err == nil {
		t.Fatal("expected error when --file given without --metric")
	}
	if !strings.Contains(err.Error(), "--metric is required") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestRunInit_MultipleFiles(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "a.py"), []byte("# a"), 0o644)
	os.WriteFile(filepath.Join(dir, "b.py"), []byte("# b"), 0o644)

	err := runInit([]string{
		"--dir", dir,
		"--file", "a.py",
		"--file", "b.py",
		"--eval", "python run.py",
		"--metric", `score: (\d+)`,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	cfg, err := config.Load(filepath.Join(dir, "research.yaml"))
	if err != nil {
		t.Fatalf("config.Load failed: %v", err)
	}
	if len(cfg.Files) != 2 {
		t.Errorf("files: got %v, want 2 files", cfg.Files)
	}
}
