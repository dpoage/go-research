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
