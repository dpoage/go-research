package display

import (
	"math"
	"testing"
)

func TestFormatMetric(t *testing.T) {
	tests := []struct {
		name string
		v    float64
		want string
	}{
		{name: "NaN returns none", v: math.NaN(), want: "none"},
		{name: "zero", v: 0.0, want: "0.000000"},
		{name: "positive integer", v: 1.0, want: "1.000000"},
		{name: "negative value", v: -3.5, want: "-3.500000"},
		{name: "small fraction", v: 0.000001, want: "0.000001"},
		{name: "large value", v: 123456.789, want: "123456.789000"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := FormatMetric(tt.v)
			if got != tt.want {
				t.Errorf("FormatMetric(%v) = %q, want %q", tt.v, got, tt.want)
			}
		})
	}
}

func TestTruncate(t *testing.T) {
	tests := []struct {
		name string
		s    string
		max  int
		want string
	}{
		{name: "shorter than max", s: "hello", max: 10, want: "hello"},
		{name: "equal to max", s: "hello", max: 5, want: "hello"},
		{name: "longer than max", s: "hello world", max: 5, want: "hello..."},
		{name: "empty string", s: "", max: 5, want: ""},
		{name: "max zero with content", s: "hi", max: 0, want: "..."},
		{name: "single char at max one", s: "a", max: 1, want: "a"},
		{name: "two chars truncated to one", s: "ab", max: 1, want: "a..."},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := Truncate(tt.s, tt.max)
			if got != tt.want {
				t.Errorf("Truncate(%q, %d) = %q, want %q", tt.s, tt.max, got, tt.want)
			}
		})
	}
}
