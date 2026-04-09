package cmd

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/dpoage/go-research/config"
	"github.com/dpoage/go-research/experiment"
)

// minimalConfig returns a valid research.yaml for testing.
func minimalConfig(direction string) string {
	return `program: program.md
files: [main.go]
eval:
  command: "go test"
  metric: score
  direction: ` + direction + `
provider:
  backend: anthropic
  model: claude-sonnet-4-20250514
`
}

func TestRunStatus_ValidResults(t *testing.T) {
	dir := t.TempDir()

	results := "iteration\tmetric\tstatus\telapsed_ms\ttimestamp\tnote\n" +
		"1\t0.845000\tkeep\t1234\t2026-04-07T14:30:22Z\t\n" +
		"2\t0.823000\tkeep\t1456\t2026-04-07T14:31:45Z\t\n" +
		"3\t0.900000\terror\t1200\t2026-04-07T14:32:50Z\tfailed\n"
	os.WriteFile(filepath.Join(dir, "results.tsv"), []byte(results), 0o644)
	os.WriteFile(filepath.Join(dir, "research.yaml"), []byte(minimalConfig("minimize")), 0o644)

	gf := globalFlags{config: filepath.Join(dir, "research.yaml")}
	err := runStatus(gf, []string{"--results", filepath.Join(dir, "results.tsv")})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestRunStatus_NoResultsFile(t *testing.T) {
	dir := t.TempDir()
	gf := globalFlags{config: filepath.Join(dir, "research.yaml")}
	err := runStatus(gf, []string{"--results", filepath.Join(dir, "results.tsv")})
	if err == nil {
		t.Fatal("expected error for missing results.tsv")
	}
}

func TestRunStatus_MaximizeDirection(t *testing.T) {
	dir := t.TempDir()

	results := "iteration\tmetric\tstatus\telapsed_ms\ttimestamp\tnote\n" +
		"1\t0.500000\tkeep\t100\t2026-04-07T14:30:22Z\t\n" +
		"2\t0.900000\tkeep\t100\t2026-04-07T14:31:45Z\t\n"
	os.WriteFile(filepath.Join(dir, "results.tsv"), []byte(results), 0o644)
	os.WriteFile(filepath.Join(dir, "research.yaml"), []byte(minimalConfig("maximize")), 0o644)

	gf := globalFlags{config: filepath.Join(dir, "research.yaml")}
	err := runStatus(gf, []string{"--results", filepath.Join(dir, "results.tsv")})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestBestKeptMetric_Minimize(t *testing.T) {
	rows := []resultRow{
		{Iteration: 1, Metric: 0.9, Status: experiment.StatusKeep},
		{Iteration: 2, Metric: 0.5, Status: experiment.StatusKeep},
		{Iteration: 3, Metric: 0.7, Status: experiment.StatusDiscard},
		{Iteration: 4, Metric: 0.3, Status: experiment.StatusError},
	}
	best, ok := bestKeptMetric(rows, config.DirectionMinimize)
	if !ok {
		t.Fatal("expected to find best metric")
	}
	if best != 0.5 {
		t.Errorf("best = %f, want 0.5", best)
	}
}

func TestBestKeptMetric_Maximize(t *testing.T) {
	rows := []resultRow{
		{Iteration: 1, Metric: 0.5, Status: experiment.StatusKeep},
		{Iteration: 2, Metric: 0.9, Status: experiment.StatusKeep},
		{Iteration: 3, Metric: 1.0, Status: experiment.StatusDiscard},
	}
	best, ok := bestKeptMetric(rows, config.DirectionMaximize)
	if !ok {
		t.Fatal("expected to find best metric")
	}
	if best != 0.9 {
		t.Errorf("best = %f, want 0.9", best)
	}
}

func TestKeptMetricValues(t *testing.T) {
	rows := []resultRow{
		{Iteration: 1, Metric: 0.9, Status: experiment.StatusKeep},
		{Iteration: 2, Metric: 0.5, Status: experiment.StatusDiscard},
		{Iteration: 3, Metric: 0.7, Status: experiment.StatusKeep},
		{Iteration: 4, Metric: 0.3, Status: experiment.StatusError},
	}
	vals := keptMetricValues(rows)
	if len(vals) != 2 {
		t.Fatalf("expected 2 values, got %d", len(vals))
	}
	if vals[0] != 0.9 || vals[1] != 0.7 {
		t.Errorf("got %v, want [0.9 0.7]", vals)
	}
}

func TestKeptMetricValues_Empty(t *testing.T) {
	vals := keptMetricValues(nil)
	if len(vals) != 0 {
		t.Errorf("expected empty, got %v", vals)
	}
}

func TestRunStatus_NoConfig(t *testing.T) {
	// Status should work even without a config file (direction will be empty).
	dir := t.TempDir()
	results := "iteration\tmetric\tstatus\telapsed_ms\ttimestamp\tnote\n" +
		"1\t0.845000\tkeep\t1234\t2026-04-07T14:30:22Z\t\n"
	os.WriteFile(filepath.Join(dir, "results.tsv"), []byte(results), 0o644)

	gf := globalFlags{config: filepath.Join(dir, "nope.yaml")}
	err := runStatus(gf, []string{"--results", filepath.Join(dir, "results.tsv")})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestRunStatus_EmptyResults(t *testing.T) {
	dir := t.TempDir()
	results := "iteration\tmetric\tstatus\telapsed_ms\ttimestamp\tnote\n"
	os.WriteFile(filepath.Join(dir, "results.tsv"), []byte(results), 0o644)

	gf := globalFlags{config: filepath.Join(dir, "nope.yaml")}
	err := runStatus(gf, []string{"--results", filepath.Join(dir, "results.tsv")})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestRunStatus_BadFlags(t *testing.T) {
	gf := globalFlags{}
	err := runStatus(gf, []string{"--bogus"})
	if err == nil {
		t.Fatal("expected error for unknown flag")
	}
}

func TestRunHistory_BadFlags(t *testing.T) {
	gf := globalFlags{}
	err := runHistory(gf, []string{"--bogus"})
	if err == nil {
		t.Fatal("expected error for unknown flag")
	}
}

func TestBestKeptMetric_NoKeepRows(t *testing.T) {
	rows := []resultRow{
		{Iteration: 1, Metric: 0.5, Status: experiment.StatusDiscard},
		{Iteration: 2, Metric: 0.9, Status: experiment.StatusError},
	}
	_, ok := bestKeptMetric(rows, config.DirectionMinimize)
	if ok {
		t.Error("expected no best metric for rows with no 'keep' status")
	}
}
