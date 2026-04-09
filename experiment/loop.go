package experiment

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"os"

	"github.com/dpoage/go-research/config"
	"github.com/dpoage/go-research/llm"
	"github.com/dpoage/go-research/tools"
)

// maxToolOutput is the maximum bytes of tool output sent back to the LLM
// to avoid blowing the context window.
const maxToolOutput = 16000

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
func ToolDefs() []llm.ToolDef {
	return []llm.ToolDef{
		{
			Name:        tools.ToolReadFile,
			Description: "Read the contents of a file.",
			InputSchema: json.RawMessage(`{"type":"object","properties":{"path":{"type":"string","description":"Path to the file to read"}},"required":["path"]}`),
		},
		{
			Name:        tools.ToolWriteFile,
			Description: "Write content to a file. Only allowed files may be written.",
			InputSchema: json.RawMessage(`{"type":"object","properties":{"path":{"type":"string","description":"Path to the file to write"},"content":{"type":"string","description":"Content to write to the file"}},"required":["path","content"]}`),
		},
		{
			Name:        tools.ToolRunCommand,
			Description: "Run a shell command and return its output.",
			InputSchema: json.RawMessage(`{"type":"object","properties":{"command":{"type":"string","description":"Shell command to execute"}},"required":["command"]}`),
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
	toolDefs := ToolDefs()
	consecutiveErrors := 0
	var lastResult *iterOutcome

	for iter := 1; maxIter <= 0 || iter <= maxIter; iter++ {
		if ctx.Err() != nil {
			return ctx.Err()
		}

		l.observer.IterationStart(iter, maxIter)

		prompt := l.buildPrompt(iter, bestMetric, lastResult)
		messages := []llm.Message{llm.NewTextMessage(llm.RoleUser, prompt)}

		// Run tool-use turns until the model stops requesting tools.
		messages, err = l.toolLoop(ctx, system, messages, toolDefs)
		if err != nil {
			l.observer.IterationError(iter, err)
			l.logResult(iter, EvalResult{Error: err}, StatusError, err.Error())
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
			l.logResult(iter, result, StatusError, result.Error.Error())
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
			l.logResult(iter, result, StatusKeep, "")
			lastResult = &iterOutcome{Metric: result.Metric, Status: StatusKeep}

			if err := l.git.Commit(fmt.Sprintf("iter %d: metric=%.6f", iter, result.Metric), l.logger.Path()); err != nil {
				l.observer.Warning(fmt.Sprintf("git commit failed: %v", err))
			}
		} else {
			l.observer.NoImprovement(iter, result.Metric, bestMetric)
			l.revert(iter)
			// Log after revert so the entry isn't reverted with the code changes.
			l.logResult(iter, result, StatusDiscard, fmt.Sprintf("best=%.6f", bestMetric))
			lastResult = &iterOutcome{Metric: result.Metric, Status: StatusDiscard}
		}
	}

	l.observer.Complete(bestMetric)
	return nil
}

func (l *Loop) buildPrompt(iter int, bestMetric float64, last *iterOutcome) string {
	prompt := fmt.Sprintf("Iteration %d.\n\nAllowed files: %v\nEval command: %s\nDirection: %v\n",
		iter, l.config.Files, l.config.Eval.Command, l.config.Eval.Direction)

	if !math.IsNaN(bestMetric) {
		prompt += fmt.Sprintf("Current best metric: %.6f\n", bestMetric)
	}

	if last != nil {
		switch last.Status {
		case StatusKeep:
			prompt += fmt.Sprintf("Last iteration: kept (metric=%.6f). Try a different improvement.\n", last.Metric)
		case StatusDiscard:
			prompt += fmt.Sprintf("Last iteration: discarded (metric=%.6f). That approach didn't help — try something else.\n", last.Metric)
		case StatusError:
			prompt += "Last iteration: error. Try a different approach.\n"
		}
	}

	prompt += `
Make exactly ONE focused change per iteration.
After writing your change, stop — the eval runs automatically.
Do not read files you have already read in this iteration.
Do not verify or re-read files after writing them.`
	return prompt
}

// toolLoop runs LLM completion in a loop, dispatching tool calls until the model
// returns end_turn or we hit max rounds.
func (l *Loop) toolLoop(ctx context.Context, system string, messages []llm.Message, toolDefs []llm.ToolDef) ([]llm.Message, error) {
	maxRounds := l.config.Provider.MaxRounds
	hasWritten := false

	for round := range maxRounds {
		if ctx.Err() != nil {
			return messages, ctx.Err()
		}

		resp, err := l.provider.Complete(ctx, &llm.Request{
			System:    system,
			Messages:  messages,
			Tools:     toolDefs,
			MaxTokens: l.config.Provider.MaxTokens,
		})
		if err != nil {
			return messages, fmt.Errorf("LLM completion (round %d): %w", round+1, err)
		}

		// Append assistant response.
		messages = append(messages, llm.Message{Role: llm.RoleAssistant, Content: resp.Content})

		if text := resp.TextContent(); text != "" {
			l.observer.AgentText(text)
		}

		toolCalls := resp.ToolUseBlocks()
		if len(toolCalls) == 0 || resp.StopReason == llm.StopEndTurn {
			return messages, nil
		}

		// Dispatch each tool call and build result messages.
		wroteThisRound := false
		readOnly := true
		for _, tc := range toolCalls {
			if tc.Name == tools.ToolWriteFile {
				wroteThisRound = true
				readOnly = false
			} else if tc.Name != tools.ToolReadFile {
				readOnly = false
			}

			result := l.executor.Dispatch(ctx, tc.Name, tc.Input)

			// Truncate long output to avoid blowing context.
			output := result.Output
			if len(output) > maxToolOutput {
				output = output[:maxToolOutput] + "\n... (truncated)"
			}

			l.observer.ToolCall(tc.Name, output)
			messages = append(messages, llm.NewToolResultMessage(tc.ID, output, result.IsError))
		}

		// If the model previously wrote a file and this round only reads,
		// it is just verifying — exit the tool loop.
		if hasWritten && readOnly {
			return messages, nil
		}
		if wroteThisRound {
			hasWritten = true
		}
	}

	return messages, fmt.Errorf("tool loop exceeded %d rounds", maxRounds)
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

func (l *Loop) logResult(iter int, result EvalResult, status Status, note string) {
	if err := l.logger.Append(ResultEntry{
		Iteration: iter,
		Metric:    result.Metric,
		Status:    status,
		Elapsed:   result.Elapsed,
		Note:      note,
	}); err != nil {
		l.observer.Warning(fmt.Sprintf("log result failed: %v", err))
	}
}
