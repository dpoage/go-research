package display

import "testing"

func TestSparkline(t *testing.T) {
	tests := []struct {
		name   string
		values []float64
		want   string
	}{
		{name: "empty", values: nil, want: ""},
		{name: "single value", values: []float64{5.0}, want: "▅"},
		{name: "all same", values: []float64{3, 3, 3}, want: "▅▅▅"},
		{name: "ascending", values: []float64{0, 1, 2, 3, 4, 5, 6, 7}, want: "▁▂▃▄▅▆▇█"},
		{name: "descending", values: []float64{7, 6, 5, 4, 3, 2, 1, 0}, want: "█▇▆▅▄▃▂▁"},
		{name: "two values", values: []float64{0, 1}, want: "▁█"},
		{name: "v shape", values: []float64{10, 5, 0, 5, 10}, want: "█▅▁▅█"},
		{name: "negative values", values: []float64{-3, -1, 0, 2}, want: "▁▄▅█"},
		{name: "tiny range", values: []float64{1.000, 1.001, 1.002}, want: "▁▄█"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := Sparkline(tt.values)
			if got != tt.want {
				t.Errorf("Sparkline(%v) = %q, want %q", tt.values, got, tt.want)
			}
		})
	}
}
