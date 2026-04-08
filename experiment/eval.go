// Package experiment implements evaluation and result tracking for the research loop.
package experiment

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"regexp"
	"strconv"
	"time"
)

// Eval runs an evaluation command and extracts a numeric metric from its output.
type Eval struct {
	Command string
	Metric  *regexp.Regexp
	Timeout time.Duration
}

// NewEval creates an Eval from the config fields.
// The metric pattern must contain exactly one capturing group that matches a number.
func NewEval(command, metricPattern string, timeout time.Duration) (*Eval, error) {
	re, err := regexp.Compile(metricPattern)
	if err != nil {
		return nil, fmt.Errorf("compile metric pattern: %w", err)
	}
	if re.NumSubexp() < 1 {
		return nil, fmt.Errorf("metric pattern must contain at least one capturing group, got %q", metricPattern)
	}
	return &Eval{
		Command: command,
		Metric:  re,
		Timeout: timeout,
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

	metric, err := extractMetric(e.Metric, combined)
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

func extractMetric(re *regexp.Regexp, output string) (float64, error) {
	matches := re.FindStringSubmatch(output)
	if matches == nil {
		return 0, fmt.Errorf("metric pattern %q did not match output", re.String())
	}
	val, err := strconv.ParseFloat(matches[1], 64)
	if err != nil {
		return 0, fmt.Errorf("parse metric value %q: %w", matches[1], err)
	}
	return val, nil
}
