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

func TestBestAllMetric_Minimize(t *testing.T) {
	rows := []resultRow{
		{Iteration: 1, Metric: 0.9, Status: experiment.StatusKeep},
		{Iteration: 2, Metric: 0.5, Status: experiment.StatusKeep},
		{Iteration: 3, Metric: 0.3, Status: experiment.StatusDiscard},
		{Iteration: 4, Metric: 0.1, Status: experiment.StatusError},
	}
	best, ok := bestAllMetric(rows, config.DirectionMinimize)
	if !ok {
		t.Fatal("expected to find best metric")
	}
	if best != 0.3 {
		t.Errorf("best = %f, want 0.3 (discard row should be included)", best)
	}
}

func TestBestAllMetric_Maximize(t *testing.T) {
	rows := []resultRow{
		{Iteration: 1, Metric: 0.5, Status: experiment.StatusKeep},
		{Iteration: 2, Metric: 0.9, Status: experiment.StatusKeep},
		{Iteration: 3, Metric: 1.0, Status: experiment.StatusDiscard},
	}
	best, ok := bestAllMetric(rows, config.DirectionMaximize)
	if !ok {
		t.Fatal("expected to find best metric")
	}
	if best != 1.0 {
		t.Errorf("best = %f, want 1.0 (discard row should be included)", best)
	}
}

func TestAllMetricValues(t *testing.T) {
	rows := []resultRow{
		{Iteration: 1, Metric: 0.9, Status: experiment.StatusKeep},
		{Iteration: 2, Metric: 0.5, Status: experiment.StatusDiscard},
		{Iteration: 3, Metric: 0.7, Status: experiment.StatusKeep},
		{Iteration: 4, Metric: 0.3, Status: experiment.StatusError},
	}
	vals := allMetricValues(rows)
	if len(vals) != 3 {
		t.Fatalf("expected 3 values (error excluded), got %d", len(vals))
	}
	if vals[0] != 0.9 || vals[1] != 0.5 || vals[2] != 0.7 {
		t.Errorf("got %v, want [0.9 0.5 0.7]", vals)
	}
}

func TestAllMetricValues_Empty(t *testing.T) {
	vals := allMetricValues(nil)
	if len(vals) != 0 {
		t.Errorf("expected empty, got %v", vals)
	}
}

func TestLastMetric(t *testing.T) {
	rows := []resultRow{
		{Iteration: 1, Metric: 0.9, Status: experiment.StatusKeep},
		{Iteration: 2, Metric: 0.5, Status: experiment.StatusDiscard},
		{Iteration: 3, Metric: 0.0, Status: experiment.StatusError},
	}
	last, ok := lastMetric(rows)
	if !ok {
		t.Fatal("expected to find last metric")
	}
	if last.Metric != 0.5 || last.Status != experiment.StatusDiscard {
		t.Errorf("got metric=%f status=%s, want 0.5/discard", last.Metric, last.Status)
	}
}

func TestLastMetric_AllErrors(t *testing.T) {
	rows := []resultRow{
		{Iteration: 1, Metric: 0.0, Status: experiment.StatusError},
	}
	_, ok := lastMetric(rows)
	if ok {
		t.Error("expected no last metric when all rows are errors")
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

func TestBestAllMetric_OnlyErrors(t *testing.T) {
	rows := []resultRow{
		{Iteration: 1, Metric: 0.5, Status: experiment.StatusError},
		{Iteration: 2, Metric: 0.9, Status: experiment.StatusError},
	}
	_, ok := bestAllMetric(rows, config.DirectionMinimize)
	if ok {
		t.Error("expected no best metric when all rows are errors")
	}
}

func TestBestAllMetric_IncludesDiscards(t *testing.T) {
	rows := []resultRow{
		{Iteration: 1, Metric: 0.5, Status: experiment.StatusKeep},
		{Iteration: 2, Metric: 0.8, Status: experiment.StatusDiscard},
	}
	best, ok := bestAllMetric(rows, config.DirectionMaximize)
	if !ok {
		t.Fatal("expected to find best metric")
	}
	if best != 0.8 {
		t.Errorf("best = %f, want 0.8 (discard should be considered)", best)
	}
}
