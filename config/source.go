package config

import (
	"fmt"
	"strings"

	"gopkg.in/yaml.v3"
)

const sourceFilePrefix = "file:"

// SourceKind distinguishes the eval output source.
type SourceKind string

const (
	SourceKindStdout SourceKind = "stdout"
	SourceKindFile   SourceKind = "file"
)

// Source represents a parsed eval source — either stdout or a file path.
type Source struct {
	Kind SourceKind // SourceKindStdout or SourceKindFile
	Path string     // non-empty only when Kind == SourceKindFile
}

// NewSourceStdout returns the default source that reads from command stdout.
func NewSourceStdout() Source {
	return Source{Kind: SourceKindStdout}
}

// NewSourceFile returns a source that reads from the given file path.
func NewSourceFile(path string) Source {
	return Source{Kind: SourceKindFile, Path: path}
}

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
	return s.String(), nil
}

// IsFile returns true when the source reads from a file.
func (s Source) IsFile() bool {
	return s.Kind == SourceKindFile
}

// String returns the canonical string representation.
func (s Source) String() string {
	if s.Kind == SourceKindFile {
		return sourceFilePrefix + s.Path
	}
	return string(SourceKindStdout)
}

// parseSource classifies a raw source string.
func parseSource(raw string) (Source, error) {
	switch {
	case raw == "" || raw == string(SourceKindStdout):
		return NewSourceStdout(), nil
	case strings.HasPrefix(raw, sourceFilePrefix):
		p, ok := strings.CutPrefix(raw, sourceFilePrefix)
		if !ok || p == "" {
			return Source{}, fmt.Errorf("file source requires a path")
		}
		return NewSourceFile(p), nil
	default:
		return Source{}, fmt.Errorf("eval.source must be %q or %q<path>, got %q", SourceKindStdout, sourceFilePrefix, raw)
	}
}
