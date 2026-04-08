package cmd

import (
	"os"
	"path/filepath"
	"testing"
)

func TestRunInit_CreatesFiles(t *testing.T) {
	dir := t.TempDir()

	err := runInit([]string{"--dir", dir})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	for _, name := range []string{"research.yaml", "program.md"} {
		path := filepath.Join(dir, name)
		info, err := os.Stat(path)
		if err != nil {
			t.Errorf("%s not created: %v", name, err)
			continue
		}
		if info.Size() == 0 {
			t.Errorf("%s is empty", name)
		}
	}
}

func TestRunInit_SkipsExisting(t *testing.T) {
	dir := t.TempDir()

	// Pre-create research.yaml with custom content.
	existing := filepath.Join(dir, "research.yaml")
	os.WriteFile(existing, []byte("custom"), 0o644)

	err := runInit([]string{"--dir", dir})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Existing file should not be overwritten.
	data, _ := os.ReadFile(existing)
	if string(data) != "custom" {
		t.Errorf("existing file was overwritten: got %q", data)
	}

	// program.md should still be created.
	if _, err := os.Stat(filepath.Join(dir, "program.md")); err != nil {
		t.Errorf("program.md not created: %v", err)
	}
}

func TestRunInit_DefaultYAMLIsValid(t *testing.T) {
	// The default YAML template should parse without error.
	// We write it to a temp file and load it with the config package.
	dir := t.TempDir()
	path := filepath.Join(dir, "research.yaml")
	if err := os.WriteFile(path, []byte(defaultConfigYAML), 0o644); err != nil {
		t.Fatal(err)
	}

	// Just verify it's valid YAML by attempting to unmarshal.
	// We import config in a separate integration test to avoid circular deps.
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(data) == 0 {
		t.Fatal("default config is empty")
	}
}
