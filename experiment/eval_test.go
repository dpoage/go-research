package experiment

import (
	"context"
	"math"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/dpoage/go-research/config"
)

func evalCfg(command, metric string, source config.Source, timeout time.Duration) config.EvalConfig {
	return config.EvalConfig{
		Command:   command,
		Metric:    metric,
		Source:    source,
		Direction: config.DirectionMinimize,
		Timeout:   config.Duration{Duration: timeout},
	}
}

func TestNewEval_ValidPattern(t *testing.T) {
	ev, err := NewEval(evalCfg("echo 'accuracy: 0.95'", `accuracy:\s+(\d+\.\d+)`, config.NewSourceStdout(), 5*time.Second))
	if err != nil {
		t.Fatal(err)
	}
	if ev.Command != "echo 'accuracy: 0.95'" {
		t.Errorf("unexpected command: %s", ev.Command)
	}
}

func TestNewEval_NoCaptureGroup(t *testing.T) {
	_, err := NewEval(evalCfg("echo test", `accuracy`, config.NewSourceStdout(), 5*time.Second))
	if err == nil {
		t.Error("expected error for pattern without capture group")
	}
}

func TestNewEval_InvalidRegex(t *testing.T) {
	_, err := NewEval(evalCfg("echo test", `(unclosed`, config.NewSourceStdout(), 5*time.Second))
	if err == nil {
		t.Error("expected error for invalid regex")
	}
}

func TestEval_Run_Success(t *testing.T) {
	ev, err := NewEval(evalCfg("echo 'loss: 0.042'", `loss:\s+(\d+\.\d+)`, config.NewSourceStdout(), 5*time.Second))
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
	ev, err := NewEval(evalCfg("exit 1", `(\d+)`, config.NewSourceStdout(), 5*time.Second))
	if err != nil {
		t.Fatal(err)
	}

	result := ev.Run(context.Background())
	if result.Error == nil {
		t.Error("expected error for failing command")
	}
}

func TestEval_Run_NoMatch(t *testing.T) {
	ev, err := NewEval(evalCfg("echo 'no metric here'", `accuracy:\s+(\d+\.\d+)`, config.NewSourceStdout(), 5*time.Second))
	if err != nil {
		t.Fatal(err)
	}

	result := ev.Run(context.Background())
	if result.Error == nil {
		t.Error("expected error when metric pattern does not match")
	}
}

func TestEval_Run_Timeout(t *testing.T) {
	ev, err := NewEval(evalCfg("sleep 10", `(\d+)`, config.NewSourceStdout(), 100*time.Millisecond))
	if err != nil {
		t.Fatal(err)
	}

	result := ev.Run(context.Background())
	if result.Error == nil {
		t.Error("expected timeout error")
	}
}

func TestNewEval_WithFileSource(t *testing.T) {
	dir := t.TempDir()
	metricFile := filepath.Join(dir, "metrics.txt")
	os.WriteFile(metricFile, []byte("accuracy: 0.97\n"), 0o644)

	ev, err := NewEval(evalCfg("echo 'no metric here'", `accuracy:\s+(\d+\.\d+)`, config.NewSourceFile(metricFile), 5*time.Second))
	if err != nil {
		t.Fatal(err)
	}

	result := ev.Run(context.Background())
	if result.Error != nil {
		t.Fatalf("unexpected error: %v", result.Error)
	}
	if math.Abs(result.Metric-0.97) > 1e-9 {
		t.Errorf("metric = %f, want 0.97", result.Metric)
	}
}

func TestNewEval_StdoutSource(t *testing.T) {
	ev, err := NewEval(evalCfg("echo 'loss: 0.05'", `loss:\s+(\d+\.\d+)`, config.NewSourceStdout(), 5*time.Second))
	if err != nil {
		t.Fatal(err)
	}

	result := ev.Run(context.Background())
	if result.Error != nil {
		t.Fatalf("unexpected error: %v", result.Error)
	}
	if math.Abs(result.Metric-0.05) > 1e-9 {
		t.Errorf("metric = %f, want 0.05", result.Metric)
	}
}

func TestNewEval_FileSourceKind(t *testing.T) {
	// Verify that a file source is wired correctly even when the file doesn't
	// exist at construction time (the extractor only reads at Extract time).
	ev, err := NewEval(evalCfg("echo ignored", `(\d+)`, config.NewSourceFile("/tmp/nonexistent"), 5*time.Second))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ev == nil {
		t.Fatal("expected non-nil Eval")
	}
}
