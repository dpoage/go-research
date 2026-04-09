package experiment

import (
	"fmt"
	"os"
	"strings"
)

// FileExtractor reads a file and delegates metric extraction to an inner extractor.
// Format: file:<path>:<inner-pattern>
type FileExtractor struct {
	path  string
	inner MetricExtractor
}

// NewFileExtractor parses a "path:inner-pattern" spec and returns a FileExtractor.
func NewFileExtractor(spec string) (*FileExtractor, error) {
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

	return &FileExtractor{path: path, inner: inner}, nil
}

// Extract reads the file and delegates to the inner extractor.
func (e *FileExtractor) Extract(_ string) (float64, error) {
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
