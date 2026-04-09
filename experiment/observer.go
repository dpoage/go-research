package experiment

import "time"

// Observer receives lifecycle events from the experiment loop.
// Implementations control how experiment progress is displayed.
type Observer interface {
	// IterationStart is called at the beginning of each iteration.
	IterationStart(iter, maxIter int)

	// AgentText is called when the LLM emits text (not tool calls).
	AgentText(text string)

	// ToolCall is called after each tool is dispatched.
	ToolCall(name, output string)

	// EvalStarted is called when evaluation begins.
	EvalStarted()

	// EvalResult is called with the metric after a successful evaluation.
	EvalResult(iter int, metric float64, elapsed time.Duration)

	// Improvement is called when the metric improves.
	Improvement(iter int, metric, previousBest float64)

	// NoImprovement is called when the metric does not improve.
	NoImprovement(iter int, metric, best float64)

	// IterationError is called when an iteration fails (eval or tool error).
	IterationError(iter int, err error)

	// Warning is called for non-fatal issues (git commit fail, log fail, revert fail).
	Warning(msg string)

	// Complete is called when the loop finishes.
	Complete(bestMetric float64)
}
