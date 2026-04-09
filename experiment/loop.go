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

// Loop runs the autonomous experiment cycle.
type Loop struct {
	config      *config.Config
	provider    llm.Provider
	executor    *tools.Executor
	eval        *Eval
	git         *Git
	logger      *ResultLogger
	resultsPath string
	observer    Observer
}

// NewLoop creates a Loop wired to the given collaborators.
func NewLoop(cfg *config.Config, provider llm.Provider, executor *tools.Executor, eval *Eval, git *Git, logger *ResultLogger, resultsPath string, observer Observer) *Loop {
	return &Loop{
		config:      cfg,
		provider:    provider,
		executor:    executor,
		eval:        eval,
		git:         git,
		logger:      logger,
		resultsPath: resultsPath,
		observer:    observer,
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

	for iter := 1; maxIter <= 0 || iter <= maxIter; iter++ {
		if ctx.Err() != nil {
			return ctx.Err()
		}

		l.observer.IterationStart(iter, maxIter)

		prompt := l.buildPrompt(iter, bestMetric)
		messages := []llm.Message{llm.NewTextMessage(llm.RoleUser, prompt)}

		// Run tool-use turns until the model stops requesting tools.
		messages, err = l.toolLoop(ctx, system, messages, toolDefs)
		if err != nil {
			l.logError(iter, err)
			continue
		}

		// Evaluate.
		l.observer.EvalStarted()
		result := l.eval.Run(ctx)
		if result.Error != nil {
			l.observer.IterationError(iter, result.Error)
			l.revert(iter)
			l.logResult(iter, result, StatusError, result.Error.Error())
			continue
		}

		l.observer.EvalResult(iter, result.Metric, result.Elapsed)

		// Decide keep/discard.
		if l.isBetter(result.Metric, bestMetric) {
			l.observer.Improvement(iter, result.Metric, bestMetric)
			bestMetric = result.Metric

			// Log before commit so the results file is included in the snapshot.
			l.logResult(iter, result, StatusKeep, "")

			if err := l.git.Commit(fmt.Sprintf("iter %d: metric=%.6f", iter, result.Metric), l.resultsPath); err != nil {
				l.observer.Warning(fmt.Sprintf("git commit failed: %v", err))
			}
		} else {
			l.observer.NoImprovement(iter, result.Metric, bestMetric)
			l.revert(iter)
			// Log after revert so the entry isn't reverted with the code changes.
			l.logResult(iter, result, StatusDiscard, fmt.Sprintf("best=%.6f", bestMetric))
		}
	}

	l.observer.Complete(bestMetric)
	return nil
}

func (l *Loop) buildPrompt(iter int, bestMetric float64) string {
	prompt := fmt.Sprintf("Iteration %d.\n\nAllowed files: %v\nEval command: %s\nDirection: %v\n",
		iter, l.config.Files, l.config.Eval.Command, l.config.Eval.Direction)

	if !math.IsNaN(bestMetric) {
		prompt += fmt.Sprintf("Current best metric: %.6f\n", bestMetric)
	}
	prompt += "\nPropose and apply your next experiment. Use the tools to read and modify files."
	return prompt
}

// toolLoop runs LLM completion in a loop, dispatching tool calls until the model
// returns end_turn or we hit max rounds.
func (l *Loop) toolLoop(ctx context.Context, system string, messages []llm.Message, toolDefs []llm.ToolDef) ([]llm.Message, error) {
	const maxRounds = 20

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
		for _, tc := range toolCalls {
			result := l.executor.Dispatch(ctx, tc.Name, tc.Input)

			// Truncate long output to avoid blowing context.
			output := result.Output
			if len(output) > maxToolOutput {
				output = output[:maxToolOutput] + "\n... (truncated)"
			}

			l.observer.ToolCall(tc.Name, output)
			messages = append(messages, llm.NewToolResultMessage(tc.ID, output, result.IsError))
		}
	}

	return messages, fmt.Errorf("tool loop exceeded %d rounds", maxRounds)
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

func (l *Loop) logError(iter int, err error) {
	l.observer.IterationError(iter, err)
	if logErr := l.logger.Append(ResultEntry{
		Iteration: iter,
		Status:    StatusError,
		Note:      err.Error(),
	}); logErr != nil {
		l.observer.Warning(fmt.Sprintf("log error failed: %v", logErr))
	}
}

func formatMetric(v float64) string {
	if math.IsNaN(v) {
		return "none"
	}
	return fmt.Sprintf("%.6f", v)
}

func truncateDisplay(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max] + "..."
}
