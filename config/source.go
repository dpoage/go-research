package config

import (
	"fmt"
	"strings"

	"gopkg.in/yaml.v3"
)

const sourceFilePrefix = "file:"

// Source represents a parsed eval source — either stdout or a file path.
type Source struct {
	Kind string // "stdout" or "file"
	Path string // non-empty only when Kind == "file"
}

// SourceStdout is the default source that reads from command stdout.
var SourceStdout = Source{Kind: "stdout"}

// UnmarshalYAML parses the source value at YAML load time.
// Accepts "", "stdout", or "file:<path>".
func (s *Source) UnmarshalYAML(value *yaml.Node) error {
	var raw string
	if err := value.Decode(&raw); err != nil {
		return fmt.Errorf("source: %w", err)
	}
	parsed, err := parseSource(raw)
	if err != nil {
		return err
	}
	*s = parsed
	return nil
}

// MarshalYAML writes the source back to YAML.
func (s Source) MarshalYAML() (any, error) {
	if s.Kind == "file" {
		return sourceFilePrefix + s.Path, nil
	}
	return s.Kind, nil
}

// IsFile returns true when the source reads from a file.
func (s Source) IsFile() bool {
	return s.Kind == "file"
}

// String returns the canonical string representation.
func (s Source) String() string {
	if s.Kind == "file" {
		return sourceFilePrefix + s.Path
	}
	return "stdout"
}

// parseSource classifies a raw source string.
func parseSource(raw string) (Source, error) {
	switch {
	case raw == "" || raw == "stdout":
		return Source{Kind: "stdout"}, nil
	case strings.HasPrefix(raw, sourceFilePrefix):
		p := strings.TrimPrefix(raw, sourceFilePrefix)
		if p == "" {
			return Source{}, fmt.Errorf("file source requires a path")
		}
		return Source{Kind: "file", Path: p}, nil
	default:
		return Source{}, fmt.Errorf("eval.source must be %q or %q<path>, got %q", "stdout", sourceFilePrefix, raw)
	}
}
