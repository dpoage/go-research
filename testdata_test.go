package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/dpoage/go-research/config"
)

func TestCounterFixture_ConfigLoads(t *testing.T) {
	cfg, err := config.Load("testdata/counter/research.yaml")
	if err != nil {
		t.Fatalf("config.Load failed: %v", err)
	}

	if cfg.Program != "program.md" {
		t.Errorf("program = %q, want %q", cfg.Program, "program.md")
	}
	if len(cfg.Files) != 1 || cfg.Files[0] != "counter.txt" {
		t.Errorf("files = %v, want [counter.txt]", cfg.Files)
	}
	if cfg.Eval.Direction != config.DirectionMaximize {
		t.Errorf("direction = %q, want %q", cfg.Eval.Direction, config.DirectionMaximize)
	}
	if cfg.Eval.Command != "bash eval.sh" {
		t.Errorf("command = %q, want %q", cfg.Eval.Command, "bash eval.sh")
	}
}

func TestCounterFixture_EvalScript(t *testing.T) {
	cmd := exec.Command("bash", "eval.sh")
	cmd.Dir = "testdata/counter"
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("eval.sh failed: %v\noutput: %s", err, out)
	}

	output := strings.TrimSpace(string(out))
	re := regexp.MustCompile(`count: (\d+)`)
	if !re.MatchString(output) {
		t.Errorf("eval output %q does not match metric pattern %q", output, `count: (\d+)`)
	}
}

func TestCounterFixture_FilesExist(t *testing.T) {
	dir := "testdata/counter"
	for _, name := range []string{"counter.txt", "eval.sh", "research.yaml", "program.md"} {
		path := filepath.Join(dir, name)
		if _, err := os.Stat(path); err != nil {
			t.Errorf("expected file %s to exist: %v", path, err)
		}
	}
}
