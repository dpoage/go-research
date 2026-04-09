package experiment

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/dpoage/go-research/config"
	"github.com/dpoage/go-research/llm"
	"github.com/dpoage/go-research/tools"
)

// mockProvider is a fake LLM that returns pre-scripted responses.
type mockProvider struct {
	// responses is consumed in order. Each call pops the first element.
	responses []*llm.Response
	calls     int
}

func (m *mockProvider) Complete(_ context.Context, _ *llm.Request) (*llm.Response, error) {
	if m.calls >= len(m.responses) {
		return &llm.Response{
			Content:    []llm.ContentBlock{{Type: llm.BlockText, Text: "done"}},
			StopReason: llm.StopEndTurn,
		}, nil
	}
	resp := m.responses[m.calls]
	m.calls++
	return resp, nil
}

func TestLoop_EndToEnd(t *testing.T) {
	dir := initTestRepo(t)
	chdir(t, dir)

	// Create a trivial "counter" fixture: a file with a number, eval reads it.
	counterPath := filepath.Join(dir, "counter.txt")
	if err := os.WriteFile(counterPath, []byte("10"), 0644); err != nil {
		t.Fatal(err)
	}

	// Create the eval script: prints the counter value as a metric.
	evalScript := filepath.Join(dir, "eval.sh")
	if err := os.WriteFile(evalScript, []byte("#!/bin/sh\necho \"metric: $(cat counter.txt)\"\n"), 0755); err != nil {
		t.Fatal(err)
	}

	// Create program.md.
	programPath := filepath.Join(dir, "program.md")
	if err := os.WriteFile(programPath, []byte("You are a test agent."), 0644); err != nil {
		t.Fatal(err)
	}

	// Stage and commit fixtures so git revert works cleanly.
	for _, args := range [][]string{
		{"git", "add", "."},
		{"git", "commit", "-m", "add fixtures"},
	} {
		cmd := exec.Command(args[0], args[1:]...)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("%v: %s: %s", args, err, out)
		}
	}

	cfg := &config.Config{
		Program: programPath,
		Files:   []string{"counter.txt"},
		Eval: config.EvalConfig{
			Command:   "sh " + evalScript,
			Metric:    `metric:\s+(\d+)`,
			Source:    config.NewSourceStdout(),
			Direction: config.DirectionMinimize,
			Timeout:   config.Duration{Duration: 10 * time.Second},
		},
		Provider: config.ProviderConfig{
			Backend:   config.BackendAnthropic,
			Model:     "test",
			MaxTokens: 1024,
		},
		Git: config.GitConfig{
			Enabled:      true,
			BranchPrefix: "test/",
		},
	}

	sandbox, err := tools.NewSandbox(dir, cfg.Files)
	if err != nil {
		t.Fatal(err)
	}
	executor := tools.NewExecutor(sandbox, 10*time.Second)

	eval, err := NewEval(cfg.Eval)
	if err != nil {
		t.Fatal(err)
	}

	git := NewGit(true, dir, cfg.Files)
	if _, err := git.CreateBranch(cfg.Git.BranchPrefix); err != nil {
		t.Fatal(err)
	}

	resultsPath := filepath.Join(dir, "results.tsv")
	logger, err := NewResultLogger(resultsPath)
	if err != nil {
		t.Fatal(err)
	}

	// Mock provider: iteration 1 writes "5" (improvement from 10), iteration 2 writes "15" (regression, reverted).
	provider := &mockProvider{
		responses: []*llm.Response{
			// Iter 1: write_file to set counter to 5.
			{
				Content: []llm.ContentBlock{{
					Type:  llm.BlockToolUse,
					ID:    "call_1",
					Name:  tools.ToolWriteFile,
					Input: json.RawMessage(fmt.Sprintf(`{"path":%q,"content":"5"}`, counterPath)),
				}},
				StopReason: llm.StopToolUse,
			},
			// Iter 1: tool result processed, model says done.
			{
				Content:    []llm.ContentBlock{{Type: llm.BlockText, Text: "Wrote 5 to counter"}},
				StopReason: llm.StopEndTurn,
			},
			// Iter 2: write_file to set counter to 15 (regression).
			{
				Content: []llm.ContentBlock{{
					Type:  llm.BlockToolUse,
					ID:    "call_2",
					Name:  tools.ToolWriteFile,
					Input: json.RawMessage(fmt.Sprintf(`{"path":%q,"content":"15"}`, counterPath)),
				}},
				StopReason: llm.StopToolUse,
			},
			// Iter 2: model says done.
			{
				Content:    []llm.ContentBlock{{Type: llm.BlockText, Text: "Wrote 15 to counter"}},
				StopReason: llm.StopEndTurn,
			},
		},
	}

	loop := NewLoop(cfg, provider, executor, eval, git, logger, resultsPath)

	if err := loop.Run(context.Background(), 2); err != nil {
		t.Fatal(err)
	}

	// Verify results log has 2 entries.
	data, err := os.ReadFile(resultsPath)
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	if len(lines) != 3 { // header + 2 entries
		t.Fatalf("expected 3 lines in results, got %d:\n%s", len(lines), data)
	}

	// First iteration should be "keep" (5 < 10).
	if !strings.Contains(lines[1], "keep") {
		t.Errorf("iter 1 should be keep: %s", lines[1])
	}

	// Second iteration should be "discard" (15 > 5 when minimizing).
	if !strings.Contains(lines[2], "discard") {
		t.Errorf("iter 2 should be discard: %s", lines[2])
	}

	// Counter should be 5 (reverted from 15 back to committed state).
	counterData, err := os.ReadFile(counterPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(counterData) != "5" {
		t.Errorf("counter should be 5 after revert, got %q", counterData)
	}
}

func TestLoop_ContextCancellation(t *testing.T) {
	dir := initTestRepo(t)
	chdir(t, dir)

	programPath := filepath.Join(dir, "program.md")
	if err := os.WriteFile(programPath, []byte("test"), 0644); err != nil {
		t.Fatal(err)
	}
	counterPath := filepath.Join(dir, "counter.txt")
	if err := os.WriteFile(counterPath, []byte("1"), 0644); err != nil {
		t.Fatal(err)
	}

	for _, args := range [][]string{
		{"git", "add", "."},
		{"git", "commit", "-m", "fixtures"},
	} {
		cmd := exec.Command(args[0], args[1:]...)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("%v: %s: %s", args, err, out)
		}
	}

	cfg := &config.Config{
		Program: programPath,
		Files:   []string{"counter.txt"},
		Eval: config.EvalConfig{
			Command:   "echo 'metric: 1'",
			Metric:    `metric:\s+(\d+)`,
			Source:    config.NewSourceStdout(),
			Direction: config.DirectionMinimize,
			Timeout:   config.Duration{Duration: 5 * time.Second},
		},
		Provider: config.ProviderConfig{MaxTokens: 1024},
		Git:      config.GitConfig{Enabled: false},
	}

	sandbox, _ := tools.NewSandbox(dir, cfg.Files)
	executor := tools.NewExecutor(sandbox, 5*time.Second)
	eval, _ := NewEval(cfg.Eval)
	git := NewGit(false, dir, nil)
	resultsPath := filepath.Join(dir, "results.tsv")
	logger, _ := NewResultLogger(resultsPath)

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately.

	loop := NewLoop(cfg, &mockProvider{}, executor, eval, git, logger, resultsPath)

	err := loop.Run(ctx, 10)
	if err == nil {
		t.Error("expected context cancellation error")
	}
}

func TestToolDefs(t *testing.T) {
	defs := ToolDefs()
	if len(defs) != 3 {
		t.Fatalf("expected 3 tool defs, got %d", len(defs))
	}

	names := map[string]bool{}
	for _, d := range defs {
		names[d.Name] = true
		// Verify input schema is valid JSON.
		var schema map[string]interface{}
		if err := json.Unmarshal(d.InputSchema, &schema); err != nil {
			t.Errorf("tool %s has invalid input schema: %v", d.Name, err)
		}
	}

	for _, expected := range []string{tools.ToolReadFile, tools.ToolWriteFile, tools.ToolRunCommand} {
		if !names[expected] {
			t.Errorf("missing tool def: %s", expected)
		}
	}
}
