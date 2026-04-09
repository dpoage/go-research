package experiment

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
)

// JQExtractor extracts a metric from JSON output using a dot-separated path.
type JQExtractor struct {
	segments []pathSegment
}

type pathSegment struct {
	key   string // map key (empty if index is used)
	index int    // array index (-1 if key is used)
}

// NewJQExtractor creates a JQ extractor for the given dot-path.
func NewJQExtractor(path string) (*JQExtractor, error) {
	if path == "" {
		return nil, fmt.Errorf("jq path cannot be empty")
	}

	segments, err := parsePath(path)
	if err != nil {
		return nil, err
	}
	if len(segments) == 0 {
		return nil, fmt.Errorf("jq path %q has no segments", path)
	}

	return &JQExtractor{segments: segments}, nil
}

func (e *JQExtractor) Extract(output string) (float64, error) {
	var data any
	if err := json.Unmarshal([]byte(output), &data); err != nil {
		return 0, fmt.Errorf("parse JSON output: %w", err)
	}

	current := data
	for _, seg := range e.segments {
		if seg.index >= 0 {
			arr, ok := current.([]any)
			if !ok {
				return 0, fmt.Errorf("expected array at index [%d], got %T", seg.index, current)
			}
			if seg.index >= len(arr) {
				return 0, fmt.Errorf("array index [%d] out of range (len %d)", seg.index, len(arr))
			}
			current = arr[seg.index]
		} else {
			m, ok := current.(map[string]any)
			if !ok {
				return 0, fmt.Errorf("expected object for key %q, got %T", seg.key, current)
			}
			val, exists := m[seg.key]
			if !exists {
				return 0, fmt.Errorf("key %q not found in object", seg.key)
			}
			current = val
		}
	}

	switch v := current.(type) {
	case float64:
		return v, nil
	case json.Number:
		return v.Float64()
	default:
		return 0, fmt.Errorf("value at path is %T, not a number", current)
	}
}

// parsePath splits a dot-path like ".results[0].loss" into segments.
func parsePath(path string) ([]pathSegment, error) {
	path = strings.TrimPrefix(path, ".")

	var segments []pathSegment
	for path != "" {
		if path[0] == '[' {
			end := strings.Index(path, "]")
			if end < 0 {
				return nil, fmt.Errorf("unclosed bracket in path %q", path)
			}
			idx, err := strconv.Atoi(path[1:end])
			if err != nil {
				return nil, fmt.Errorf("invalid array index %q: %w", path[1:end], err)
			}
			segments = append(segments, pathSegment{index: idx})
			path = path[end+1:]
			path = strings.TrimPrefix(path, ".")
			continue
		}

		end := strings.IndexAny(path, ".[")
		if end < 0 {
			end = len(path)
		}
		key := path[:end]
		if key == "" {
			return nil, fmt.Errorf("empty key in path")
		}
		segments = append(segments, pathSegment{key: key, index: -1})
		path = path[end:]
		path = strings.TrimPrefix(path, ".")
	}

	return segments, nil
}
