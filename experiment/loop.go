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

// maxFreeRounds caps the number of unbilled (read-only) rounds to prevent
// an infinite loop if the model keeps issuing free tool calls.
const maxFreeRounds = 20

// freeTools are tools whose rounds do not count toward the budget.
// Note: run_eval is handled before this map is checked (see toolLoop),
// so it does not need an entry here.
var freeTools = map[string]bool{
	tools.ToolReadFile: true,
	tools.ToolGrep:     true,
}

// iterOutcome summarizes what happened in the previous iteration,
// so the model has context for its next attempt.
type iterOutcome struct {
	Metric  float64
	Status  Status
	Summary string
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
			Description: "Read file contents. Editable files are inlined in your prompt — use this only for other files or files too large to inline. Free (does not consume a round).",
			InputSchema: json.RawMessage(`{"type":"object","properties":{"path":{"type":"string","description":"Path to the file to read"},"offset":{"type":"integer","description":"Start reading from this line number (1-based, optional)"},"limit":{"type":"integer","description":"Maximum number of lines to return (optional)"}},"required":["path"]}`),
		},
		{
			Name:        tools.ToolWriteFile,
			Description: fmt.Sprintf("Write content to a file. Writable files: [%s]. Other paths will fail.", fileList),
			InputSchema: json.RawMessage(`{"type":"object","properties":{"path":{"type":"string","description":"Path to the file to write"},"content":{"type":"string","description":"Complete file content to write"}},"required":["path","content"]}`),
		},
		{
			Name:        tools.ToolEditFile,
			Description: fmt.Sprintf("Replace a unique string in a file with new content. Writable files: [%s]. The old string must appear exactly once.", fileList),
			InputSchema: json.RawMessage(`{"type":"object","properties":{"path":{"type":"string","description":"Path to the file to edit"},"old":{"type":"string","description":"Exact text to find (must be unique in the file)"},"new":{"type":"string","description":"Replacement text"}},"required":["path","old","new"]}`),
		},
		{
			Name:        tools.ToolGrep,
			Description: "Search file contents with grep -rn. Free (does not consume a round).",
			InputSchema: json.RawMessage(`{"type":"object","properties":{"pattern":{"type":"string","description":"Search pattern (basic regex)"},"path":{"type":"string","description":"File or directory to search (default: .)"},"include":{"type":"string","description":"Glob pattern to filter files, e.g. *.py"}},"required":["pattern"]}`),
		},
		{
			Name:        tools.ToolRunCommand,
			Description: "Run a shell command (sh -c). Timeout: 30s. Use for builds/tests; prefer read_file for reading files.",
			InputSchema: json.RawMessage(`{"type":"object","properties":{"command":{"type":"string","description":"Shell command to execute"}},"required":["command"]}`),
		},
		{
			Name:        tools.ToolRunEval,
			Description: "Run the eval command and return the current metric. Free (does not consume a round). The harness runs eval automatically after done — you rarely need this.",
			InputSchema: json.RawMessage(`{"type":"object","properties":{}}`),
		},
		{
			Name:        tools.ToolDone,
			Description: "Signal that you are finished editing. You MUST call this after making your change. Provide a summary of what you changed — this is preserved across iterations to avoid repeating failed approaches.",
			InputSchema: json.RawMessage(`{"type":"object","properties":{"summary":{"type":"string","description":"Brief description of what you changed and why"}},"required":["summary"]}`),
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

	// Run initial benchmark so the model has a baseline.
	l.observer.EvalStarted()
	baseline := l.eval.Run(ctx)
	if baseline.Error != nil {
		l.observer.Warning(fmt.Sprintf("initial benchmark failed: %v", baseline.Error))
	} else {
		bestMetric = baseline.Metric
		l.observer.EvalResult(0, baseline.Metric, baseline.Elapsed)
	}

	for iter := 1; maxIter <= 0 || iter <= maxIter; iter++ {
		if ctx.Err() != nil {
			return ctx.Err()
		}

		l.observer.IterationStart(iter, maxIter)

		prompt := l.buildPrompt(iter, bestMetric, lastResult)
		messages := []llm.Message{llm.NewTextMessage(llm.RoleUser, prompt)}

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
			lastResult = &iterOutcome{Metric: result.Metric, Status: StatusKeep, Summary: stats.Summary}

			if err := l.git.Commit(fmt.Sprintf("iter %d: metric=%.6f", iter, result.Metric), l.logger.Path()); err != nil {
				l.observer.Warning(fmt.Sprintf("git commit failed: %v", err))
			}
		} else {
			l.observer.NoImprovement(iter, result.Metric, bestMetric)
			l.revert(iter)
			// Log after revert so the entry isn't reverted with the code changes.
			l.logResult(iter, result, StatusDiscard, fmt.Sprintf("best=%.6f", bestMetric), stats)
			lastResult = &iterOutcome{Metric: result.Metric, Status: StatusDiscard, Summary: stats.Summary}
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
		if last == nil {
			b.WriteString(" (baseline)")
		}
	}

	if last != nil {
		switch last.Status {
		case StatusKeep:
			fmt.Fprintf(&b, "  |  **Last:** kept (%.6f) — try a different improvement", last.Metric)
		case StatusDiscard:
			fmt.Fprintf(&b, "  |  **Last:** discarded (%.6f)", last.Metric)
			if last.Summary != "" {
				fmt.Fprintf(&b, " — tried: %s. Try a DIFFERENT approach", last.Summary)
			} else {
				b.WriteString(" — try something else")
			}
		case StatusError:
			b.WriteString("  |  **Last:** error — try a different approach")
		}
	}
	b.WriteString("\n")

	// Protocol — explicit workflow with guardrails.
	fmt.Fprintf(&b, `
### Protocol
1. Review the file contents below (already inlined — do NOT re-read them)
2. Decide on ONE focused change to improve the metric
3. Edit the file(s) using edit_file or write_file
4. Call done with a summary of what you changed

Do NOT:
- Re-read files that are already inlined below
- Run run_eval or read files after editing — the harness evaluates automatically
- Make multiple unrelated changes in one iteration

Budget: %d edit rounds (read_file and grep are free but do not stall on them).
The harness reverts if the metric doesn't improve, so always call done.
`, l.config.Provider.MaxRounds)

	// Inline current file contents so the agent can act immediately.
	l.appendFileContents(&b)

	return b.String()
}

// maxPrefillBytes is the total budget for file contents injected in the
// synthetic turn-0 exchange. Files are included in config order until the
// budget is exhausted; any remaining files are listed by name and size.
const maxPrefillBytes = 16000

// keepRecentRounds is the number of most-recent tool-use rounds whose
// tool_result content is preserved verbatim by compressHistory. Older
// rounds have their tool_result content replaced with a short summary.
const keepRecentRounds = 3

// appendFileContents writes the current contents of the editable files into
// the prompt so the agent can act immediately without burning tool rounds on
// initial reads. Files are included in config order until the byte budget is
// exhausted; oversized files are listed by name and size.
func (l *Loop) appendFileContents(b *strings.Builder) {
	budget := maxPrefillBytes
	b.WriteString("\n### Current file contents (already loaded — do not re-read these)\n")

	var anyWritten bool
	for _, path := range l.config.Files {
		data, err := os.ReadFile(path)
		if err != nil {
			fmt.Fprintf(b, "\n**%s** — not found\n", path)
			anyWritten = true
			continue
		}
		content := string(data)
		if len(content) <= budget {
			fmt.Fprintf(b, "\n**%s**\n```\n%s\n```\n", path, content)
			budget -= len(content)
			anyWritten = true
		} else {
			fmt.Fprintf(b, "\n**%s** — %d bytes, use read_file to view\n", path, len(data))
			anyWritten = true
		}
	}

	if !anyWritten {
		b.WriteString("(no files configured)\n")
	}
}

// toolLoop runs LLM completion in a loop, dispatching tool calls until the model
// calls the done tool, returns end_turn, or exhausts the round budget.
// Rounds where the only tool calls are read_file are "free" and do not count
// toward the budget, so the model can orient itself without pressure.
func (l *Loop) toolLoop(ctx context.Context, system string, messages []llm.Message, toolDefs []llm.ToolDef) ([]llm.Message, ToolLoopStats, error) {
	maxRounds := l.config.Provider.MaxRounds
	var stats ToolLoopStats
	billed := 0
	consecutiveFree := 0

	for {
		if ctx.Err() != nil {
			return messages, stats, ctx.Err()
		}

		remaining := maxRounds - billed

		// On the final billed round, strip tools to force a text-only response.
		reqTools := toolDefs
		if remaining <= 1 {
			reqTools = nil
		}

		compressed := compressHistory(messages, keepRecentRounds)

		resp, err := l.provider.Complete(ctx, &llm.Request{
			System:    system,
			Messages:  compressed,
			Tools:     reqTools,
			MaxTokens: l.config.Provider.MaxTokens,
		})
		if err != nil {
			return messages, stats, fmt.Errorf("LLM completion (round %d): %w", stats.Rounds+1, err)
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
		readOnly := true
		var resultBlocks []llm.ContentBlock
		for _, tc := range toolCalls {
			if tc.Name == tools.ToolDone {
				done = true
				readOnly = false
				var di struct {
					Summary string `json:"summary"`
				}
				_ = json.Unmarshal(tc.Input, &di)
				stats.Summary = di.Summary
				l.observer.ToolCall(tc.Name, "iteration complete")
				resultBlocks = append(resultBlocks, llm.ContentBlock{
					Type: llm.BlockToolResult, ID: tc.ID, Content: "Evaluation will now run.",
				})
				continue
			}

			if tc.Name == tools.ToolRunEval {
				evalResult := l.eval.Run(ctx)
				var output string
				if evalResult.Error != nil {
					output = fmt.Sprintf("eval error: %v", evalResult.Error)
				} else {
					output = fmt.Sprintf("current metric: %.6f", evalResult.Metric)
				}
				l.observer.ToolCall(tc.Name, output)
				resultBlocks = append(resultBlocks, llm.ContentBlock{
					Type: llm.BlockToolResult, ID: tc.ID, Content: output, IsError: evalResult.Error != nil,
				})
				continue
			}

			if !freeTools[tc.Name] {
				readOnly = false
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

		if !readOnly {
			billed++
			consecutiveFree = 0
		} else {
			consecutiveFree++
			if stats.Rounds-billed > maxFreeRounds {
				return messages, stats, fmt.Errorf("exceeded %d free read-only rounds", maxFreeRounds)
			}
		}

		// Inject feedback as a text block in the tool-result message.
		if billed > 0 {
			resultBlocks = append(resultBlocks, llm.ContentBlock{
				Type: llm.BlockText, Text: budgetMessage(maxRounds - billed),
			})
		} else if consecutiveFree >= 2 {
			resultBlocks = append(resultBlocks, llm.ContentBlock{
				Type: llm.BlockText, Text: freeRoundNudge(consecutiveFree),
			})
		}

		messages = append(messages, llm.Message{Role: llm.RoleUser, Content: resultBlocks})

		if done {
			return messages, stats, nil
		}

		if billed >= maxRounds {
			return messages, stats, fmt.Errorf("tool loop exceeded %d rounds", maxRounds)
		}
	}
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
// Messages start with "[" so compressHistory can strip stale ones from old messages.
func budgetMessage(remaining int) string {
	switch {
	case remaining <= 2:
		return "[URGENT: last edit round after this. Call done NOW.]"
	default:
		return fmt.Sprintf("[%d edit rounds remaining.]", remaining)
	}
}

// freeRoundNudge returns an escalating reminder when the model is only using free tools.
// Messages start with "[" so compressHistory can strip stale ones from old messages.
func freeRoundNudge(consecutive int) string {
	switch {
	case consecutive >= 10:
		return fmt.Sprintf("[You have used %d free rounds without editing. Make your change and call done NOW.]", consecutive)
	case consecutive >= 5:
		return fmt.Sprintf("[%d free rounds without editing. Make your change and call done.]", consecutive)
	default:
		return "[Reminder: make your edit and call done when ready.]"
	}
}

// compressHistory replaces the content of old tool_result blocks with a short
// summary to keep context size manageable. It preserves the most recent
// keepRecentRounds worth of assistant+tool-result exchanges intact.
func compressHistory(messages []llm.Message, keepRecent int) []llm.Message {
	var assistantCount int
	for _, m := range messages {
		if m.Role == llm.RoleAssistant {
			assistantCount++
		}
	}

	if assistantCount <= keepRecent {
		return messages
	}

	cutoff := assistantCount - keepRecent
	// Safe to share ContentBlock slices with the original: messages is
	// only appended to (never mutated in place) after this returns.
	out := make([]llm.Message, len(messages))
	var seen int
	for i, m := range messages {
		if m.Role == llm.RoleAssistant {
			seen++
		}
		// Compress tool_result blocks and strip stale injected text in messages
		// that precede the cutoff.
		if seen <= cutoff && m.Role == llm.RoleUser {
			compressed := make([]llm.ContentBlock, 0, len(m.Content))
			for _, b := range m.Content {
				// Drop stale budget/nudge text blocks from old messages.
				if b.Type == llm.BlockText && strings.HasPrefix(b.Text, "[") {
					continue
				}
				if b.Type == llm.BlockToolResult && len(b.Content) > 200 {
					compressed = append(compressed, llm.ContentBlock{
						Type:    llm.BlockToolResult,
						ID:      b.ID,
						Content: fmt.Sprintf("[%d bytes, truncated from history]", len(b.Content)),
						IsError: b.IsError,
					})
				} else {
					compressed = append(compressed, b)
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
