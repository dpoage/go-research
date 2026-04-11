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

	allMetrics := allMetricValues(rows)
	bestMetric, hasBest := bestAllMetric(rows, direction)
	trend := display.Sparkline(allMetrics)

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
	if last, ok := lastMetric(rows); ok {
		fmt.Printf("Last metric: %.6f (%s)\n", last.Metric, last.Status)
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

// allMetricValues extracts metric values from all non-error rows in order,
// giving a complete picture of the experiment trajectory.
func allMetricValues(rows []resultRow) []float64 {
	var vals []float64
	for _, r := range rows {
		if r.Status != experiment.StatusError {
			vals = append(vals, r.Metric)
		}
	}
	return vals
}

// bestAllMetric returns the best metric value among all non-error rows, respecting direction.
func bestAllMetric(rows []resultRow, direction config.Direction) (float64, bool) {
	best := math.NaN()
	found := false

	for _, r := range rows {
		if r.Status == experiment.StatusError {
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

// lastMetric returns the most recent non-error result row.
func lastMetric(rows []resultRow) (resultRow, bool) {
	for i := len(rows) - 1; i >= 0; i-- {
		if rows[i].Status != experiment.StatusError {
			return rows[i], true
		}
	}
	return resultRow{}, false
}
