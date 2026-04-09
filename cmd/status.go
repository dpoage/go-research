package cmd

import (
	"errors"
	"fmt"
	"math"
	"os"

	flag "github.com/spf13/pflag"

	"github.com/dpoage/go-research/config"
	"github.com/dpoage/go-research/display"
	"github.com/dpoage/go-research/experiment"
)

func runStatus(gf globalFlags, args []string) error {
	fs := flag.NewFlagSet("status", flag.ContinueOnError)
	resultFile := fs.String("results", defaultResultsFile, "path to TSV result log")
	if err := fs.Parse(args); err != nil {
		return err
	}

	rows, err := parseResults(*resultFile)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("no results found (%s does not exist)", *resultFile)
		}
		return fmt.Errorf("parse results: %w", err)
	}

	// Load config for direction; fall back to showing metric without direction label.
	var direction config.Direction
	cfg, cfgErr := config.Load(gf.config)
	if cfgErr == nil {
		direction = cfg.Eval.Direction
	}

	// Get current git branch via experiment.Git (always enabled for status display).
	git := experiment.NewGit(true, ".", nil)
	branch, _ := git.CurrentBranch()

	// Calculate stats.
	iterCount := len(rows)

	var lastTimestamp string
	if iterCount > 0 {
		lastTimestamp = rows[iterCount-1].Timestamp
	}

	keptMetrics := keptMetricValues(rows)
	bestMetric, hasBest := bestKeptMetric(rows, direction)
	trend := display.Sparkline(keptMetrics)

	// Print output.
	fmt.Printf("Branch:      %s\n", branch)
	fmt.Printf("Iterations:  %d\n", iterCount)
	if hasBest {
		if direction != "" {
			fmt.Printf("Best metric: %.6f (%v)\n", bestMetric, direction)
		} else {
			fmt.Printf("Best metric: %.6f\n", bestMetric)
		}
	} else {
		fmt.Println("Best metric: n/a")
	}
	if trend != "" {
		fmt.Printf("Trend:       %s\n", trend)
	}
	if lastTimestamp != "" {
		fmt.Printf("Last run:    %s\n", lastTimestamp)
	} else {
		fmt.Println("Last run:    n/a")
	}

	return nil
}

// keptMetricValues extracts the metric values from "keep" rows in order.
func keptMetricValues(rows []resultRow) []float64 {
	var vals []float64
	for _, r := range rows {
		if r.Status == experiment.StatusKeep {
			vals = append(vals, r.Metric)
		}
	}
	return vals
}

// bestKeptMetric returns the best metric value among "keep" rows, respecting direction.
func bestKeptMetric(rows []resultRow, direction config.Direction) (float64, bool) {
	best := math.NaN()
	found := false

	for _, r := range rows {
		if r.Status != experiment.StatusKeep {
			continue
		}
		if !found {
			best = r.Metric
			found = true
			continue
		}
		switch direction {
		case config.DirectionMinimize:
			if r.Metric < best {
				best = r.Metric
			}
		default: // maximize or unknown
			if r.Metric > best {
				best = r.Metric
			}
		}
	}

	return best, found
}
