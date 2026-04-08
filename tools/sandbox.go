// Package tools implements tool execution with sandboxed file access.
package tools

import (
	"fmt"
	"path/filepath"
)

// Sandbox enforces file access restrictions for the experiment loop.
// Only files explicitly listed in the research config may be written.
type Sandbox struct {
	// allowed maps cleaned absolute paths to true.
	allowed map[string]bool
	// root is the working directory used to resolve relative paths.
	root string
}

// NewSandbox creates a Sandbox that permits writes only to the given files.
// Paths are resolved relative to root.
func NewSandbox(root string, files []string) (*Sandbox, error) {
	absRoot, err := filepath.Abs(root)
	if err != nil {
		return nil, fmt.Errorf("resolve root: %w", err)
	}

	allowed := make(map[string]bool, len(files))
	for _, f := range files {
		var abs string
		if filepath.IsAbs(f) {
			abs = filepath.Clean(f)
		} else {
			abs = filepath.Join(absRoot, f)
		}
		allowed[abs] = true
	}

	return &Sandbox{allowed: allowed, root: absRoot}, nil
}

func (s *Sandbox) CheckWrite(path string) error {
	if !s.allowed[s.resolve(path)] {
		return fmt.Errorf("write denied: %q is not in the allowed file list", path)
	}
	return nil
}

// CheckRead always succeeds — reads are unrestricted.
func (s *Sandbox) CheckRead(path string) error {
	return nil
}

func (s *Sandbox) resolve(path string) string {
	if filepath.IsAbs(path) {
		return filepath.Clean(path)
	}
	return filepath.Join(s.root, path)
}
