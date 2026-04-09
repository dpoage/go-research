// Package display provides terminal rendering utilities for experiment output.
package display

import "math"

// blocks are the Unicode block elements used for sparkline rendering,
// ordered from lowest to highest.
var blocks = []rune{'▁', '▂', '▃', '▄', '▅', '▆', '▇', '█'}

// Sparkline renders a slice of float64 values as a unicode sparkline string.
// Each value maps to one of 8 block characters scaled between min and max.
// Returns an empty string for empty input.
func Sparkline(values []float64) string {
	if len(values) == 0 {
		return ""
	}

	min, max := values[0], values[0]
	for _, v := range values[1:] {
		if v < min {
			min = v
		}
		if v > max {
			max = v
		}
	}

	rng := max - min
	out := make([]rune, len(values))
	for i, v := range values {
		if rng == 0 {
			// All values identical — render as mid-height.
			out[i] = blocks[len(blocks)/2]
			continue
		}
		// Scale to [0, len(blocks)-1].
		idx := int(math.Round((v - min) / rng * float64(len(blocks)-1)))
		out[i] = blocks[idx]
	}
	return string(out)
}
