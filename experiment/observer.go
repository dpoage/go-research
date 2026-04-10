package experiment

import "time"

// ToolLoopStats captures per-iteration metrics from the tool loop.
type ToolLoopStats struct {
	Rounds       int // Number of LLM completion rounds used.
	InputTokens  int // Total input tokens across all rounds.
	OutputTokens int // Total output tokens across all rounds.
}

// Observer receives lifecycle events from the experiment loop.
// Implementations control how experiment progress is displayed.
type Observer interface {
	IterationStart(iter, maxIter int)
	AgentText(text string)
	ToolCall(name, output string)
	EvalStarted()
	EvalResult(iter int, metric float64, elapsed time.Duration)
	Improvement(iter int, metric, previousBest float64)
	NoImprovement(iter int, metric, best float64)
	IterationError(iter int, err error)
	// ToolLoopComplete is called after the tool loop finishes for an iteration,
	// reporting round and token usage for performance analysis.
	ToolLoopComplete(iter int, stats ToolLoopStats)
	// Warning reports non-fatal issues (git commit fail, log fail, revert fail).
	Warning(msg string)
	Complete(bestMetric float64)
}
