package experiment

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestNewResultLogger_CreatesFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "results.tsv")

	logger, err := NewResultLogger(path)
	if err != nil {
		t.Fatal(err)
	}
	if logger == nil {
		t.Fatal("expected non-nil logger")
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(string(data), "iteration\t") {
		t.Errorf("expected header row, got: %q", data)
	}
}

func TestNewResultLogger_ExistingFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "results.tsv")

	// Pre-create with content.
	existing := "iteration\tmetric\tstatus\telapsed_ms\ttimestamp\tnote\n1\t0.5\tkeep\t100\t2024-01-01T00:00:00Z\tfirst\n"
	if err := os.WriteFile(path, []byte(existing), 0644); err != nil {
		t.Fatal(err)
	}

	logger, err := NewResultLogger(path)
	if err != nil {
		t.Fatal(err)
	}
	if logger == nil {
		t.Fatal("expected non-nil logger")
	}

	// Verify existing content preserved.
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != existing {
		t.Error("existing file content was modified")
	}
}

func TestResultLogger_Append(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "results.tsv")

	logger, err := NewResultLogger(path)
	if err != nil {
		t.Fatal(err)
	}

	err = logger.Append(ResultEntry{
		Iteration: 1,
		Metric:    0.95,
		Status:    StatusKeep,
		Elapsed:   150 * time.Millisecond,
		Note:      "improved accuracy",
	})
	if err != nil {
		t.Fatal(err)
	}

	err = logger.Append(ResultEntry{
		Iteration: 2,
		Metric:    0.90,
		Status:    StatusDiscard,
		Elapsed:   200 * time.Millisecond,
		Note:      "regression",
	})
	if err != nil {
		t.Fatal(err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}

	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	if len(lines) != 3 { // header + 2 entries
		t.Fatalf("expected 3 lines, got %d: %v", len(lines), lines)
	}

	// Check first data row.
	fields := strings.Split(lines[1], "\t")
	if len(fields) != 6 {
		t.Fatalf("expected 6 fields, got %d: %v", len(fields), fields)
	}
	if fields[0] != "1" || fields[2] != "keep" {
		t.Errorf("unexpected fields: %v", fields)
	}
}

func TestResultLogger_Append_SanitizesNote(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "results.tsv")

	logger, err := NewResultLogger(path)
	if err != nil {
		t.Fatal(err)
	}

	err = logger.Append(ResultEntry{
		Iteration: 1,
		Metric:    0.5,
		Status:    StatusError,
		Elapsed:   0,
		Note:      "has\ttab\tand\nnewline",
	})
	if err != nil {
		t.Fatal(err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}

	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	if len(lines) != 2 {
		t.Fatalf("expected 2 lines, got %d", len(lines))
	}

	// The note field should have no tabs or newlines.
	fields := strings.Split(lines[1], "\t")
	if len(fields) != 6 {
		t.Fatalf("expected 6 fields (sanitized note), got %d: %v", len(fields), fields)
	}
}
