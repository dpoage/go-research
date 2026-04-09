package experiment

import (
	"fmt"
	"os"
	"strings"
)

// FileSource reads a file and delegates metric extraction to an inner extractor.
// It acts as a source wrapper, replacing the eval command output with file contents.
// Format: file:<path>:<inner-pattern>
type FileSource struct {
	path  string
	inner MetricExtractor
}

// NewFileSource parses a "path:inner-pattern" spec and returns a FileSource.
func NewFileSource(spec string) (*FileSource, error) {
	// Split into path and inner pattern at the first colon.
	idx := strings.Index(spec, ":")
	if idx < 0 {
		return nil, fmt.Errorf("file extractor requires format file:<path>:<extractor>, got %q", spec)
	}

	path := spec[:idx]
	innerPattern := spec[idx+1:]

	if path == "" {
		return nil, fmt.Errorf("file extractor path cannot be empty")
	}
	if innerPattern == "" {
		return nil, fmt.Errorf("file extractor requires an inner extractor after the path")
	}

	inner, err := NewExtractor(innerPattern)
	if err != nil {
		return nil, fmt.Errorf("file extractor inner: %w", err)
	}

	return &FileSource{path: path, inner: inner}, nil
}

// NewFileSourceFromParts creates a FileSource from an already-constructed
// path and inner extractor, enabling programmatic composition.
func NewFileSourceFromParts(path string, inner MetricExtractor) *FileSource {
	return &FileSource{path: path, inner: inner}
}

// Extract reads the file and delegates to the inner extractor.
func (e *FileSource) Extract(_ string) (float64, error) {
	data, err := os.ReadFile(e.path)
	if err != nil {
		return 0, fmt.Errorf("read metric file %q: %w", e.path, err)
	}
	contents := string(data)
	if contents == "" {
		return 0, fmt.Errorf("metric file %q is empty", e.path)
	}
	return e.inner.Extract(contents)
}
