package experiment

import (
	"errors"
	"math"
	"testing"
	"time"
)

// Verify StatusLineObserver implements Observer at compile time.
var _ Observer = (*StatusLineObserver)(nil)

func TestStatusLineObserver_TracksCounts(t *testing.T) {
	o := NewStatusLineObserver()

	o.IterationStart(1, 5)
	o.EvalStarted()
	o.EvalResult(1, 0.5, time.Second)
	o.Improvement(1, 0.5, math.NaN())

	o.IterationStart(2, 5)
	o.EvalStarted()
	o.EvalResult(2, 0.6, time.Second)
	o.NoImprovement(2, 0.6, 0.5)

	o.IterationStart(3, 5)
	o.IterationError(3, errors.New("fail"))

	if o.nKeep != 1 {
		t.Errorf("nKeep = %d, want 1", o.nKeep)
	}
	if o.nDisc != 1 {
		t.Errorf("nDisc = %d, want 1", o.nDisc)
	}
	if o.nErr != 1 {
		t.Errorf("nErr = %d, want 1", o.nErr)
	}
	if len(o.kept) != 1 || o.kept[0] != 0.5 {
		t.Errorf("kept = %v, want [0.5]", o.kept)
	}
	if o.best != 0.5 {
		t.Errorf("best = %f, want 0.5", o.best)
	}
}

func TestStatusLineObserver_NoOpsAreHarmless(t *testing.T) {
	o := NewStatusLineObserver()
	// These should not panic.
	o.AgentText("some text")
	o.Warning("some warning")
}

func TestStatusLineObserver_IterationStart_NoMaxIter(t *testing.T) {
	o := NewStatusLineObserver()
	// maxIter == 0 uses the "iter N" format without a slash.
	o.IterationStart(1, 0)
	if o.phase != "iter 1" {
		t.Errorf("phase = %q, want %q", o.phase, "iter 1")
	}
}

func TestStatusLineObserver_ToolCall(t *testing.T) {
	o := NewStatusLineObserver()
	o.IterationStart(1, 5)
	// ToolCall renders the tool name as the activity; should not panic.
	o.ToolCall("write_file", "output data")
}

func TestStatusLineObserver_EvalResult(t *testing.T) {
	o := NewStatusLineObserver()
	// EvalResult is a no-op but must not panic.
	o.EvalResult(1, 0.9, time.Second)
}

func TestStatusLineObserver_AgentText(t *testing.T) {
	o := NewStatusLineObserver()
	// AgentText is a no-op but must not panic.
	o.AgentText("hello from agent")
}

func TestStatusLineObserver_Complete_NaN(t *testing.T) {
	o := NewStatusLineObserver()
	o.IterationStart(1, 1)
	// Complete with NaN prints "No successful iterations."
	// It also calls clearLine which requires lastLen > 0.
	o.Complete(math.NaN())
}

func TestStatusLineObserver_Complete_WithMetric(t *testing.T) {
	o := NewStatusLineObserver()
	o.IterationStart(1, 2)
	o.EvalStarted()
	o.Improvement(1, 0.75, math.NaN())
	// Complete with a real metric prints stats including sparkline.
	o.Complete(0.75)
}

func TestStatusLineObserver_ClearLine_ZeroLastLen(t *testing.T) {
	o := NewStatusLineObserver()
	// clearLine is a no-op when lastLen == 0; should not panic.
	o.clearLine()
}

func TestStatusLineObserver_Warning(t *testing.T) {
	o := NewStatusLineObserver()
	// Warning is a no-op but must not panic.
	o.Warning("something went wrong")
}
