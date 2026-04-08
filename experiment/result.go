package experiment

import (
	"errors"
	"fmt"
	"os"
	"strings"
	"time"
)

const resultHeader = "iteration\tmetric\tstatus\telapsed_ms\ttimestamp\tnote"

// Status represents the outcome of an experiment iteration.
type Status string

const (
	StatusKeep    Status = "keep"
	StatusDiscard Status = "discard"
	StatusError   Status = "error"
)

// ResultLogger appends experiment results to a TSV file.
type ResultLogger struct {
	path string
}

// NewResultLogger creates a logger that writes to the given path.
// If the file does not exist it is created with a header row.
func NewResultLogger(path string) (*ResultLogger, error) {
	// Atomic create-if-not-exists to avoid TOCTOU race.
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0644)
	if err == nil {
		_, writeErr := f.WriteString(resultHeader + "\n")
		closeErr := f.Close()
		if writeErr != nil {
			return nil, fmt.Errorf("write result log header: %w", writeErr)
		}
		if closeErr != nil {
			return nil, fmt.Errorf("close result log: %w", closeErr)
		}
	} else if !errors.Is(err, os.ErrExist) {
		return nil, fmt.Errorf("create result log: %w", err)
	}
	return &ResultLogger{path: path}, nil
}

// ResultEntry is a single row in the result log.
type ResultEntry struct {
	Iteration int
	Metric    float64
	Status    Status
	Elapsed   time.Duration
	Note      string
}

// Append writes a result entry to the TSV log.
func (l *ResultLogger) Append(entry ResultEntry) error {
	sanitize := func(s string) string {
		s = strings.ReplaceAll(s, "\t", " ")
		s = strings.ReplaceAll(s, "\n", " ")
		return s
	}

	line := fmt.Sprintf("%d\t%.6f\t%s\t%d\t%s\t%s\n",
		entry.Iteration,
		entry.Metric,
		sanitize(string(entry.Status)),
		entry.Elapsed.Milliseconds(),
		time.Now().UTC().Format(time.RFC3339),
		sanitize(entry.Note),
	)

	f, err := os.OpenFile(l.path, os.O_APPEND|os.O_WRONLY, 0644)
	if err != nil {
		return fmt.Errorf("open result log: %w", err)
	}

	_, writeErr := f.WriteString(line)
	closeErr := f.Close()
	if writeErr != nil {
		return fmt.Errorf("write result: %w", writeErr)
	}
	if closeErr != nil {
		return fmt.Errorf("close result log: %w", closeErr)
	}
	return nil
}
