package cmd

import "github.com/dpoage/go-research/experiment"

// resultRow is an alias for the experiment package's ResultRow.
type resultRow = experiment.ResultRow

// parseResults delegates to the experiment package where the format is defined.
func parseResults(path string) ([]resultRow, error) {
	return experiment.ParseResults(path)
}
