package experiment

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"strings"

	"github.com/dpoage/go-research/config"
	"github.com/dpoage/go-research/llm"
	"github.com/dpoage/go-research/tools"
)

// maxToolOutput is the maximum bytes of tool output sent back to the LLM
// to avoid blowing the context window.
const maxToolOutput = 8000

// maxConsecutiveErrors is the number of consecutive iteration errors before
// the experiment loop aborts (circuit breaker).
const maxConsecutiveErrors = 3

// iterOutcome summarizes what happened in the previous iteration,
// so the model has context for its next attempt.
type iterOutcome struct {
	Metric float64
	Status Status
}

// LoopParams holds the dependencies for an experiment Loop.
type LoopParams struct {
	Config   *config.Config
	Provider llm.Provider
	Executor *tools.Executor
	Eval     *Eval
	Git      *Git
	Logger   *ResultLogger
	Observer Observer
}

// Loop runs the autonomous experiment cycle.
type Loop struct {
	config   *config.Config
	provider llm.Provider
	executor *tools.Executor
	eval     *Eval
	git      *Git
	logger   *ResultLogger
	observer Observer
}

// NewLoop creates a Loop from the given parameters.
func NewLoop(p LoopParams) *Loop {
	return &Loop{
		config:   p.Config,
		provider: p.Provider,
		executor: p.Executor,
		eval:     p.Eval,
		git:      p.Git,
		logger:   p.Logger,
		observer: p.Observer,
	}
}

// ToolDefs returns the tool definitions to register with the LLM.
// Descriptions include dynamic context (writable files, round budget) so the
// model has actionable information without having to parse the user prompt.
func ToolDefs(cfg *config.Config) []llm.ToolDef {
	fileList := strings.Join(cfg.Files, ", ")
	return []llm.ToolDef{
		{
			Name:        tools.ToolReadFile,
			Description: "Read file contents. All paths are readable. Prefer targeted reads over large files.",
			InputSchema: json.RawMessage(`{"type":"object","properties":{"path":{"type":"string","description":"Path to the file to read"},"offset":{"type":"integer","description":"Start reading from this line number (1-based, optional)"},"limit":{"type":"integer","description":"Maximum number of lines to return (optional)"}},"required":["path"]}`),
		},
		{
			Name:        tools.ToolWriteFile,
			Description: fmt.Sprintf("Write content to a file. Writable files: [%s]. Other paths will fail.", fileList),
			InputSchema: json.RawMessage(`{"type":"object","properties":{"path":{"type":"string","description":"Path to the file to write"},"content":{"type":"string","description":"Complete file content to write"}},"required":["path","content"]}`),
		},
		{
			Name:        tools.ToolRunCommand,
			Description: "Run a shell command (sh -c). Timeout: 30s. Use for builds/tests; prefer read_file for reading files.",
			InputSchema: json.RawMessage(`{"type":"object","properties":{"command":{"type":"string","description":"Shell command to execute"}},"required":["command"]}`),
		},
		{
			Name:        tools.ToolDone,
			Description: "Signal completion and trigger eval. You MUST call this when finished. The harness reverts if the metric doesn't improve, so always call done — don't overthink it.",
			InputSchema: json.RawMessage(`{"type":"object","properties":{"summary":{"type":"string","description":"Brief description of the change you made"}},"required":["summary"]}`),
		},
	}
}

// Run executes the experiment loop until the context is cancelled.
// maxIter <= 0 means unlimited iterations.
func (l *Loop) Run(ctx context.Context, maxIter int) error {
	program, err := os.ReadFile(l.config.Program)
	if err != nil {
		return fmt.Errorf("read program: %w", err)
	}

	system := string(program)
	bestMetric := math.NaN()
	toolDefs := ToolDefs(l.config)
	consecutiveErrors := 0
	var lastResult *iterOutcome

	for iter := 1; maxIter <= 0 || iter <= maxIter; iter++ {
		if ctx.Err() != nil {
			return ctx.Err()
		}

		l.observer.IterationStart(iter, maxIter)

		prompt := l.buildPrompt(iter, bestMetric, lastResult)
		messages := []llm.Message{llm.NewTextMessage(llm.RoleUser, prompt)}

		// Pre-seed file contents as a synthetic "turn 0" so the agent
		// doesn't burn real tool rounds reading files it will always need.
		// This sits after the user prompt, keeping the system prompt stable
		// for caching while giving the agent immediate orientation.
		messages = l.prefillFileContents(messages)

		// Run tool-use turns until the model stops requesting tools.
		var stats ToolLoopStats
		messages, stats, err = l.toolLoop(ctx, system, messages, toolDefs)
		l.observer.ToolLoopComplete(iter, stats)
		if err != nil {
			l.observer.IterationError(iter, err)
			l.logResult(iter, EvalResult{Error: err}, StatusError, err.Error(), stats)
			lastResult = &iterOutcome{Status: StatusError}
			if abortErr := l.checkCircuitBreaker(&consecutiveErrors, err); abortErr != nil {
				return abortErr
			}
			continue
		}

		// Evaluate.
		l.observer.EvalStarted()
		result := l.eval.Run(ctx)
		if result.Error != nil {
			l.observer.IterationError(iter, result.Error)
			l.revert(iter)
			l.logResult(iter, result, StatusError, result.Error.Error(), stats)
			lastResult = &iterOutcome{Status: StatusError}
			if abortErr := l.checkCircuitBreaker(&consecutiveErrors, result.Error); abortErr != nil {
				return abortErr
			}
			continue
		}

		consecutiveErrors = 0

		l.observer.EvalResult(iter, result.Metric, result.Elapsed)

		// Decide keep/discard.
		if l.isBetter(result.Metric, bestMetric) {
			l.observer.Improvement(iter, result.Metric, bestMetric)
			bestMetric = result.Metric

			// Log before commit so the results file is included in the snapshot.
			l.logResult(iter, result, StatusKeep, "", stats)
			lastResult = &iterOutcome{Metric: result.Metric, Status: StatusKeep}

			if err := l.git.Commit(fmt.Sprintf("iter %d: metric=%.6f", iter, result.Metric), l.logger.Path()); err != nil {
				l.observer.Warning(fmt.Sprintf("git commit failed: %v", err))
			}
		} else {
			l.observer.NoImprovement(iter, result.Metric, bestMetric)
			l.revert(iter)
			// Log after revert so the entry isn't reverted with the code changes.
			l.logResult(iter, result, StatusDiscard, fmt.Sprintf("best=%.6f", bestMetric), stats)
			lastResult = &iterOutcome{Metric: result.Metric, Status: StatusDiscard}
		}
	}

	l.observer.Complete(bestMetric)
	return nil
}

func (l *Loop) buildPrompt(iter int, bestMetric float64, last *iterOutcome) string {
	var b strings.Builder
	fmt.Fprintf(&b, "## Iteration %d\n\n", iter)

	// Context block — compact key-value layout.
	fmt.Fprintf(&b, "**Files:** %v  |  **Eval:** `%s`  |  **Direction:** %v\n",
		l.config.Files, l.config.Eval.Command, l.config.Eval.Direction)

	if !math.IsNaN(bestMetric) {
		fmt.Fprintf(&b, "**Best metric:** %.6f", bestMetric)
	}

	if last != nil {
		switch last.Status {
		case StatusKeep:
			fmt.Fprintf(&b, "  |  **Last:** kept (%.6f) — try a different improvement", last.Metric)
		case StatusDiscard:
			fmt.Fprintf(&b, "  |  **Last:** discarded (%.6f) — try something else", last.Metric)
		case StatusError:
			b.WriteString("  |  **Last:** error — try a different approach")
		}
	}
	b.WriteString("\n")

	// Protocol — terse instructions the model can scan quickly.
	fmt.Fprintf(&b, `
### Protocol
1. Read only what you need
2. Make ONE focused change
3. Call done with a summary

Budget: %d tool rounds. The harness reverts if the metric doesn't improve, so always call done — don't overthink it.
`, l.config.Provider.MaxRounds)

	return b.String()
}

// maxPrefillBytes is the total budget for file contents injected in the
// synthetic turn-0 exchange. Files are included in config order until the
// budget is exhausted; any remaining files are listed by name and size.
const maxPrefillBytes = 16000

// prefillFileContents appends a synthetic assistant+user exchange to messages
// that contains the current contents of the editable files. This lets the
// agent skip the "read everything" phase that otherwise burns real tool rounds.
func (l *Loop) prefillFileContents(messages []llm.Message) []llm.Message {
	type fileEntry struct {
		path    string
		content string
		size    int
	}

	var included []fileEntry
	var skipped []fileEntry
	budget := maxPrefillBytes

	for _, path := range l.config.Files {
		data, err := os.ReadFile(path)
		if err != nil {
			// File might not exist yet; just skip.
			skipped = append(skipped, fileEntry{path: path, size: -1})
			continue
		}
		content := string(data)
		if len(content) <= budget {
			included = append(included, fileEntry{path: path, content: content, size: len(data)})
			budget -= len(content)
		} else {
			skipped = append(skipped, fileEntry{path: path, size: len(data)})
		}
	}

	if len(included) == 0 && len(skipped) == 0 {
		return messages
	}

	// Synthetic assistant message: looks like the agent decided to read files.
	messages = append(messages, llm.Message{
		Role: llm.RoleAssistant,
		Content: []llm.ContentBlock{{
			Type: llm.BlockText,
			Text: "I'll review the current file contents before making changes.",
		}},
	})

	// Build the synthetic user response with file contents.
	var blocks []llm.ContentBlock
	for _, f := range included {
		blocks = append(blocks, llm.ContentBlock{
			Type: llm.BlockText,
			Text: fmt.Sprintf("=== %s ===\n%s", f.path, f.content),
		})
	}
	if len(skipped) > 0 {
		var note strings.Builder
		note.WriteString("Files not shown (use read_file to view):")
		for _, f := range skipped {
			if f.size >= 0 {
				fmt.Fprintf(&note, "\n  %s (%d bytes)", f.path, f.size)
			} else {
				fmt.Fprintf(&note, "\n  %s (not found)", f.path)
			}
		}
		blocks = append(blocks, llm.ContentBlock{
			Type: llm.BlockText,
			Text: note.String(),
		})
	}

	messages = append(messages, llm.Message{Role: llm.RoleUser, Content: blocks})
	return messages
}

// toolLoop runs LLM completion in a loop, dispatching tool calls until the model
// calls the done tool, returns end_turn, or exhausts the round budget.
func (l *Loop) toolLoop(ctx context.Context, system string, messages []llm.Message, toolDefs []llm.ToolDef) ([]llm.Message, ToolLoopStats, error) {
	maxRounds := l.config.Provider.MaxRounds
	var stats ToolLoopStats

	for round := range maxRounds {
		if ctx.Err() != nil {
			return messages, stats, ctx.Err()
		}

		remaining := maxRounds - round

		// On the final round, strip tools to force a text-only response.
		reqTools := toolDefs
		if remaining == 1 {
			reqTools = nil
		}

		// Compress old tool results to keep context size manageable.
		compressed := compressHistory(messages, 3)

		resp, err := l.provider.Complete(ctx, &llm.Request{
			System:    system,
			Messages:  compressed,
			Tools:     reqTools,
			MaxTokens: l.config.Provider.MaxTokens,
		})
		if err != nil {
			return messages, stats, fmt.Errorf("LLM completion (round %d): %w", round+1, err)
		}

		stats.Rounds++
		stats.InputTokens += resp.Usage.InputTokens
		stats.OutputTokens += resp.Usage.OutputTokens

		messages = append(messages, llm.Message{Role: llm.RoleAssistant, Content: resp.Content})

		if text := resp.TextContent(); text != "" {
			l.observer.AgentText(text)
		}

		toolCalls := resp.ToolUseBlocks()
		if len(toolCalls) == 0 || resp.StopReason == llm.StopEndTurn {
			return messages, stats, nil
		}

		// Dispatch tool calls, collecting results into a single user message.
		done := false
		var resultBlocks []llm.ContentBlock
		for _, tc := range toolCalls {
			if tc.Name == tools.ToolDone {
				done = true
				l.observer.ToolCall(tc.Name, "iteration complete")
				resultBlocks = append(resultBlocks, llm.ContentBlock{
					Type: llm.BlockToolResult, ID: tc.ID, Content: "Evaluation will now run.",
				})
				continue
			}

			result := l.executor.Dispatch(ctx, tc.Name, tc.Input)

			output := result.Output
			if len(output) > maxToolOutput {
				output = output[:maxToolOutput] + "\n... (truncated)"
			}

			l.observer.ToolCall(tc.Name, output)
			resultBlocks = append(resultBlocks, llm.ContentBlock{
				Type: llm.BlockToolResult, ID: tc.ID, Content: output, IsError: result.IsError,
			})
		}

		// Inject a budget reminder after the first round as a text block
		// in the same user message. Keeping the system prompt stable
		// enables provider-side prompt caching.
		if round > 0 {
			resultBlocks = append(resultBlocks, llm.ContentBlock{
				Type: llm.BlockText, Text: budgetMessage(remaining - 1),
			})
		}

		messages = append(messages, llm.Message{Role: llm.RoleUser, Content: resultBlocks})

		if done {
			return messages, stats, nil
		}
	}

	return messages, stats, fmt.Errorf("tool loop exceeded %d rounds", maxRounds)
}

// checkCircuitBreaker increments the consecutive error counter and returns
// a non-nil error if the threshold is reached.
func (l *Loop) checkCircuitBreaker(consecutiveErrors *int, err error) error {
	*consecutiveErrors++
	if *consecutiveErrors >= maxConsecutiveErrors {
		return fmt.Errorf("aborting after %d consecutive errors: %w", *consecutiveErrors, err)
	}
	return nil
}

// budgetMessage returns an escalating urgency reminder based on remaining rounds.
func budgetMessage(remaining int) string {
	switch {
	case remaining <= 1:
		return "[FINAL ROUND — tools disabled. Summarize what you changed and stop.]"
	case remaining <= 2:
		return fmt.Sprintf("[URGENT: %d rounds left. Call done NOW.]", remaining)
	default:
		return fmt.Sprintf("[%d rounds remaining.]", remaining)
	}
}

// compressHistory replaces the content of old tool_result blocks with a short
// summary to keep context size manageable. It preserves the most recent
// keepRecentRounds worth of assistant+tool-result exchanges intact.
func compressHistory(messages []llm.Message, keepRecentRounds int) []llm.Message {
	// Count rounds by counting assistant messages.
	var assistantCount int
	for _, m := range messages {
		if m.Role == llm.RoleAssistant {
			assistantCount++
		}
	}

	// Nothing to compress if we haven't exceeded the keep window.
	if assistantCount <= keepRecentRounds {
		return messages
	}

	cutoff := assistantCount - keepRecentRounds
	out := make([]llm.Message, len(messages))
	var seen int
	for i, m := range messages {
		if m.Role == llm.RoleAssistant {
			seen++
		}
		// Compress tool_result blocks in messages that precede the cutoff.
		if seen <= cutoff && m.Role == llm.RoleUser {
			compressed := make([]llm.ContentBlock, len(m.Content))
			for j, b := range m.Content {
				if b.Type == llm.BlockToolResult && len(b.Content) > 200 {
					compressed[j] = llm.ContentBlock{
						Type:    llm.BlockToolResult,
						ID:      b.ID,
						Content: fmt.Sprintf("[%d bytes, truncated from history]", len(b.Content)),
						IsError: b.IsError,
					}
				} else {
					compressed[j] = b
				}
			}
			out[i] = llm.Message{Role: m.Role, Content: compressed}
		} else {
			out[i] = m
		}
	}
	return out
}

func (l *Loop) isBetter(current, best float64) bool {
	if math.IsNaN(best) {
		return true
	}
	if l.config.Eval.Direction == config.DirectionMinimize {
		return current < best
	}
	return current > best
}

func (l *Loop) revert(iter int) {
	if err := l.git.Revert(); err != nil {
		l.observer.Warning(fmt.Sprintf("revert failed (iter %d): %v", iter, err))
	}
}

func (l *Loop) logResult(iter int, result EvalResult, status Status, note string, stats ToolLoopStats) {
	if err := l.logger.Append(ResultEntry{
		Iteration:    iter,
		Metric:       result.Metric,
		Status:       status,
		Elapsed:      result.Elapsed,
		Note:         note,
		Rounds:       stats.Rounds,
		InputTokens:  stats.InputTokens,
		OutputTokens: stats.OutputTokens,
	}); err != nil {
		l.observer.Warning(fmt.Sprintf("log result failed: %v", err))
	}
}
