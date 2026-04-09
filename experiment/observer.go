package experiment

import "time"

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
	// Warning reports non-fatal issues (git commit fail, log fail, revert fail).
	Warning(msg string)
	Complete(bestMetric float64)
}
