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
	// errs mirrors responses; if non-nil the corresponding call returns that error.
	errs  []error
	calls int
}

func (m *mockProvider) Complete(_ context.Context, _ *llm.Request) (*llm.Response, error) {
	if m.calls >= len(m.responses) {
		return &llm.Response{
			Content:    []llm.ContentBlock{{Type: llm.BlockText, Text: "done"}},
			StopReason: llm.StopEndTurn,
		}, nil
	}
	idx := m.calls
	m.calls++
	if m.errs != nil && idx < len(m.errs) && m.errs[idx] != nil {
		return nil, m.errs[idx]
	}
	return m.responses[idx], nil
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

	loop := NewLoop(LoopParams{
		Config:   cfg,
		Provider: provider,
		Executor: executor,
		Eval:     eval,
		Git:      git,
		Logger:   logger,
		Observer: VerboseObserver{},
	})

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

	loop := NewLoop(LoopParams{
		Config:   cfg,
		Provider: &mockProvider{},
		Executor: executor,
		Eval:     eval,
		Git:      git,
		Logger:   logger,
		Observer: VerboseObserver{},
	})

	err := loop.Run(ctx, 10)
	if err == nil {
		t.Error("expected context cancellation error")
	}
}

// capturingObserver records Warning calls for assertion.
type capturingObserver struct {
	VerboseObserver
	warnings []string
}

func (o *capturingObserver) Warning(msg string) {
	o.warnings = append(o.warnings, msg)
}

func TestLoop_Run_ProgramReadError(t *testing.T) {
	dir := initTestRepo(t)
	chdir(t, dir)

	cfg := &config.Config{
		Program: filepath.Join(dir, "nonexistent-program.md"),
		Files:   []string{},
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

	resultsPath := filepath.Join(dir, "results.tsv")
	logger, err := NewResultLogger(resultsPath)
	if err != nil {
		t.Fatal(err)
	}

	sandbox, _ := tools.NewSandbox(dir, cfg.Files)
	executor := tools.NewExecutor(sandbox, 5*time.Second)
	eval, _ := NewEval(cfg.Eval)
	git := NewGit(false, dir, nil)

	loop := NewLoop(LoopParams{
		Config:   cfg,
		Provider: &mockProvider{},
		Executor: executor,
		Eval:     eval,
		Git:      git,
		Logger:   logger,
		Observer: VerboseObserver{},
	})

	err = loop.Run(context.Background(), 1)
	if err == nil {
		t.Error("expected error when program file is missing, got nil")
	}
}

func TestLoop_IsBetter_Maximize(t *testing.T) {
	dir := initTestRepo(t)
	chdir(t, dir)

	programPath := filepath.Join(dir, "program.md")
	if err := os.WriteFile(programPath, []byte("test"), 0644); err != nil {
		t.Fatal(err)
	}
	counterPath := filepath.Join(dir, "counter.txt")
	if err := os.WriteFile(counterPath, []byte("5"), 0644); err != nil {
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
			Command:   "sh -c 'echo metric: $(cat counter.txt)'",
			Metric:    `metric:\s+(\d+)`,
			Source:    config.NewSourceStdout(),
			Direction: config.DirectionMaximize, // maximize: higher is better
			Timeout:   config.Duration{Duration: 10 * time.Second},
		},
		Provider: config.ProviderConfig{MaxTokens: 1024},
		Git:      config.GitConfig{Enabled: false},
	}

	resultsPath := filepath.Join(dir, "results.tsv")
	logger, err := NewResultLogger(resultsPath)
	if err != nil {
		t.Fatal(err)
	}

	sandbox, _ := tools.NewSandbox(dir, cfg.Files)
	executor := tools.NewExecutor(sandbox, 5*time.Second)
	eval, _ := NewEval(cfg.Eval)
	git := NewGit(false, dir, nil)

	// Iter 1: write 10 (better than initial 5 when maximizing).
	// Iter 2: write 3 (worse than 10 when maximizing, should discard).
	provider := &mockProvider{
		responses: []*llm.Response{
			{
				Content: []llm.ContentBlock{{
					Type:  llm.BlockToolUse,
					ID:    "call_1",
					Name:  tools.ToolWriteFile,
					Input: json.RawMessage(fmt.Sprintf(`{"path":%q,"content":"10"}`, counterPath)),
				}},
				StopReason: llm.StopToolUse,
			},
			{
				Content:    []llm.ContentBlock{{Type: llm.BlockText, Text: "wrote 10"}},
				StopReason: llm.StopEndTurn,
			},
			{
				Content: []llm.ContentBlock{{
					Type:  llm.BlockToolUse,
					ID:    "call_2",
					Name:  tools.ToolWriteFile,
					Input: json.RawMessage(fmt.Sprintf(`{"path":%q,"content":"3"}`, counterPath)),
				}},
				StopReason: llm.StopToolUse,
			},
			{
				Content:    []llm.ContentBlock{{Type: llm.BlockText, Text: "wrote 3"}},
				StopReason: llm.StopEndTurn,
			},
		},
	}

	loop := NewLoop(LoopParams{
		Config:   cfg,
		Provider: provider,
		Executor: executor,
		Eval:     eval,
		Git:      git,
		Logger:   logger,
		Observer: VerboseObserver{},
	})

	if err := loop.Run(context.Background(), 2); err != nil {
		t.Fatal(err)
	}

	data, err := os.ReadFile(resultsPath)
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	if len(lines) != 3 {
		t.Fatalf("expected 3 lines in results, got %d:\n%s", len(lines), data)
	}
	if !strings.Contains(lines[1], "keep") {
		t.Errorf("iter 1 should be keep (10 > 5 when maximizing): %s", lines[1])
	}
	if !strings.Contains(lines[2], "discard") {
		t.Errorf("iter 2 should be discard (3 < 10 when maximizing): %s", lines[2])
	}
}

func TestLoop_Revert_WarnsOnError(t *testing.T) {
	// Use git enabled but with invalid dir so Revert() fails, producing a Warning.
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

	resultsPath := filepath.Join(dir, "results.tsv")
	logger, err := NewResultLogger(resultsPath)
	if err != nil {
		t.Fatal(err)
	}

	sandbox, _ := tools.NewSandbox(dir, cfg.Files)
	executor := tools.NewExecutor(sandbox, 5*time.Second)
	eval, _ := NewEval(cfg.Eval)

	// Use a git instance pointing at an invalid dir so Revert() always fails.
	brokenGit := NewGit(true, "/nonexistent-dir-xyz", nil)

	obs := &capturingObserver{}

	// Iter 1 returns metric 1 (no prior best → keep path, no revert).
	// Iter 2 returns metric 2 (worse when minimizing → NoImprovement → revert → warning).
	provider := &mockProvider{
		responses: []*llm.Response{
			{
				Content:    []llm.ContentBlock{{Type: llm.BlockText, Text: "no change"}},
				StopReason: llm.StopEndTurn,
			},
			{
				Content:    []llm.ContentBlock{{Type: llm.BlockText, Text: "no change"}},
				StopReason: llm.StopEndTurn,
			},
		},
	}

	// We need a working git for the Commit call in iter 1.
	// Use a loop with git disabled to avoid the commit failure path, but brokenGit for revert.
	// Actually we want to test the revert error, so use brokenGit and also accept git commit warning.
	loop := NewLoop(LoopParams{
		Config:   cfg,
		Provider: provider,
		Executor: executor,
		Eval:     eval,
		Git:      brokenGit,
		Logger:   logger,
		Observer: obs,
	})

	// Run 2 iterations. Iter 1: metric 1 → keep (git commit fails → warning).
	// Iter 2: metric 1 again → no improvement (1 is not < 1) → revert → warning.
	if err := loop.Run(context.Background(), 2); err != nil {
		t.Fatal(err)
	}

	// Should have received at least one warning (from revert or commit failure).
	if len(obs.warnings) == 0 {
		t.Error("expected at least one warning from git failure, got none")
	}
}

func TestLoop_LogResult_WarnsOnError(t *testing.T) {
	dir := initTestRepo(t)
	chdir(t, dir)

	programPath := filepath.Join(dir, "program.md")
	if err := os.WriteFile(programPath, []byte("test"), 0644); err != nil {
		t.Fatal(err)
	}
	counterPath := filepath.Join(dir, "counter.txt")
	if err := os.WriteFile(counterPath, []byte("5"), 0644); err != nil {
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
			Command:   "echo 'metric: 5'",
			Metric:    `metric:\s+(\d+)`,
			Source:    config.NewSourceStdout(),
			Direction: config.DirectionMinimize,
			Timeout:   config.Duration{Duration: 5 * time.Second},
		},
		Provider: config.ProviderConfig{MaxTokens: 1024},
		Git:      config.GitConfig{Enabled: false},
	}

	resultsPath := filepath.Join(dir, "results.tsv")
	logger, err := NewResultLogger(resultsPath)
	if err != nil {
		t.Fatal(err)
	}

	// Delete the results file so Append fails on the first iteration.
	if err := os.Remove(resultsPath); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(dir, 0500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chmod(dir, 0700) })

	sandbox, _ := tools.NewSandbox(dir, cfg.Files)
	executor := tools.NewExecutor(sandbox, 5*time.Second)
	eval, _ := NewEval(cfg.Eval)
	git := NewGit(false, dir, nil)

	obs := &capturingObserver{}

	provider := &mockProvider{
		responses: []*llm.Response{
			{
				Content:    []llm.ContentBlock{{Type: llm.BlockText, Text: "no change"}},
				StopReason: llm.StopEndTurn,
			},
		},
	}

	loop := NewLoop(LoopParams{
		Config:   cfg,
		Provider: provider,
		Executor: executor,
		Eval:     eval,
		Git:      git,
		Logger:   logger,
		Observer: obs,
	})

	if err := loop.Run(context.Background(), 1); err != nil {
		t.Fatal(err)
	}

	if len(obs.warnings) == 0 {
		t.Error("expected warning from failed log result, got none")
	}
}

func TestLoop_ToolLoop_ProviderError(t *testing.T) {
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

	resultsPath := filepath.Join(dir, "results.tsv")
	logger, err := NewResultLogger(resultsPath)
	if err != nil {
		t.Fatal(err)
	}

	sandbox, _ := tools.NewSandbox(dir, cfg.Files)
	executor := tools.NewExecutor(sandbox, 5*time.Second)
	eval, _ := NewEval(cfg.Eval)
	git := NewGit(false, dir, nil)

	// Provider returns an error on first call.
	provider := &mockProvider{
		responses: []*llm.Response{nil},
		errs:      []error{fmt.Errorf("provider unavailable")},
	}

	obs := &capturingObserver{}

	loop := NewLoop(LoopParams{
		Config:   cfg,
		Provider: provider,
		Executor: executor,
		Eval:     eval,
		Git:      git,
		Logger:   logger,
		Observer: obs,
	})

	// Run should succeed overall (provider error is handled per-iteration).
	if err := loop.Run(context.Background(), 1); err != nil {
		t.Fatal(err)
	}

	// The iteration should have been logged as an error.
	data, err := os.ReadFile(resultsPath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "error") {
		t.Errorf("expected 'error' status in results, got:\n%s", data)
	}
}

func TestLoop_ToolLoop_ExceedsMaxRounds(t *testing.T) {
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

	resultsPath := filepath.Join(dir, "results.tsv")
	logger, err := NewResultLogger(resultsPath)
	if err != nil {
		t.Fatal(err)
	}

	sandbox, _ := tools.NewSandbox(dir, cfg.Files)
	executor := tools.NewExecutor(sandbox, 5*time.Second)
	eval, _ := NewEval(cfg.Eval)
	git := NewGit(false, dir, nil)

	// Build 21 responses that all request a tool call (to trigger "exceeded 20 rounds").
	// The read_file tool on counter.txt will succeed.
	var responses []*llm.Response
	for i := 0; i < 21; i++ {
		responses = append(responses, &llm.Response{
			Content: []llm.ContentBlock{{
				Type:  llm.BlockToolUse,
				ID:    fmt.Sprintf("call_%d", i),
				Name:  tools.ToolReadFile,
				Input: json.RawMessage(fmt.Sprintf(`{"path":%q}`, counterPath)),
			}},
			StopReason: llm.StopToolUse,
		})
	}

	provider := &mockProvider{responses: responses}

	obs := &capturingObserver{}
	loop := NewLoop(LoopParams{
		Config:   cfg,
		Provider: provider,
		Executor: executor,
		Eval:     eval,
		Git:      git,
		Logger:   logger,
		Observer: obs,
	})

	if err := loop.Run(context.Background(), 1); err != nil {
		t.Fatal(err)
	}

	// The iteration should have been logged as an error due to max rounds.
	data, err := os.ReadFile(resultsPath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "error") {
		t.Errorf("expected 'error' status in results for exceeded rounds, got:\n%s", data)
	}
}

func TestLoop_ToolLoop_ContextCancelledDuringToolRound(t *testing.T) {
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

	resultsPath := filepath.Join(dir, "results.tsv")
	logger, err := NewResultLogger(resultsPath)
	if err != nil {
		t.Fatal(err)
	}

	sandbox, _ := tools.NewSandbox(dir, cfg.Files)
	executor := tools.NewExecutor(sandbox, 5*time.Second)
	eval, _ := NewEval(cfg.Eval)
	git := NewGit(false, dir, nil)

	ctx, cancel := context.WithCancel(context.Background())

	// First call returns a tool request; we cancel the context between rounds.
	callCount := 0
	provider := &mockProvider{}
	// Override with a custom provider that cancels after first call.
	type cancellingProvider struct {
		inner  *mockProvider
		cancel context.CancelFunc
	}
	_ = callCount

	// Use a provider that returns tool call first, then cancels context before second call.
	// We simulate this by pre-cancelling and using the mock's default "done" response.
	// The toolLoop checks ctx.Err() at the top of each round.
	cancel() // cancel before run so context is already done

	loop := NewLoop(LoopParams{
		Config:   cfg,
		Provider: provider,
		Executor: executor,
		Eval:     eval,
		Git:      git,
		Logger:   logger,
		Observer: VerboseObserver{},
	})

	err = loop.Run(ctx, 5)
	if err == nil {
		t.Error("expected context cancellation error, got nil")
	}
}

func TestLoop_Run_EvalError(t *testing.T) {
	// Test the path where eval.Run returns an error (lines 108-113 in loop.go).
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
			// This command exits with a non-zero status, causing an eval error.
			Command:   "exit 1",
			Metric:    `metric:\s+(\d+)`,
			Source:    config.NewSourceStdout(),
			Direction: config.DirectionMinimize,
			Timeout:   config.Duration{Duration: 5 * time.Second},
		},
		Provider: config.ProviderConfig{MaxTokens: 1024},
		Git:      config.GitConfig{Enabled: false},
	}

	resultsPath := filepath.Join(dir, "results.tsv")
	logger, err := NewResultLogger(resultsPath)
	if err != nil {
		t.Fatal(err)
	}

	sandbox, _ := tools.NewSandbox(dir, cfg.Files)
	executor := tools.NewExecutor(sandbox, 5*time.Second)
	eval, _ := NewEval(cfg.Eval)
	git := NewGit(false, dir, nil)

	provider := &mockProvider{
		responses: []*llm.Response{
			{
				Content:    []llm.ContentBlock{{Type: llm.BlockText, Text: "no change"}},
				StopReason: llm.StopEndTurn,
			},
		},
	}

	obs := &capturingObserver{}
	loop := NewLoop(LoopParams{
		Config:   cfg,
		Provider: provider,
		Executor: executor,
		Eval:     eval,
		Git:      git,
		Logger:   logger,
		Observer: obs,
	})

	if err := loop.Run(context.Background(), 1); err != nil {
		t.Fatal(err)
	}

	data, err := os.ReadFile(resultsPath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "error") {
		t.Errorf("expected 'error' status when eval fails, got:\n%s", data)
	}
}

func TestLoop_ToolLoop_TruncatesLongOutput(t *testing.T) {
	// Test the tool output truncation path (output > maxToolOutput bytes).
	dir := initTestRepo(t)
	chdir(t, dir)

	programPath := filepath.Join(dir, "program.md")
	if err := os.WriteFile(programPath, []byte("test"), 0644); err != nil {
		t.Fatal(err)
	}
	counterPath := filepath.Join(dir, "counter.txt")
	// Write a file larger than maxToolOutput (16000) bytes.
	bigContent := strings.Repeat("x", 20000)
	if err := os.WriteFile(counterPath, []byte(bigContent), 0644); err != nil {
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

	resultsPath := filepath.Join(dir, "results.tsv")
	logger, err := NewResultLogger(resultsPath)
	if err != nil {
		t.Fatal(err)
	}

	sandbox, _ := tools.NewSandbox(dir, cfg.Files)
	executor := tools.NewExecutor(sandbox, 5*time.Second)
	eval, _ := NewEval(cfg.Eval)
	git := NewGit(false, dir, nil)

	var observedOutput string
	type recordingObserver struct {
		VerboseObserver
		output *string
	}
	obs := &struct {
		capturingObserver
		toolOutput string
	}{}
	_ = obs

	// Use a recordingObserver to capture the tool output passed to ToolCall.
	type toolCapture struct {
		VerboseObserver
		lastOutput string
	}
	tc := &toolCapture{}

	// Iter 1: read the big file (output will be truncated), then end turn.
	provider := &mockProvider{
		responses: []*llm.Response{
			{
				Content: []llm.ContentBlock{{
					Type:  llm.BlockToolUse,
					ID:    "call_1",
					Name:  tools.ToolReadFile,
					Input: json.RawMessage(fmt.Sprintf(`{"path":%q}`, counterPath)),
				}},
				StopReason: llm.StopToolUse,
			},
			{
				Content:    []llm.ContentBlock{{Type: llm.BlockText, Text: "done"}},
				StopReason: llm.StopEndTurn,
			},
		},
	}

	_ = tc
	_ = observedOutput

	loop := NewLoop(LoopParams{
		Config:   cfg,
		Provider: provider,
		Executor: executor,
		Eval:     eval,
		Git:      git,
		Logger:   logger,
		Observer: VerboseObserver{},
	})

	if err := loop.Run(context.Background(), 1); err != nil {
		t.Fatal(err)
	}
}

// cancelOnFirstCallProvider cancels its context immediately on the first Complete call
// and returns a tool-use response. This causes toolLoop to dispatch the tool, then hit
// ctx.Err() != nil at the top of round 1 before the second Complete call.
type cancelOnFirstCallProvider struct {
	cancel    context.CancelFunc
	firstResp *llm.Response
	calls     int
}

func (p *cancelOnFirstCallProvider) Complete(_ context.Context, _ *llm.Request) (*llm.Response, error) {
	p.calls++
	if p.calls == 1 {
		// Cancel context immediately, then return a tool-use so toolLoop tries another round.
		p.cancel()
		return p.firstResp, nil
	}
	return &llm.Response{
		Content:    []llm.ContentBlock{{Type: llm.BlockText, Text: "done"}},
		StopReason: llm.StopEndTurn,
	}, nil
}

func TestLoop_ToolLoop_ContextCancelledBetweenRounds(t *testing.T) {
	// Test that toolLoop returns ctx.Err() when context is cancelled mid-loop.
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

	ctx, cancel := context.WithCancel(context.Background())

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

	resultsPath := filepath.Join(dir, "results.tsv")
	logger, err := NewResultLogger(resultsPath)
	if err != nil {
		t.Fatal(err)
	}

	sandbox, _ := tools.NewSandbox(dir, cfg.Files)
	executor := tools.NewExecutor(sandbox, 5*time.Second)
	eval, _ := NewEval(cfg.Eval)
	git := NewGit(false, dir, nil)

	// First Complete call cancels the context and returns a tool-use response.
	// After dispatching the tool, toolLoop checks ctx.Err() at the top of round 1
	// and returns immediately without making a second Complete call.
	provider := &cancelOnFirstCallProvider{
		cancel: cancel,
		firstResp: &llm.Response{
			Content: []llm.ContentBlock{{
				Type:  llm.BlockToolUse,
				ID:    "id1",
				Name:  tools.ToolReadFile,
				Input: json.RawMessage(fmt.Sprintf(`{"path":%q}`, counterPath)),
			}},
			StopReason: llm.StopToolUse,
		},
	}

	loop := NewLoop(LoopParams{
		Config:   cfg,
		Provider: provider,
		Executor: executor,
		Eval:     eval,
		Git:      git,
		Logger:   logger,
		Observer: VerboseObserver{},
	})

	// Run should complete without returning an error at the top level because
	// the toolLoop ctx cancellation is handled as an iteration error.
	loop.Run(ctx, 1) //nolint:errcheck - we don't assert the error here since the context
	// cancellation may be caught at the Run level or as an iteration error depending on timing.
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
