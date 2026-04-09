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

func TestResultLogger_Path(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "results.tsv")

	logger, err := NewResultLogger(path)
	if err != nil {
		t.Fatal(err)
	}
	if logger.Path() != path {
		t.Errorf("Path() = %q, want %q", logger.Path(), path)
	}
}

func TestNewResultLogger_BadDir(t *testing.T) {
	path := "/nonexistent-dir-xyz/results.tsv"
	_, err := NewResultLogger(path)
	if err == nil {
		t.Error("expected error creating logger in nonexistent dir, got nil")
	}
}

func TestResultLogger_Append_BadPath(t *testing.T) {
	// Create a logger with a valid path, then delete the file so Append fails.
	dir := t.TempDir()
	path := filepath.Join(dir, "results.tsv")

	logger, err := NewResultLogger(path)
	if err != nil {
		t.Fatal(err)
	}

	// Remove the file to force Append to fail.
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	// Also remove write permission on the directory to prevent re-creation.
	if err := os.Chmod(dir, 0500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chmod(dir, 0700) })

	err = logger.Append(ResultEntry{Iteration: 1, Metric: 0.5, Status: StatusKeep})
	if err == nil {
		t.Error("expected error from Append with missing file, got nil")
	}
}

func TestParseResults_MissingFile(t *testing.T) {
	_, err := ParseResults("/nonexistent-dir-xyz/results.tsv")
	if err == nil {
		t.Error("expected error for missing file, got nil")
	}
}

func TestParseResults_EmptyFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "empty.tsv")

	// Write only a header line (no data rows).
	if err := os.WriteFile(path, []byte(resultHeader+"\n"), 0644); err != nil {
		t.Fatal(err)
	}

	rows, err := ParseResults(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 0 {
		t.Errorf("expected 0 rows, got %d", len(rows))
	}
}

func TestParseResults_HeaderOnly_NoNewline(t *testing.T) {
	// A file with only a header and no newline still returns 0 rows.
	dir := t.TempDir()
	path := filepath.Join(dir, "header_only.tsv")

	if err := os.WriteFile(path, []byte(resultHeader), 0644); err != nil {
		t.Fatal(err)
	}

	rows, err := ParseResults(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 0 {
		t.Errorf("expected 0 rows, got %d", len(rows))
	}
}

func TestParseResults_CompletelyEmpty(t *testing.T) {
	// A completely empty file (0 bytes) means the header scan fails immediately.
	// ParseResults returns 0 rows with no error.
	dir := t.TempDir()
	path := filepath.Join(dir, "empty.tsv")

	if err := os.WriteFile(path, []byte{}, 0644); err != nil {
		t.Fatal(err)
	}

	rows, err := ParseResults(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 0 {
		t.Errorf("expected 0 rows for empty file, got %d", len(rows))
	}
}

func TestParseResults_ValidData(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "results.tsv")

	content := resultHeader + "\n" +
		"1\t0.950000\tkeep\t150\t2024-01-01T00:00:00Z\timproved\n" +
		"2\t0.900000\tdiscard\t200\t2024-01-01T00:01:00Z\tregression\n"
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	rows, err := ParseResults(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 2 {
		t.Fatalf("expected 2 rows, got %d", len(rows))
	}

	if rows[0].Iteration != 1 {
		t.Errorf("row[0].Iteration = %d, want 1", rows[0].Iteration)
	}
	if rows[0].Metric != 0.95 {
		t.Errorf("row[0].Metric = %f, want 0.95", rows[0].Metric)
	}
	if rows[0].Status != StatusKeep {
		t.Errorf("row[0].Status = %q, want %q", rows[0].Status, StatusKeep)
	}
	if rows[0].ElapsedMs != 150 {
		t.Errorf("row[0].ElapsedMs = %d, want 150", rows[0].ElapsedMs)
	}
	if rows[0].Note != "improved" {
		t.Errorf("row[0].Note = %q, want %q", rows[0].Note, "improved")
	}

	if rows[1].Iteration != 2 {
		t.Errorf("row[1].Iteration = %d, want 2", rows[1].Iteration)
	}
	if rows[1].Status != StatusDiscard {
		t.Errorf("row[1].Status = %q, want %q", rows[1].Status, StatusDiscard)
	}
}

func TestParseResults_SkipsBlankLines(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "results.tsv")

	content := resultHeader + "\n" +
		"1\t0.950000\tkeep\t150\t2024-01-01T00:00:00Z\tnote\n" +
		"\n" +
		"2\t0.800000\tdiscard\t100\t2024-01-01T00:02:00Z\t\n"
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	rows, err := ParseResults(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 2 {
		t.Fatalf("expected 2 rows (blank line skipped), got %d", len(rows))
	}
}

func TestParseResults_RowWithNoNote(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "results.tsv")

	// Row has exactly 5 fields (no note column).
	content := resultHeader + "\n" +
		"1\t0.500000\terror\t0\t2024-01-01T00:00:00Z\n"
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	rows, err := ParseResults(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 {
		t.Fatalf("expected 1 row, got %d", len(rows))
	}
	if rows[0].Note != "" {
		t.Errorf("expected empty note, got %q", rows[0].Note)
	}
}

func TestParseResults_MalformedTooFewFields(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "results.tsv")

	content := resultHeader + "\n" + "1\t0.5\tkeep\n" // only 3 fields
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	_, err := ParseResults(path)
	if err == nil {
		t.Error("expected error for malformed row (too few fields), got nil")
	}
}

func TestParseResults_MalformedBadIteration(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "results.tsv")

	content := resultHeader + "\n" + "notanumber\t0.5\tkeep\t100\t2024-01-01T00:00:00Z\n"
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	_, err := ParseResults(path)
	if err == nil {
		t.Error("expected error for invalid iteration field, got nil")
	}
}

func TestParseResults_MalformedBadMetric(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "results.tsv")

	content := resultHeader + "\n" + "1\tnotafloat\tkeep\t100\t2024-01-01T00:00:00Z\n"
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	_, err := ParseResults(path)
	if err == nil {
		t.Error("expected error for invalid metric field, got nil")
	}
}

func TestParseResults_MalformedBadElapsed(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "results.tsv")

	content := resultHeader + "\n" + "1\t0.5\tkeep\tnotanumber\t2024-01-01T00:00:00Z\n"
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	_, err := ParseResults(path)
	if err == nil {
		t.Error("expected error for invalid elapsed_ms field, got nil")
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
