package experiment

import (
	"fmt"
	"time"
)

// VerboseObserver prints detailed output for every loop event,
// matching the original loop output format.
type VerboseObserver struct{}

func (VerboseObserver) IterationStart(iter, _ int) {
	fmt.Printf("\n=== Iteration %d ===\n", iter)
}

func (VerboseObserver) AgentText(text string) {
	fmt.Printf("Agent: %s\n", text)
}

func (VerboseObserver) ToolCall(name, output string) {
	fmt.Printf("Tool %s: %s\n", name, truncateDisplay(output, 200))
}

func (VerboseObserver) EvalStarted() {
	fmt.Println("Running evaluation...")
}

func (VerboseObserver) EvalResult(_ int, metric float64, elapsed time.Duration) {
	fmt.Printf("Metric: %.6f (elapsed: %s)\n", metric, elapsed)
}

func (VerboseObserver) Improvement(_ int, _, previousBest float64) {
	fmt.Printf("Improvement! (previous best: %v)\n", formatMetric(previousBest))
}

func (VerboseObserver) NoImprovement(_ int, _ float64, best float64) {
	fmt.Printf("No improvement (best: %v). Reverting.\n", formatMetric(best))
}

func (VerboseObserver) IterationError(iter int, err error) {
	fmt.Printf("Error in iteration %d: %v\n", iter, err)
}

func (VerboseObserver) Warning(msg string) {
	fmt.Printf("Warning: %s\n", msg)
}

func (VerboseObserver) Complete(bestMetric float64) {
	fmt.Printf("\nDone. Best metric: %v\n", formatMetric(bestMetric))
}
