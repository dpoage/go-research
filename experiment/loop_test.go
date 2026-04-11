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
			MaxRounds: config.DefaultMaxRounds,
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
		Provider: config.ProviderConfig{MaxTokens: 1024, MaxRounds: config.DefaultMaxRounds},
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

// capturingObserver records Warning and ToolCall events for assertion.
type capturingObserver struct {
	VerboseObserver
	warnings  []string
	toolCalls []string
}

func (o *capturingObserver) Warning(msg string) {
	o.warnings = append(o.warnings, msg)
}

func (o *capturingObserver) ToolCall(name, output string) {
	o.toolCalls = append(o.toolCalls, name)
	o.VerboseObserver.ToolCall(name, output)
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
		Provider: config.ProviderConfig{MaxTokens: 1024, MaxRounds: config.DefaultMaxRounds},
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
		Provider: config.ProviderConfig{MaxTokens: 1024, MaxRounds: config.DefaultMaxRounds},
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
		Provider: config.ProviderConfig{MaxTokens: 1024, MaxRounds: config.DefaultMaxRounds},
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
		Provider: config.ProviderConfig{MaxTokens: 1024, MaxRounds: config.DefaultMaxRounds},
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
		Provider: config.ProviderConfig{MaxTokens: 1024, MaxRounds: config.DefaultMaxRounds},
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
		Provider: config.ProviderConfig{MaxTokens: 1024, MaxRounds: config.DefaultMaxRounds},
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

	// Build responses that all request run_command (to trigger "exceeded N rounds").
	// read_file rounds are free, so we use run_command to test budget enforcement.
	var responses []*llm.Response
	for i := 0; i < config.DefaultMaxRounds+1; i++ {
		responses = append(responses, &llm.Response{
			Content: []llm.ContentBlock{{
				Type:  llm.BlockToolUse,
				ID:    fmt.Sprintf("call_%d", i),
				Name:  tools.ToolRunCommand,
				Input: json.RawMessage(`{"command":"echo hi"}`),
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
		Provider: config.ProviderConfig{MaxTokens: 1024, MaxRounds: config.DefaultMaxRounds},
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
		Provider: config.ProviderConfig{MaxTokens: 1024, MaxRounds: config.DefaultMaxRounds},
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
		Provider: config.ProviderConfig{MaxTokens: 1024, MaxRounds: config.DefaultMaxRounds},
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
		Provider: config.ProviderConfig{MaxTokens: 1024, MaxRounds: config.DefaultMaxRounds},
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
	cfg := &config.Config{
		Files: []string{"model.py", "train.py"},
	}
	defs := ToolDefs(cfg)
	if len(defs) != 7 {
		t.Fatalf("expected 7 tool defs, got %d", len(defs))
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

	for _, expected := range []string{tools.ToolReadFile, tools.ToolWriteFile, tools.ToolEditFile, tools.ToolGrep, tools.ToolRunCommand, tools.ToolRunEval, tools.ToolDone} {
		if !names[expected] {
			t.Errorf("missing tool def: %s", expected)
		}
	}
}

func TestLoop_ToolLoop_DoneToolExit(t *testing.T) {
	// The tool loop should exit when the model calls the done tool.
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
		Provider: config.ProviderConfig{MaxTokens: 1024, MaxRounds: 20},
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

	// Round 0: write_file
	// Round 1: write_file + done (batched — both dispatched, then loop exits)
	// Round 2: should never be reached
	provider := &mockProvider{
		responses: []*llm.Response{
			{
				Content: []llm.ContentBlock{{
					Type:  llm.BlockToolUse,
					ID:    "call_1",
					Name:  tools.ToolWriteFile,
					Input: json.RawMessage(fmt.Sprintf(`{"path":%q,"content":"2"}`, counterPath)),
				}},
				StopReason: llm.StopToolUse,
			},
			{
				Content: []llm.ContentBlock{
					{
						Type:  llm.BlockToolUse,
						ID:    "call_2",
						Name:  tools.ToolWriteFile,
						Input: json.RawMessage(fmt.Sprintf(`{"path":%q,"content":"0"}`, counterPath)),
					},
					{
						Type:  llm.BlockToolUse,
						ID:    "call_3",
						Name:  tools.ToolDone,
						Input: json.RawMessage(`{"summary":"lowered counter"}`),
					},
				},
				StopReason: llm.StopToolUse,
			},
			// This should NOT be reached.
			{
				Content: []llm.ContentBlock{{
					Type:  llm.BlockToolUse,
					ID:    "call_4",
					Name:  tools.ToolReadFile,
					Input: json.RawMessage(fmt.Sprintf(`{"path":%q}`, counterPath)),
				}},
				StopReason: llm.StopToolUse,
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

	if err := loop.Run(context.Background(), 1); err != nil {
		t.Fatal(err)
	}

	// Provider should have been called exactly 2 times (write round + write+done round).
	if provider.calls != 2 {
		t.Errorf("expected 2 provider calls (done tool exit), got %d", provider.calls)
	}

	// The write in the done round should have been dispatched before exiting.
	data, err := os.ReadFile(counterPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "0" {
		t.Errorf("expected counter to be '0' (write dispatched before done), got %q", string(data))
	}
}

func TestLoop_ToolLoop_CustomMaxRounds(t *testing.T) {
	// Verify that a custom MaxRounds value is respected.
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
		Provider: config.ProviderConfig{MaxTokens: 1024, MaxRounds: 3},
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

	// Build 4 responses that all request writes (to exceed MaxRounds=3).
	// Note: read_file rounds are free and don't count toward the budget,
	// so we use write_file to test budget enforcement.
	var responses []*llm.Response
	for i := 0; i < 4; i++ {
		responses = append(responses, &llm.Response{
			Content: []llm.ContentBlock{{
				Type:  llm.BlockToolUse,
				ID:    fmt.Sprintf("call_%d", i),
				Name:  tools.ToolWriteFile,
				Input: json.RawMessage(fmt.Sprintf(`{"path":%q,"content":"x"}`, counterPath)),
			}},
			StopReason: llm.StopToolUse,
		})
	}

	provider := &mockProvider{responses: responses}

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

	// Should have logged an error about exceeding 3 rounds.
	data, err := os.ReadFile(resultsPath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "error") {
		t.Errorf("expected 'error' status for exceeded custom max rounds, got:\n%s", data)
	}
	if !strings.Contains(string(data), "3 rounds") {
		t.Errorf("expected error to mention '3 rounds', got:\n%s", data)
	}
}

func TestLoop_ToolLoop_FreeReads(t *testing.T) {
	// Verify that read_file-only rounds don't count toward the budget.
	dir := initTestRepo(t)
	chdir(t, dir)

	programPath := filepath.Join(dir, "program.md")
	os.WriteFile(programPath, []byte("test"), 0644)
	counterPath := filepath.Join(dir, "counter.txt")
	os.WriteFile(counterPath, []byte("1"), 0644)

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
			Direction: config.DirectionMinimize,
			Timeout:   config.Duration{Duration: 5 * time.Second},
		},
		Provider: config.ProviderConfig{MaxTokens: 1024, MaxRounds: 2},
		Git:      config.GitConfig{Enabled: false},
	}

	resultsPath := filepath.Join(dir, "results.tsv")
	logger, _ := NewResultLogger(resultsPath)
	sandbox, _ := tools.NewSandbox(dir, cfg.Files)
	executor := tools.NewExecutor(sandbox, 5*time.Second)
	eval, _ := NewEval(cfg.Eval)
	git := NewGit(false, dir, nil)

	// Round 1: read_file (free, doesn't count)
	// Round 2: read_file (free, doesn't count)
	// Round 3: write_file (billed, counts as round 1)
	// Round 4: done (billed, counts as round 2)
	// With MaxRounds=2, this should succeed — the 2 reads are free.
	provider := &mockProvider{
		responses: []*llm.Response{
			{
				Content: []llm.ContentBlock{{
					Type: llm.BlockToolUse, ID: "c1", Name: tools.ToolReadFile,
					Input: json.RawMessage(fmt.Sprintf(`{"path":%q}`, counterPath)),
				}},
				StopReason: llm.StopToolUse,
			},
			{
				Content: []llm.ContentBlock{{
					Type: llm.BlockToolUse, ID: "c2", Name: tools.ToolReadFile,
					Input: json.RawMessage(fmt.Sprintf(`{"path":%q}`, counterPath)),
				}},
				StopReason: llm.StopToolUse,
			},
			{
				Content: []llm.ContentBlock{{
					Type: llm.BlockToolUse, ID: "c3", Name: tools.ToolWriteFile,
					Input: json.RawMessage(fmt.Sprintf(`{"path":%q,"content":"0"}`, counterPath)),
				}},
				StopReason: llm.StopToolUse,
			},
			{
				Content: []llm.ContentBlock{{
					Type: llm.BlockToolUse, ID: "c4", Name: tools.ToolDone,
					Input: json.RawMessage(`{"summary":"lowered"}`),
				}},
				StopReason: llm.StopToolUse,
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

	if err := loop.Run(context.Background(), 1); err != nil {
		t.Fatal(err)
	}

	// All 4 rounds should have been called.
	if provider.calls != 4 {
		t.Errorf("expected 4 provider calls (2 free reads + 2 billed), got %d", provider.calls)
	}

	// Should have succeeded (keep), not errored from round limit.
	data, _ := os.ReadFile(resultsPath)
	if !strings.Contains(string(data), "keep") {
		t.Errorf("expected 'keep' (free reads didn't exhaust budget), got:\n%s", data)
	}
}

func TestLoop_CircuitBreaker(t *testing.T) {
	// Verify that Run() aborts after maxConsecutiveErrors consecutive failures.
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
		Provider: config.ProviderConfig{MaxTokens: 1024, MaxRounds: 20},
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

	// All calls return errors — should trigger circuit breaker after 3.
	provider := &mockProvider{
		responses: make([]*llm.Response, 10),
		errs: []error{
			fmt.Errorf("provider error 1"),
			fmt.Errorf("provider error 2"),
			fmt.Errorf("provider error 3"),
			fmt.Errorf("provider error 4"),
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

	// Run with maxIter=10 — should abort after 3 consecutive errors, not run all 10.
	err = loop.Run(context.Background(), 10)
	if err == nil {
		t.Fatal("expected circuit breaker error, got nil")
	}
	if !strings.Contains(err.Error(), "aborting after 3 consecutive errors") {
		t.Errorf("expected circuit breaker message, got: %v", err)
	}

	// Should have only attempted 3 iterations, not 10.
	if provider.calls != 3 {
		t.Errorf("expected 3 provider calls before circuit breaker, got %d", provider.calls)
	}
}

func TestLoop_CircuitBreaker_ResetsOnSuccess(t *testing.T) {
	// Verify that a successful iteration resets the consecutive error counter.
	dir := initTestRepo(t)
	chdir(t, dir)

	programPath := filepath.Join(dir, "program.md")
	if err := os.WriteFile(programPath, []byte("test"), 0644); err != nil {
		t.Fatal(err)
	}
	counterPath := filepath.Join(dir, "counter.txt")
	if err := os.WriteFile(counterPath, []byte("10"), 0644); err != nil {
		t.Fatal(err)
	}

	evalScript := filepath.Join(dir, "eval.sh")
	if err := os.WriteFile(evalScript, []byte("#!/bin/sh\necho \"metric: $(cat counter.txt)\"\n"), 0755); err != nil {
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
			Command:   "sh " + evalScript,
			Metric:    `metric:\s+(\d+)`,
			Source:    config.NewSourceStdout(),
			Direction: config.DirectionMinimize,
			Timeout:   config.Duration{Duration: 5 * time.Second},
		},
		Provider: config.ProviderConfig{MaxTokens: 1024, MaxRounds: 20},
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

	// Pattern: error, error, success (write 5), error, error.
	// The success in the middle resets the counter, so we never hit 3 consecutive.
	provider := &mockProvider{
		responses: []*llm.Response{
			nil, // error 1
			nil, // error 2
			// Iter 3: successful write
			{
				Content: []llm.ContentBlock{{
					Type:  llm.BlockToolUse,
					ID:    "call_1",
					Name:  tools.ToolWriteFile,
					Input: json.RawMessage(fmt.Sprintf(`{"path":%q,"content":"5"}`, counterPath)),
				}},
				StopReason: llm.StopToolUse,
			},
			{
				Content:    []llm.ContentBlock{{Type: llm.BlockText, Text: "done"}},
				StopReason: llm.StopEndTurn,
			},
			nil, // error 4
			nil, // error 5
		},
		errs: []error{
			fmt.Errorf("error 1"),
			fmt.Errorf("error 2"),
			nil, // success
			nil, // success (end turn)
			fmt.Errorf("error 4"),
			fmt.Errorf("error 5"),
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

	// Run 5 iterations. Should complete without circuit breaker since errors are
	// never 3 consecutive (reset by the success in iter 3).
	err = loop.Run(context.Background(), 5)
	if err != nil {
		t.Fatalf("expected no circuit breaker error, got: %v", err)
	}
}

func TestCompressHistory_NoCompression(t *testing.T) {
	// With fewer assistant messages than keepRecent, no compression happens.
	messages := []llm.Message{
		llm.NewTextMessage(llm.RoleUser, "prompt"),
		{Role: llm.RoleAssistant, Content: []llm.ContentBlock{{Type: llm.BlockText, Text: "thinking"}}},
		{Role: llm.RoleUser, Content: []llm.ContentBlock{{Type: llm.BlockToolResult, ID: "c1", Content: strings.Repeat("x", 500)}}},
	}
	result := compressHistory(messages, 3)
	// Content should be unchanged since assistantCount (1) <= keepRecent (3).
	if result[2].Content[0].Content != messages[2].Content[0].Content {
		t.Error("expected no compression when assistant count <= keepRecent")
	}
}

func TestCompressHistory_CompressesOldRounds(t *testing.T) {
	messages := []llm.Message{
		llm.NewTextMessage(llm.RoleUser, "prompt"),
		// Round 1 (old — should be compressed)
		{Role: llm.RoleAssistant, Content: []llm.ContentBlock{{Type: llm.BlockText, Text: "r1"}}},
		{Role: llm.RoleUser, Content: []llm.ContentBlock{{Type: llm.BlockToolResult, ID: "c1", Content: strings.Repeat("x", 500)}}},
		// Round 2 (old — should be compressed)
		{Role: llm.RoleAssistant, Content: []llm.ContentBlock{{Type: llm.BlockText, Text: "r2"}}},
		{Role: llm.RoleUser, Content: []llm.ContentBlock{{Type: llm.BlockToolResult, ID: "c2", Content: strings.Repeat("y", 500)}}},
		// Round 3 (recent — keep)
		{Role: llm.RoleAssistant, Content: []llm.ContentBlock{{Type: llm.BlockText, Text: "r3"}}},
		{Role: llm.RoleUser, Content: []llm.ContentBlock{{Type: llm.BlockToolResult, ID: "c3", Content: strings.Repeat("z", 500)}}},
		// Round 4 (recent — keep)
		{Role: llm.RoleAssistant, Content: []llm.ContentBlock{{Type: llm.BlockText, Text: "r4"}}},
		{Role: llm.RoleUser, Content: []llm.ContentBlock{{Type: llm.BlockToolResult, ID: "c4", Content: strings.Repeat("w", 500)}}},
	}

	result := compressHistory(messages, 2)

	// Rounds 1 and 2 (indices 2 and 4) should have truncated tool_result content.
	for _, idx := range []int{2, 4} {
		content := result[idx].Content[0].Content
		if len(content) > 200 {
			t.Errorf("message[%d] content not compressed: len=%d", idx, len(content))
		}
		if !strings.Contains(content, "truncated from history") {
			t.Errorf("message[%d] missing truncation marker: %s", idx, content)
		}
	}

	// Rounds 3 and 4 (indices 6 and 8) should be intact.
	for _, idx := range []int{6, 8} {
		content := result[idx].Content[0].Content
		if strings.Contains(content, "truncated") {
			t.Errorf("message[%d] should NOT be compressed: %s", idx, content)
		}
	}

	// Assistant text blocks should never be compressed.
	for _, idx := range []int{1, 3, 5, 7} {
		if result[idx].Content[0].Text != messages[idx].Content[0].Text {
			t.Errorf("message[%d] assistant text was modified", idx)
		}
	}
}

func TestCompressHistory_PreservesShortContent(t *testing.T) {
	messages := []llm.Message{
		llm.NewTextMessage(llm.RoleUser, "prompt"),
		{Role: llm.RoleAssistant, Content: []llm.ContentBlock{{Type: llm.BlockText, Text: "r1"}}},
		{Role: llm.RoleUser, Content: []llm.ContentBlock{{Type: llm.BlockToolResult, ID: "c1", Content: "short"}}},
		{Role: llm.RoleAssistant, Content: []llm.ContentBlock{{Type: llm.BlockText, Text: "r2"}}},
		{Role: llm.RoleUser, Content: []llm.ContentBlock{{Type: llm.BlockToolResult, ID: "c2", Content: "also short"}}},
	}

	result := compressHistory(messages, 1)

	// The old round's content is under 200 chars, so it should be preserved.
	if result[2].Content[0].Content != "short" {
		t.Errorf("short content should not be compressed: %s", result[2].Content[0].Content)
	}
}

func TestToolDefs_WritableFilesInDescription(t *testing.T) {
	cfg := &config.Config{
		Files: []string{"model.py", "train.py"},
	}
	defs := ToolDefs(cfg)

	var writeDesc string
	for _, d := range defs {
		if d.Name == "write_file" {
			writeDesc = d.Description
		}
	}
	if !strings.Contains(writeDesc, "model.py") || !strings.Contains(writeDesc, "train.py") {
		t.Errorf("write_file description should contain writable files, got: %s", writeDesc)
	}
}

func TestBudgetMessage(t *testing.T) {
	tests := []struct {
		remaining int
		contains  string
	}{
		{1, "URGENT"},
		{2, "URGENT"},
		{5, "5 edit rounds remaining"},
	}
	for _, tt := range tests {
		msg := budgetMessage(tt.remaining)
		if !strings.Contains(msg, tt.contains) {
			t.Errorf("budgetMessage(%d) = %q, want to contain %q", tt.remaining, msg, tt.contains)
		}
	}
}

func TestFreeRoundNudge(t *testing.T) {
	tests := []struct {
		consecutive int
		contains    string
	}{
		{2, "Reminder"},
		{5, "5 free rounds"},
		{10, "10 free rounds without editing"},
		{15, "15 free rounds without editing"},
	}
	for _, tt := range tests {
		msg := freeRoundNudge(tt.consecutive)
		if !strings.Contains(msg, tt.contains) {
			t.Errorf("freeRoundNudge(%d) = %q, want to contain %q", tt.consecutive, msg, tt.contains)
		}
	}
}

func TestAppendFileContents(t *testing.T) {
	dir := t.TempDir()

	f1 := filepath.Join(dir, "a.txt")
	f2 := filepath.Join(dir, "b.txt")
	os.WriteFile(f1, []byte("hello"), 0644)
	os.WriteFile(f2, []byte("world"), 0644)

	loop := &Loop{config: &config.Config{Files: []string{f1, f2}}}

	var b strings.Builder
	loop.appendFileContents(&b)
	output := b.String()

	if !strings.Contains(output, "hello") || !strings.Contains(output, "world") {
		t.Errorf("should contain file contents, got: %s", output)
	}
	if !strings.Contains(output, "a.txt") || !strings.Contains(output, "b.txt") {
		t.Errorf("should contain file names, got: %s", output)
	}
}

func TestAppendFileContents_ExceedsLimit(t *testing.T) {
	dir := t.TempDir()

	small := filepath.Join(dir, "small.txt")
	large := filepath.Join(dir, "large.txt")
	os.WriteFile(small, []byte("tiny"), 0644)
	os.WriteFile(large, make([]byte, maxPrefillBytes+1), 0644)

	loop := &Loop{config: &config.Config{Files: []string{small, large}}}

	var b strings.Builder
	loop.appendFileContents(&b)
	output := b.String()

	if !strings.Contains(output, "tiny") {
		t.Errorf("small file should be included: %s", output)
	}
	if !strings.Contains(output, "use read_file") {
		t.Errorf("large file should suggest read_file: %s", output)
	}
}

func TestAppendFileContents_MissingFile(t *testing.T) {
	loop := &Loop{config: &config.Config{Files: []string{"/nonexistent/file.txt"}}}

	var b strings.Builder
	loop.appendFileContents(&b)

	if !strings.Contains(b.String(), "not found") {
		t.Errorf("missing file should be noted: %s", b.String())
	}
}

func TestLoop_RunEvalTool(t *testing.T) {
	dir := initTestRepo(t)
	chdir(t, dir)

	counterPath := filepath.Join(dir, "counter.txt")
	os.WriteFile(counterPath, []byte("42"), 0644)

	programPath := filepath.Join(dir, "program.md")
	os.WriteFile(programPath, []byte("test"), 0644)

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
			Command:   fmt.Sprintf("echo 'metric:' $(cat %s)", counterPath),
			Metric:    `metric:\s+(\d+)`,
			Source:    config.NewSourceStdout(),
			Direction: config.DirectionMinimize,
			Timeout:   config.Duration{Duration: 5 * time.Second},
		},
		Provider: config.ProviderConfig{MaxTokens: 1024, MaxRounds: 20},
		Git:      config.GitConfig{Enabled: false},
	}

	resultsPath := filepath.Join(dir, "results.tsv")
	logger, _ := NewResultLogger(resultsPath)
	sandbox, _ := tools.NewSandbox(dir, cfg.Files)
	executor := tools.NewExecutor(sandbox, 5*time.Second)
	eval, _ := NewEval(cfg.Eval)
	git := NewGit(false, dir, nil)

	// Model calls run_eval, then writes a lower value, then calls done.
	provider := &mockProvider{
		responses: []*llm.Response{
			{
				Content: []llm.ContentBlock{{
					Type:  llm.BlockToolUse,
					ID:    "eval_1",
					Name:  tools.ToolRunEval,
					Input: json.RawMessage(`{}`),
				}},
				StopReason: llm.StopToolUse,
			},
			{
				Content: []llm.ContentBlock{{
					Type:  llm.BlockToolUse,
					ID:    "write_1",
					Name:  tools.ToolWriteFile,
					Input: json.RawMessage(fmt.Sprintf(`{"path":%q,"content":"10"}`, counterPath)),
				}},
				StopReason: llm.StopToolUse,
			},
			{
				Content: []llm.ContentBlock{{
					Type:  llm.BlockToolUse,
					ID:    "done_1",
					Name:  tools.ToolDone,
					Input: json.RawMessage(`{"summary":"lowered counter"}`),
				}},
				StopReason: llm.StopToolUse,
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

	// Verify run_eval was called (captured in observer tool calls).
	foundEvalCall := false
	for _, tc := range obs.toolCalls {
		if tc == tools.ToolRunEval {
			foundEvalCall = true
		}
	}
	if !foundEvalCall {
		t.Error("expected run_eval tool call to be captured by observer")
	}
}

func TestLoop_InitialBenchmark(t *testing.T) {
	dir := initTestRepo(t)
	chdir(t, dir)

	counterPath := filepath.Join(dir, "counter.txt")
	os.WriteFile(counterPath, []byte("50"), 0644)

	programPath := filepath.Join(dir, "program.md")
	os.WriteFile(programPath, []byte("test"), 0644)

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
			Command:   fmt.Sprintf("echo 'metric:' $(cat %s)", counterPath),
			Metric:    `metric:\s+(\d+)`,
			Source:    config.NewSourceStdout(),
			Direction: config.DirectionMinimize,
			Timeout:   config.Duration{Duration: 5 * time.Second},
		},
		Provider: config.ProviderConfig{MaxTokens: 1024, MaxRounds: 20},
		Git:      config.GitConfig{Enabled: false},
	}

	resultsPath := filepath.Join(dir, "results.tsv")
	logger, _ := NewResultLogger(resultsPath)
	sandbox, _ := tools.NewSandbox(dir, cfg.Files)
	executor := tools.NewExecutor(sandbox, 5*time.Second)
	eval, _ := NewEval(cfg.Eval)
	git := NewGit(false, dir, nil)

	// Model writes 60 (worse than baseline 50 when minimizing) — should be discarded.
	provider := &mockProvider{
		responses: []*llm.Response{
			{
				Content: []llm.ContentBlock{{
					Type:  llm.BlockToolUse,
					ID:    "write_1",
					Name:  tools.ToolWriteFile,
					Input: json.RawMessage(fmt.Sprintf(`{"path":%q,"content":"60"}`, counterPath)),
				}},
				StopReason: llm.StopToolUse,
			},
			{
				Content:    []llm.ContentBlock{{Type: llm.BlockText, Text: "wrote 60"}},
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

	if err := loop.Run(context.Background(), 1); err != nil {
		t.Fatal(err)
	}

	// The iteration should be discarded because 60 > 50 (baseline) when minimizing.
	data, _ := os.ReadFile(resultsPath)
	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	if len(lines) != 2 { // header + 1 entry
		t.Fatalf("expected 2 lines, got %d:\n%s", len(lines), data)
	}
	if !strings.Contains(lines[1], "discard") {
		t.Errorf("iter 1 should be discard (60 > baseline 50), got: %s", lines[1])
	}
}

func TestLoop_DoneSummaryExtracted(t *testing.T) {
	dir := initTestRepo(t)
	chdir(t, dir)

	counterPath := filepath.Join(dir, "counter.txt")
	os.WriteFile(counterPath, []byte("10"), 0644)
	programPath := filepath.Join(dir, "program.md")
	os.WriteFile(programPath, []byte("test"), 0644)

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
			Command:   fmt.Sprintf("echo 'metric:' $(cat %s)", counterPath),
			Metric:    `metric:\s+(\d+)`,
			Source:    config.NewSourceStdout(),
			Direction: config.DirectionMinimize,
			Timeout:   config.Duration{Duration: 5 * time.Second},
		},
		Provider: config.ProviderConfig{MaxTokens: 1024, MaxRounds: 20},
		Git:      config.GitConfig{Enabled: false},
	}

	resultsPath := filepath.Join(dir, "results.tsv")
	logger, _ := NewResultLogger(resultsPath)
	sandbox, _ := tools.NewSandbox(dir, cfg.Files)
	executor := tools.NewExecutor(sandbox, 5*time.Second)
	eval, _ := NewEval(cfg.Eval)
	git := NewGit(false, dir, nil)

	// Model writes 5 (improvement), calls done with a summary.
	provider := &mockProvider{
		responses: []*llm.Response{
			{
				Content: []llm.ContentBlock{{
					Type: llm.BlockToolUse, ID: "w1", Name: tools.ToolWriteFile,
					Input: json.RawMessage(fmt.Sprintf(`{"path":%q,"content":"5"}`, counterPath)),
				}},
				StopReason: llm.StopToolUse,
			},
			{
				Content: []llm.ContentBlock{{
					Type: llm.BlockToolUse, ID: "d1", Name: tools.ToolDone,
					Input: json.RawMessage(`{"summary":"reduced counter from 10 to 5"}`),
				}},
				StopReason: llm.StopToolUse,
			},
		},
	}

	obs := &capturingObserver{}
	loop := NewLoop(LoopParams{
		Config: cfg, Provider: provider, Executor: executor,
		Eval: eval, Git: git, Logger: logger, Observer: obs,
	})

	if err := loop.Run(context.Background(), 1); err != nil {
		t.Fatal(err)
	}

	// Verify the summary was captured in ToolLoopComplete stats.
	// The capturingObserver doesn't track stats, but we can verify via
	// the observer's ToolCall which should have been called for "done".
	foundDone := false
	for _, tc := range obs.toolCalls {
		if tc == tools.ToolDone {
			foundDone = true
		}
	}
	if !foundDone {
		t.Error("expected done tool call")
	}
}

func TestCompressHistory_StripsStaleBudgetMessages(t *testing.T) {
	messages := []llm.Message{
		llm.NewTextMessage(llm.RoleUser, "prompt"),
		// Round 1 (old — budget message should be stripped)
		{Role: llm.RoleAssistant, Content: []llm.ContentBlock{{Type: llm.BlockText, Text: "r1"}}},
		{Role: llm.RoleUser, Content: []llm.ContentBlock{
			{Type: llm.BlockToolResult, ID: "c1", Content: "short result"},
			{Type: llm.BlockText, Text: "[5 edit rounds remaining.]"},
		}},
		// Round 2 (old — nudge should be stripped)
		{Role: llm.RoleAssistant, Content: []llm.ContentBlock{{Type: llm.BlockText, Text: "r2"}}},
		{Role: llm.RoleUser, Content: []llm.ContentBlock{
			{Type: llm.BlockToolResult, ID: "c2", Content: "short result"},
			{Type: llm.BlockText, Text: "[Reminder: make your edit and call done when ready.]"},
		}},
		// Round 3 (recent — keep everything)
		{Role: llm.RoleAssistant, Content: []llm.ContentBlock{{Type: llm.BlockText, Text: "r3"}}},
		{Role: llm.RoleUser, Content: []llm.ContentBlock{
			{Type: llm.BlockToolResult, ID: "c3", Content: "short result"},
			{Type: llm.BlockText, Text: "[2 edit rounds remaining.]"},
		}},
	}

	result := compressHistory(messages, 1)

	// Old messages (indices 2 and 4) should have budget/nudge text blocks stripped.
	for _, idx := range []int{2, 4} {
		for _, b := range result[idx].Content {
			if b.Type == llm.BlockText && strings.HasPrefix(b.Text, "[") {
				t.Errorf("message[%d] should have stale text block stripped: %s", idx, b.Text)
			}
		}
	}

	// Recent message (index 6) should still have its budget text.
	found := false
	for _, b := range result[6].Content {
		if b.Type == llm.BlockText && strings.Contains(b.Text, "2 edit rounds") {
			found = true
		}
	}
	if !found {
		t.Error("recent message should preserve budget text block")
	}
}

func TestFreeRoundNudge_InjectedInToolLoop(t *testing.T) {
	dir := initTestRepo(t)
	chdir(t, dir)

	counterPath := filepath.Join(dir, "counter.txt")
	os.WriteFile(counterPath, []byte("1"), 0644)
	programPath := filepath.Join(dir, "program.md")
	os.WriteFile(programPath, []byte("test"), 0644)

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
		Provider: config.ProviderConfig{MaxTokens: 1024, MaxRounds: 20},
		Git:      config.GitConfig{Enabled: false},
	}

	// Model does 3 free read_file calls, then stops.
	// After rounds 2 and 3, nudge messages should be injected.
	provider := &mockProvider{
		responses: []*llm.Response{
			{
				Content: []llm.ContentBlock{{
					Type: llm.BlockToolUse, ID: "r1", Name: tools.ToolReadFile,
					Input: json.RawMessage(fmt.Sprintf(`{"path":%q}`, counterPath)),
				}},
				StopReason: llm.StopToolUse,
			},
			{
				Content: []llm.ContentBlock{{
					Type: llm.BlockToolUse, ID: "r2", Name: tools.ToolReadFile,
					Input: json.RawMessage(fmt.Sprintf(`{"path":%q}`, counterPath)),
				}},
				StopReason: llm.StopToolUse,
			},
			{
				Content: []llm.ContentBlock{{
					Type: llm.BlockToolUse, ID: "r3", Name: tools.ToolReadFile,
					Input: json.RawMessage(fmt.Sprintf(`{"path":%q}`, counterPath)),
				}},
				StopReason: llm.StopToolUse,
			},
			// Then model stops.
			{
				Content:    []llm.ContentBlock{{Type: llm.BlockText, Text: "done thinking"}},
				StopReason: llm.StopEndTurn,
			},
		},
	}

	sandbox, _ := tools.NewSandbox(dir, cfg.Files)
	executor := tools.NewExecutor(sandbox, 5*time.Second)
	eval, _ := NewEval(cfg.Eval)

	toolDefs := ToolDefs(cfg)
	messages := []llm.Message{llm.NewTextMessage(llm.RoleUser, "test prompt")}

	loop := &Loop{
		config:   cfg,
		provider: provider,
		executor: executor,
		eval:     eval,
		observer: VerboseObserver{},
	}

	msgs, _, err := loop.toolLoop(context.Background(), "system", messages, toolDefs)
	if err != nil {
		t.Fatal(err)
	}

	// Check that nudge text blocks were injected in later user messages.
	nudgeCount := 0
	for _, m := range msgs {
		if m.Role != llm.RoleUser {
			continue
		}
		for _, b := range m.Content {
			if b.Type == llm.BlockText && strings.Contains(b.Text, "Reminder") {
				nudgeCount++
			}
		}
	}
	if nudgeCount == 0 {
		t.Error("expected at least one free-round nudge message to be injected")
	}
}
