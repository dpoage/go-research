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

func TestParseOrientationBrief_Valid(t *testing.T) {
	text := `Some preamble text.

<orientation>
<working>The baseline metric is stable at 10</working>
<failing>Previous attempt to double the value was discarded because it exceeded bounds</failing>
<gap>Need to reduce the metric by at least 20%</gap>
<change>Reduce the counter value by 2 instead of doubling it</change>
<risk>The reduction might overshoot and go negative</risk>
</orientation>

Some trailing text.`

	brief, ok := parseOrientationBrief(text)
	if !ok {
		t.Fatal("expected valid parse")
	}
	if brief.Working != "The baseline metric is stable at 10" {
		t.Errorf("working = %q", brief.Working)
	}
	if brief.Failing != "Previous attempt to double the value was discarded because it exceeded bounds" {
		t.Errorf("failing = %q", brief.Failing)
	}
	if brief.Gap != "Need to reduce the metric by at least 20%" {
		t.Errorf("gap = %q", brief.Gap)
	}
	if brief.Change != "Reduce the counter value by 2 instead of doubling it" {
		t.Errorf("change = %q", brief.Change)
	}
	if brief.Risk != "The reduction might overshoot and go negative" {
		t.Errorf("risk = %q", brief.Risk)
	}
}

func TestParseOrientationBrief_MissingBlock(t *testing.T) {
	_, ok := parseOrientationBrief("No orientation block here.")
	if ok {
		t.Error("expected parse failure for missing block")
	}
}

func TestParseOrientationBrief_MissingChangeField(t *testing.T) {
	text := `<orientation>
<working>things work</working>
<failing>nothing failed</failing>
<gap>big gap</gap>
<risk>some risk</risk>
</orientation>`

	_, ok := parseOrientationBrief(text)
	if ok {
		t.Error("expected parse failure when <change> is missing")
	}
}

func TestParseOrientationBrief_EmptyChangeField(t *testing.T) {
	text := `<orientation>
<working>things work</working>
<failing>nothing failed</failing>
<gap>big gap</gap>
<change></change>
<risk>some risk</risk>
</orientation>`

	_, ok := parseOrientationBrief(text)
	if ok {
		t.Error("expected parse failure when <change> is empty")
	}
}

func TestParseOrientationBrief_PartialFields(t *testing.T) {
	// Only change is required; others can be missing.
	text := `<orientation>
<change>Increase the learning rate by 0.01</change>
</orientation>`

	brief, ok := parseOrientationBrief(text)
	if !ok {
		t.Fatal("expected valid parse with only <change>")
	}
	if brief.Change != "Increase the learning rate by 0.01" {
		t.Errorf("change = %q", brief.Change)
	}
	if brief.Working != "" || brief.Failing != "" || brief.Gap != "" || brief.Risk != "" {
		t.Errorf("expected empty optional fields, got: %+v", brief)
	}
}

func TestBuildOrientationPrompt_FirstIteration(t *testing.T) {
	loop := &Loop{config: &config.Config{
		Files: []string{"model.py"},
		Eval:  config.EvalConfig{Command: "sh eval.sh", Direction: config.DirectionMinimize},
	}}
	prompt := loop.buildOrientationPrompt(1, 10.0, nil)

	if !strings.Contains(prompt, "Orientation — Iteration 1") {
		t.Error("should contain iteration header")
	}
	if !strings.Contains(prompt, "10.000000") {
		t.Error("should contain best metric")
	}
	if !strings.Contains(prompt, "minimize") {
		t.Error("should contain direction")
	}
	if !strings.Contains(prompt, "eval.sh") {
		t.Error("should contain eval command")
	}
	if !strings.Contains(prompt, "model.py") {
		t.Error("should contain files")
	}
	if !strings.Contains(prompt, "No prior iterations") {
		t.Error("should note first iteration when no history")
	}
	if !strings.Contains(prompt, "Read at least one file") {
		t.Error("should require file read")
	}
}

func TestBuildOrientationPrompt_WithHistory(t *testing.T) {
	history := []historyEntry{
		{Iteration: 1, Summary: "doubled the counter", Metric: 20.0, Best: 10.0, Status: StatusKeep},
		{Iteration: 2, Summary: "tripled the counter", Metric: 30.0, Best: 20.0, Status: StatusDiscard},
	}
	loop := &Loop{config: &config.Config{
		Files: []string{"counter.txt"},
		Eval:  config.EvalConfig{Command: "sh eval.sh", Direction: config.DirectionMaximize},
	}}
	prompt := loop.buildOrientationPrompt(3, 20.0, history)

	if !strings.Contains(prompt, "doubled the counter") {
		t.Error("should contain history entry 1 summary")
	}
	if !strings.Contains(prompt, "tripled the counter") {
		t.Error("should contain history entry 2 summary")
	}
	if !strings.Contains(prompt, "[keep]") {
		t.Error("should contain keep status")
	}
	if !strings.Contains(prompt, "[discard]") {
		t.Error("should contain discard status")
	}
	if !strings.Contains(prompt, "best was 10") {
		t.Error("should contain best-at-time for keep entry")
	}
	if !strings.Contains(prompt, "delta=+10") {
		t.Error("should contain delta for keep entry")
	}
	if !strings.Contains(prompt, "MECHANISTICALLY") {
		t.Error("should contain mechanistic constraint")
	}
}

func TestFormatBriefForAction(t *testing.T) {
	brief := OrientationBrief{
		Working: "Baseline is stable",
		Failing: "Doubling didn't work",
		Gap:     "Need 20% improvement",
		Change:  "Try halving the value",
		Risk:    "Might go negative",
	}
	output := formatBriefForAction(brief)

	if !strings.Contains(output, "Orientation Brief") {
		t.Error("should have brief header")
	}
	if !strings.Contains(output, "Planned change:") {
		t.Error("should have planned change label")
	}
	if !strings.Contains(output, "Try halving the value") {
		t.Error("should contain the change")
	}
}

func TestBuildIterHistory(t *testing.T) {
	outcomes := []iterOutcome{
		{Metric: 10.0, BestAtTime: 20.0, Status: StatusKeep, Summary: "change 1"},
		{Metric: 15.0, BestAtTime: 10.0, Status: StatusDiscard, Summary: "change 2"},
		{Status: StatusError}, // error, should be filtered
		{Metric: 8.0, BestAtTime: 10.0, Status: StatusKeep, Summary: "change 3"},
	}

	entries := buildIterHistory(outcomes, 10)
	if len(entries) != 3 {
		t.Fatalf("expected 3 entries (error filtered), got %d", len(entries))
	}
	if entries[0].Iteration != 1 || entries[0].Summary != "change 1" {
		t.Errorf("entry 0: %+v", entries[0])
	}
	if entries[0].Best != 20.0 {
		t.Errorf("entry 0 best: got %v, want 20.0", entries[0].Best)
	}
	if entries[2].Iteration != 4 || entries[2].Summary != "change 3" {
		t.Errorf("entry 2: %+v", entries[2])
	}
}

func TestBuildIterHistory_MaxHistory(t *testing.T) {
	outcomes := make([]iterOutcome, 20)
	for i := range outcomes {
		outcomes[i] = iterOutcome{Metric: float64(i), Status: StatusKeep, Summary: fmt.Sprintf("change %d", i+1)}
	}

	entries := buildIterHistory(outcomes, 5)
	if len(entries) != 5 {
		t.Fatalf("expected 5 entries, got %d", len(entries))
	}
	// Should contain the last 5 (iterations 16-20).
	if entries[0].Iteration != 16 {
		t.Errorf("first entry should be iteration 16, got %d", entries[0].Iteration)
	}
}

func TestBuildIterHistory_Empty(t *testing.T) {
	entries := buildIterHistory(nil, 10)
	if len(entries) != 0 {
		t.Errorf("expected 0 entries, got %d", len(entries))
	}
}

func TestRunOrientation_ValidBrief(t *testing.T) {
	dir := initTestRepo(t)
	chdir(t, dir)

	programPath := filepath.Join(dir, "program.md")
	os.WriteFile(programPath, []byte("test"), 0644)
	counterPath := filepath.Join(dir, "counter.txt")
	os.WriteFile(counterPath, []byte("10"), 0644)

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
			Direction: config.DirectionMinimize,
		},
		Provider: config.ProviderConfig{MaxTokens: 1024, MaxRounds: 5},
	}

	sandbox, _ := tools.NewSandbox(dir, cfg.Files)
	executor := tools.NewExecutor(sandbox, 5*time.Second)

	briefText := `<orientation>
<working>Counter starts at 10</working>
<failing>No failures yet</failing>
<gap>Need to reduce the counter</gap>
<change>Set the counter to 5</change>
<risk>Might not be enough reduction</risk>
</orientation>`

	// Provider returns a valid brief directly.
	provider := &directProvider{
		response: &llm.Response{
			Content:    []llm.ContentBlock{{Type: llm.BlockText, Text: briefText}},
			StopReason: llm.StopEndTurn,
		},
	}

	loop := &Loop{
		config:   cfg,
		provider: provider,
		executor: executor,
		observer: VerboseObserver{},
	}

	brief, stats, err := loop.runOrientation(context.Background(), "system", 1, 10.0, nil)
	if err != nil {
		t.Fatal(err)
	}
	if brief.Change != "Set the counter to 5" {
		t.Errorf("change = %q", brief.Change)
	}
	if stats.Rounds != 1 {
		t.Errorf("expected 1 round, got %d", stats.Rounds)
	}
}

func TestRunOrientation_WithToolCalls(t *testing.T) {
	dir := initTestRepo(t)
	chdir(t, dir)

	programPath := filepath.Join(dir, "program.md")
	os.WriteFile(programPath, []byte("test"), 0644)
	counterPath := filepath.Join(dir, "counter.txt")
	os.WriteFile(counterPath, []byte("42"), 0644)

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
			Direction: config.DirectionMinimize,
		},
		Provider: config.ProviderConfig{MaxTokens: 1024, MaxRounds: 5},
	}

	sandbox, _ := tools.NewSandbox(dir, cfg.Files)
	executor := tools.NewExecutor(sandbox, 5*time.Second)

	briefText := `<orientation>
<working>Counter is 42</working>
<failing>N/A</failing>
<gap>Need to reduce the counter</gap>
<change>Set counter to 21</change>
<risk>Halving might not be the best strategy</risk>
</orientation>`

	// Provider first returns a read_file tool call, then the brief.
	provider := &sequenceProvider{
		responses: []*llm.Response{
			{
				Content: []llm.ContentBlock{{
					Type:  llm.BlockToolUse,
					ID:    "orient_read_1",
					Name:  "read_file",
					Input: json.RawMessage(fmt.Sprintf(`{"path":%q}`, counterPath)),
				}},
				StopReason: llm.StopToolUse,
			},
			{
				Content:    []llm.ContentBlock{{Type: llm.BlockText, Text: briefText}},
				StopReason: llm.StopEndTurn,
			},
		},
	}

	loop := &Loop{
		config:   cfg,
		provider: provider,
		executor: executor,
		observer: VerboseObserver{},
	}

	brief, stats, err := loop.runOrientation(context.Background(), "system", 1, 42.0, nil)
	if err != nil {
		t.Fatal(err)
	}
	if brief.Change != "Set counter to 21" {
		t.Errorf("change = %q", brief.Change)
	}
	if stats.Rounds != 2 {
		t.Errorf("expected 2 rounds, got %d", stats.Rounds)
	}
}

func TestRunOrientation_FallbackOnInvalidBrief(t *testing.T) {
	cfg := &config.Config{
		Eval: config.EvalConfig{
			Direction: config.DirectionMinimize,
		},
		Provider: config.ProviderConfig{MaxTokens: 1024, MaxRounds: 5},
	}

	// Provider returns text without a valid orientation block.
	provider := &directProvider{
		response: &llm.Response{
			Content:    []llm.ContentBlock{{Type: llm.BlockText, Text: "I don't know what to do."}},
			StopReason: llm.StopEndTurn,
		},
	}

	loop := &Loop{
		config:   cfg,
		provider: provider,
		observer: VerboseObserver{},
	}

	brief, _, err := loop.runOrientation(context.Background(), "system", 1, 10.0, nil)
	if err != nil {
		t.Fatal(err)
	}
	if brief.Change == "" {
		t.Error("fallback brief should have a non-empty change field")
	}
	if brief.Working == "" || brief.Risk == "" {
		t.Error("fallback brief should populate all fields")
	}
	if !strings.Contains(brief.Risk, "duplicate") {
		t.Error("fallback risk should warn about duplicate attempts")
	}
}

func TestRunOrientation_ContextCancelled(t *testing.T) {
	cfg := &config.Config{
		Eval: config.EvalConfig{
			Direction: config.DirectionMinimize,
		},
		Provider: config.ProviderConfig{MaxTokens: 1024, MaxRounds: 5},
	}

	provider := &directProvider{
		response: &llm.Response{
			Content:    []llm.ContentBlock{{Type: llm.BlockText, Text: defaultOrientationText}},
			StopReason: llm.StopEndTurn,
		},
	}

	loop := &Loop{
		config:   cfg,
		provider: provider,
		observer: VerboseObserver{},
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, _, err := loop.runOrientation(ctx, "system", 1, 10.0, nil)
	if err == nil {
		t.Error("expected context cancellation error")
	}
}

func TestRunOrientation_ProviderError(t *testing.T) {
	cfg := &config.Config{
		Eval: config.EvalConfig{
			Direction: config.DirectionMinimize,
		},
		Provider: config.ProviderConfig{MaxTokens: 1024, MaxRounds: 5},
	}

	provider := &directProvider{err: fmt.Errorf("provider down")}

	loop := &Loop{
		config:   cfg,
		provider: provider,
		observer: VerboseObserver{},
	}

	_, _, err := loop.runOrientation(context.Background(), "system", 1, 10.0, nil)
	if err == nil {
		t.Error("expected provider error")
	}
	if !strings.Contains(err.Error(), "orientation") {
		t.Errorf("error should mention orientation: %v", err)
	}
}

func TestOrientationToolDefs(t *testing.T) {
	defs := orientationToolDefs()
	if len(defs) != 2 {
		t.Fatalf("expected 2 tool defs, got %d", len(defs))
	}

	names := map[string]bool{}
	for _, d := range defs {
		names[d.Name] = true
	}
	if !names[tools.ToolReadFile] || !names[tools.ToolGrep] {
		t.Errorf("expected read_file and grep, got %v", names)
	}
}

func TestBuildActionPrompt_IncludesBrief(t *testing.T) {
	cfg := &config.Config{
		Files: []string{"counter.txt"},
		Eval: config.EvalConfig{
			Command:   "sh eval.sh",
			Direction: config.DirectionMinimize,
		},
		Provider: config.ProviderConfig{MaxRounds: 5},
	}

	loop := &Loop{config: cfg}

	brief := OrientationBrief{
		Working: "Counter at 10",
		Failing: "Doubling failed",
		Gap:     "Need 20% reduction",
		Change:  "Halve the counter",
		Risk:    "Might go too low",
	}

	prompt := loop.buildActionPrompt(3, 10.0, brief)

	if !strings.Contains(prompt, "Action — Iteration 3") {
		t.Error("should contain action header")
	}
	if !strings.Contains(prompt, "Orientation Brief") {
		t.Error("should contain orientation brief")
	}
	if !strings.Contains(prompt, "Halve the counter") {
		t.Error("should contain the planned change")
	}
	if !strings.Contains(prompt, "Planned change:") {
		t.Error("should label the change field")
	}
	if !strings.Contains(prompt, "### Constraints") {
		t.Error("should have constraints section")
	}
	if !strings.Contains(prompt, "MUST execute") {
		t.Error("should have binding statement at top")
	}
	// Constraints should appear before Protocol in the prompt.
	constraintsIdx := strings.Index(prompt, "### Constraints")
	protocolIdx := strings.Index(prompt, "### Protocol")
	if constraintsIdx > protocolIdx {
		t.Error("constraints should appear before protocol")
	}
}

func TestLoop_EndToEnd_WithOrientation(t *testing.T) {
	// Full integration test: orientation → action → eval → keep/discard.
	dir := initTestRepo(t)
	chdir(t, dir)

	counterPath := filepath.Join(dir, "counter.txt")
	os.WriteFile(counterPath, []byte("10"), 0644)

	evalScript := filepath.Join(dir, "eval.sh")
	os.WriteFile(evalScript, []byte("#!/bin/sh\necho \"metric: $(cat counter.txt)\"\n"), 0755)

	programPath := filepath.Join(dir, "program.md")
	os.WriteFile(programPath, []byte("You are a test agent."), 0644)

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

	sandbox, _ := tools.NewSandbox(dir, cfg.Files)
	executor := tools.NewExecutor(sandbox, 10*time.Second)
	eval, _ := NewEval(cfg.Eval)
	git := NewGit(true, dir, cfg.Files)
	git.CreateBranch(cfg.Git.BranchPrefix)

	resultsPath := filepath.Join(dir, "results.tsv")
	logger, _ := NewResultLogger(resultsPath)

	// Use orientationAwareProvider that returns a proper brief for orientation
	// and scripted responses for action.
	provider := &orientationAwareProvider{
		orientBrief: `<orientation>
<working>Counter starts at 10</working>
<failing>No prior attempts</failing>
<gap>Need to reduce the counter</gap>
<change>Set counter to 5</change>
<risk>Might not be enough</risk>
</orientation>`,
		actionResponses: []*llm.Response{
			// Iter 1 action: write "5".
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
				Content:    []llm.ContentBlock{{Type: llm.BlockText, Text: "Set counter to 5"}},
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

	data, err := os.ReadFile(resultsPath)
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	if len(lines) != 2 { // header + 1 entry
		t.Fatalf("expected 2 lines, got %d:\n%s", len(lines), data)
	}
	if !strings.Contains(lines[1], "keep") {
		t.Errorf("iter 1 should be keep (5 < 10): %s", lines[1])
	}

	counterData, _ := os.ReadFile(counterPath)
	if string(counterData) != "5" {
		t.Errorf("counter should be 5, got %q", counterData)
	}
}

func TestBuildIterHistory_ErrorsFiltered(t *testing.T) {
	outcomes := []iterOutcome{
		{Metric: 10.0, Status: StatusKeep},
		{Metric: 0, Status: StatusError},
		{Metric: 8.0, Status: StatusKeep},
	}
	entries := buildIterHistory(outcomes, 10)
	if len(entries) != 2 {
		t.Fatalf("expected 2 entries (errors filtered), got %d", len(entries))
	}
}

// directProvider always returns the same response. Used for unit-testing
// runOrientation in isolation.
type directProvider struct {
	response *llm.Response
	err      error
}

func (p *directProvider) Complete(_ context.Context, _ *llm.Request) (*llm.Response, error) {
	if p.err != nil {
		return nil, p.err
	}
	return p.response, nil
}

// sequenceProvider returns responses in order, like mockProvider but simpler.
type sequenceProvider struct {
	responses []*llm.Response
	idx       int
}

func (p *sequenceProvider) Complete(_ context.Context, _ *llm.Request) (*llm.Response, error) {
	if p.idx >= len(p.responses) {
		return &llm.Response{
			Content:    []llm.ContentBlock{{Type: llm.BlockText, Text: "done"}},
			StopReason: llm.StopEndTurn,
		}, nil
	}
	resp := p.responses[p.idx]
	p.idx++
	return resp, nil
}

// orientationAwareProvider returns a fixed brief for orientation calls and
// scripted responses for action calls. Used for integration tests.
type orientationAwareProvider struct {
	orientBrief     string
	actionResponses []*llm.Response
	actionIdx       int
}

func (p *orientationAwareProvider) Complete(_ context.Context, req *llm.Request) (*llm.Response, error) {
	if isOrientationCall(req) {
		return &llm.Response{
			Content:    []llm.ContentBlock{{Type: llm.BlockText, Text: p.orientBrief}},
			StopReason: llm.StopEndTurn,
		}, nil
	}
	if p.actionIdx >= len(p.actionResponses) {
		return &llm.Response{
			Content:    []llm.ContentBlock{{Type: llm.BlockText, Text: "done"}},
			StopReason: llm.StopEndTurn,
		}, nil
	}
	resp := p.actionResponses[p.actionIdx]
	p.actionIdx++
	return resp, nil
}
