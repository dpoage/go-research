package experiment

import (
	"bufio"
	"errors"
	"fmt"
	"os"
	"strconv"
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

// Path returns the file path of the result log.
func (l *ResultLogger) Path() string {
	return l.path
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

// ResultRow is a parsed row from a results TSV file.
type ResultRow struct {
	Iteration int
	Metric    float64
	Status    Status
	ElapsedMs int64
	Timestamp string
	Note      string
}

// ParseResults reads and parses a TSV results file, skipping the header line.
func ParseResults(path string) ([]ResultRow, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	var rows []ResultRow
	scanner := bufio.NewScanner(f)

	// Skip header line.
	if !scanner.Scan() {
		if err := scanner.Err(); err != nil {
			return nil, fmt.Errorf("read results header: %w", err)
		}
		return rows, nil
	}

	lineNum := 1
	for scanner.Scan() {
		lineNum++
		line := scanner.Text()
		if line == "" {
			continue
		}

		fields := strings.Split(line, "\t")
		if len(fields) < 5 {
			return nil, fmt.Errorf("line %d: expected at least 5 tab-separated fields, got %d", lineNum, len(fields))
		}

		iter, err := strconv.Atoi(fields[0])
		if err != nil {
			return nil, fmt.Errorf("line %d: invalid iteration %q: %w", lineNum, fields[0], err)
		}

		metric, err := strconv.ParseFloat(fields[1], 64)
		if err != nil {
			return nil, fmt.Errorf("line %d: invalid metric %q: %w", lineNum, fields[1], err)
		}

		elapsed, err := strconv.ParseInt(fields[3], 10, 64)
		if err != nil {
			return nil, fmt.Errorf("line %d: invalid elapsed_ms %q: %w", lineNum, fields[3], err)
		}

		var note string
		if len(fields) >= 6 {
			note = fields[5]
		}

		rows = append(rows, ResultRow{
			Iteration: iter,
			Metric:    metric,
			Status:    Status(fields[2]),
			ElapsedMs: elapsed,
			Timestamp: fields[4],
			Note:      note,
		})
	}

	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("read results: %w", err)
	}

	return rows, nil
}
