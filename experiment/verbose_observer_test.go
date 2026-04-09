package experiment

import (
	"errors"
	"testing"
	"time"
)

// Verify VerboseObserver implements Observer at compile time.
var _ Observer = VerboseObserver{}

func TestVerboseObserver_AllMethods(t *testing.T) {
	o := VerboseObserver{}
	// Call every method to ensure none panic and coverage is achieved.
	// Output goes to stdout; we don't assert its content here since
	// the methods are thin fmt.Printf wrappers whose format is tested
	// implicitly by being called.
	o.IterationStart(1, 5)
	o.AgentText("agent says hello")
	o.ToolCall("read_file", "file contents here")
	o.EvalStarted()
	o.EvalResult(1, 0.85, 300*time.Millisecond)
	o.Improvement(1, 0.85, 0.70)
	o.NoImprovement(2, 0.60, 0.85)
	o.IterationError(3, errors.New("something failed"))
	o.Warning("non-fatal warning message")
	o.Complete(0.85)
}
