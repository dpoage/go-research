package experiment

import (
	"fmt"
	"regexp"
	"strconv"
)

// numberPattern matches integers, decimals, and scientific notation.
var numberPattern = regexp.MustCompile(`[-+]?(?:\d+\.?\d*|\.\d+)(?:[eE][-+]?\d+)?`)

// LastNumberExtractor extracts the last floating-point number from the output.
type LastNumberExtractor struct{}

// Extract finds all number-like tokens in output and returns the last one as a float64.
func (e *LastNumberExtractor) Extract(output string) (float64, error) {
	matches := numberPattern.FindAllString(output, -1)
	if len(matches) == 0 {
		return 0, fmt.Errorf("no numeric value found in output")
	}
	last := matches[len(matches)-1]
	val, err := strconv.ParseFloat(last, 64)
	if err != nil {
		return 0, fmt.Errorf("parse last number %q: %w", last, err)
	}
	return val, nil
}
