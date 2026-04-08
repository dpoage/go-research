package experiment

import (
	"context"
	"math"
	"testing"
	"time"
)

func TestNewEval_ValidPattern(t *testing.T) {
	ev, err := NewEval("echo 'accuracy: 0.95'", `accuracy:\s+(\d+\.\d+)`, 5*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if ev.Command != "echo 'accuracy: 0.95'" {
		t.Errorf("unexpected command: %s", ev.Command)
	}
}

func TestNewEval_NoCaptureGroup(t *testing.T) {
	_, err := NewEval("echo test", `accuracy`, 5*time.Second)
	if err == nil {
		t.Error("expected error for pattern without capture group")
	}
}

func TestNewEval_InvalidRegex(t *testing.T) {
	_, err := NewEval("echo test", `(unclosed`, 5*time.Second)
	if err == nil {
		t.Error("expected error for invalid regex")
	}
}

func TestEval_Run_Success(t *testing.T) {
	ev, err := NewEval("echo 'loss: 0.042'", `loss:\s+(\d+\.\d+)`, 5*time.Second)
	if err != nil {
		t.Fatal(err)
	}

	result := ev.Run(context.Background())
	if result.Error != nil {
		t.Fatalf("unexpected error: %v", result.Error)
	}
	if math.Abs(result.Metric-0.042) > 1e-9 {
		t.Errorf("metric = %f, want 0.042", result.Metric)
	}
	if result.Elapsed <= 0 {
		t.Error("expected positive elapsed time")
	}
}

func TestEval_Run_CommandFailure(t *testing.T) {
	ev, err := NewEval("exit 1", `(\d+)`, 5*time.Second)
	if err != nil {
		t.Fatal(err)
	}

	result := ev.Run(context.Background())
	if result.Error == nil {
		t.Error("expected error for failing command")
	}
}

func TestEval_Run_NoMatch(t *testing.T) {
	ev, err := NewEval("echo 'no metric here'", `accuracy:\s+(\d+\.\d+)`, 5*time.Second)
	if err != nil {
		t.Fatal(err)
	}

	result := ev.Run(context.Background())
	if result.Error == nil {
		t.Error("expected error when metric pattern does not match")
	}
}

func TestEval_Run_Timeout(t *testing.T) {
	ev, err := NewEval("sleep 10", `(\d+)`, 100*time.Millisecond)
	if err != nil {
		t.Fatal(err)
	}

	result := ev.Run(context.Background())
	if result.Error == nil {
		t.Error("expected timeout error")
	}
}
