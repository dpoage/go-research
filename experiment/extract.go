package experiment

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

// MetricExtractor extracts a numeric metric from eval command output.
type MetricExtractor interface {
	Extract(output string) (float64, error)
}

// NewExtractor parses a metric pattern string and returns the appropriate extractor.
// Supported prefixes:
//   - "regex:" or bare string — regex with a capture group
//   - "jq:" — JSON dot-path extraction
//   - "last-number" — last float found in output
//   - "file:<path>:<inner>" — read a file, then apply inner extractor
func NewExtractor(pattern string) (MetricExtractor, error) {
	switch {
	case pattern == "last-number":
		return &LastNumberExtractor{}, nil
	case strings.HasPrefix(pattern, "jq:"):
		return NewJQExtractor(pattern[3:])
	case strings.HasPrefix(pattern, "file:"):
		return NewFileSource(pattern[5:])
	case strings.HasPrefix(pattern, "regex:"):
		return NewRegexExtractor(pattern[6:])
	default:
		return NewRegexExtractor(pattern)
	}
}

// RegexExtractor extracts a metric using a regex with a capture group.
type RegexExtractor struct {
	re *regexp.Regexp
}

// NewRegexExtractor compiles the pattern and validates it has a capture group.
func NewRegexExtractor(pattern string) (*RegexExtractor, error) {
	re, err := regexp.Compile(pattern)
	if err != nil {
		return nil, fmt.Errorf("compile metric pattern: %w", err)
	}
	if re.NumSubexp() < 1 {
		return nil, fmt.Errorf("metric pattern must contain at least one capturing group, got %q", pattern)
	}
	return &RegexExtractor{re: re}, nil
}

func (e *RegexExtractor) Extract(output string) (float64, error) {
	matches := e.re.FindStringSubmatch(output)
	if matches == nil {
		return 0, fmt.Errorf("metric pattern %q did not match output", e.re.String())
	}
	val, err := strconv.ParseFloat(matches[1], 64)
	if err != nil {
		return 0, fmt.Errorf("parse metric value %q: %w", matches[1], err)
	}
	return val, nil
}
