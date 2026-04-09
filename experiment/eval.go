// Package experiment implements evaluation and result tracking for the research loop.
package experiment

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"time"

	"github.com/dpoage/go-research/config"
)

// Eval runs an evaluation command and extracts a numeric metric from its output.
type Eval struct {
	Command   string
	Extractor MetricExtractor
	Timeout   time.Duration
}

// NewEval creates an Eval from the config fields.
func NewEval(cfg config.EvalConfig) (*Eval, error) {
	ext, err := NewExtractor(cfg.Metric)
	if err != nil {
		return nil, err
	}

	if cfg.Source.IsFile() {
		ext = NewFileSourceFromParts(cfg.Source.Path, ext)
	}

	return &Eval{
		Command:   cfg.Command,
		Extractor: ext,
		Timeout:   cfg.Timeout.Duration,
	}, nil
}

// EvalResult holds the outcome of a single evaluation run.
type EvalResult struct {
	Metric  float64
	Output  string
	Elapsed time.Duration
	Error   error
}

// Run executes the eval command and extracts the metric.
func (e *Eval) Run(ctx context.Context) EvalResult {
	if e.Timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, e.Timeout)
		defer cancel()
	}

	start := time.Now()
	cmd := exec.CommandContext(ctx, "sh", "-c", e.Command)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()
	elapsed := time.Since(start)

	combined := stdout.String()
	if stderr.Len() > 0 {
		if combined != "" {
			combined += "\n"
		}
		combined += stderr.String()
	}

	if err != nil {
		return EvalResult{
			Output:  combined,
			Elapsed: elapsed,
			Error:   fmt.Errorf("eval command failed: %w", err),
		}
	}

	metric, err := e.Extractor.Extract(combined)
	if err != nil {
		return EvalResult{
			Output:  combined,
			Elapsed: elapsed,
			Error:   err,
		}
	}

	return EvalResult{
		Metric:  metric,
		Output:  combined,
		Elapsed: elapsed,
	}
}
