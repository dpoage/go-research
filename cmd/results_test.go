package cmd

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/dpoage/go-research/experiment"
)

func TestParseResults_Valid(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "results.tsv")
	content := "iteration\tmetric\tstatus\telapsed_ms\ttimestamp\tnote\n" +
		"1\t0.845000\tkeep\t1234\t2026-04-07T14:30:22Z\t\n" +
		"2\t0.823000\tkeep\t1456\t2026-04-07T14:31:45Z\t\n" +
		"3\t0.830000\tdiscard\t1200\t2026-04-07T14:32:50Z\tbest=0.823000\n"
	os.WriteFile(path, []byte(content), 0o644)

	rows, err := parseResults(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(rows) != 3 {
		t.Fatalf("got %d rows, want 3", len(rows))
	}

	if rows[0].Iteration != 1 || rows[0].Metric != 0.845 || rows[0].Status != experiment.StatusKeep {
		t.Errorf("row 0 = %+v", rows[0])
	}
	if rows[2].Note != "best=0.823000" {
		t.Errorf("row 2 note = %q, want %q", rows[2].Note, "best=0.823000")
	}
}

func TestParseResults_HeaderOnly(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "results.tsv")
	os.WriteFile(path, []byte("iteration\tmetric\tstatus\telapsed_ms\ttimestamp\tnote\n"), 0o644)

	rows, err := parseResults(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(rows) != 0 {
		t.Fatalf("got %d rows, want 0", len(rows))
	}
}

func TestParseResults_MalformedLine(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "results.tsv")
	content := "iteration\tmetric\tstatus\telapsed_ms\ttimestamp\tnote\n" +
		"bad\t0.5\tkeep\t100\t2026-04-07T14:30:22Z\t\n"
	os.WriteFile(path, []byte(content), 0o644)

	_, err := parseResults(path)
	if err == nil {
		t.Fatal("expected error for malformed iteration")
	}
}

func TestParseResults_FileNotFound(t *testing.T) {
	_, err := parseResults("/nonexistent/results.tsv")
	if err == nil {
		t.Fatal("expected error for missing file")
	}
}
