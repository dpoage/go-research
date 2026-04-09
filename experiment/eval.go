// Package experiment implements evaluation and result tracking for the research loop.
package experiment

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"strings"
	"time"
)

// Eval runs an evaluation command and extracts a numeric metric from its output.
type Eval struct {
	Command   string
	Extractor MetricExtractor
	Timeout   time.Duration
}

// NewEval creates an Eval from the config fields.
// The metricPattern is parsed by NewExtractor to select the appropriate backend.
// The source parameter controls where the extractor reads text from:
// "" or "stdout" reads from command output; "file:<path>" reads from a file.
func NewEval(command, metricPattern, source string, timeout time.Duration) (*Eval, error) {
	ext, err := NewExtractor(metricPattern)
	if err != nil {
		return nil, err
	}

	ext, err = applySource(ext, source)
	if err != nil {
		return nil, err
	}

	return &Eval{
		Command:   command,
		Extractor: ext,
		Timeout:   timeout,
	}, nil
}

// applySource wraps the extractor based on the source specification.
func applySource(ext MetricExtractor, source string) (MetricExtractor, error) {
	switch {
	case source == "" || source == "stdout":
		return ext, nil
	case strings.HasPrefix(source, "file:"):
		path := source[5:]
		if path == "" {
			return nil, fmt.Errorf("file source requires a path")
		}
		return NewFileSourceFromParts(path, ext), nil
	default:
		return nil, fmt.Errorf("unknown source %q (expected stdout or file:<path>)", source)
	}
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
