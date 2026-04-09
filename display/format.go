package display

import (
	"fmt"
	"math"
)

// FormatMetric formats a metric value for display. NaN renders as "none".
func FormatMetric(v float64) string {
	if math.IsNaN(v) {
		return "none"
	}
	return fmt.Sprintf("%.6f", v)
}

// Truncate shortens s to max characters, appending "..." if truncated.
func Truncate(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max] + "..."
}
