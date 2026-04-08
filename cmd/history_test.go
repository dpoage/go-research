package cmd

import (
	"os"
	"path/filepath"
	"testing"
)

func TestRunHistory_ValidResults(t *testing.T) {
	dir := t.TempDir()

	results := "iteration\tmetric\tstatus\telapsed_ms\ttimestamp\tnote\n" +
		"1\t0.845000\tkeep\t1234\t2026-04-07T14:30:22Z\t\n" +
		"2\t0.823000\tkeep\t1456\t2026-04-07T14:31:45Z\t\n" +
		"3\t0.830000\tdiscard\t1200\t2026-04-07T14:32:50Z\tbest=0.823000\n"
	os.WriteFile(filepath.Join(dir, "results.tsv"), []byte(results), 0o644)

	gf := globalFlags{}
	err := runHistory(gf, []string{"--results", filepath.Join(dir, "results.tsv")})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestRunHistory_NoResultsFile(t *testing.T) {
	dir := t.TempDir()
	gf := globalFlags{}
	err := runHistory(gf, []string{"--results", filepath.Join(dir, "results.tsv")})
	if err == nil {
		t.Fatal("expected error for missing results.tsv")
	}
}

func TestRunHistory_HeaderOnly(t *testing.T) {
	dir := t.TempDir()

	results := "iteration\tmetric\tstatus\telapsed_ms\ttimestamp\tnote\n"
	os.WriteFile(filepath.Join(dir, "results.tsv"), []byte(results), 0o644)

	gf := globalFlags{}
	err := runHistory(gf, []string{"--results", filepath.Join(dir, "results.tsv")})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}
