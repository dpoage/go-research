package experiment

import (
	"math"
	"os"
	"path/filepath"
	"testing"
)

func TestFileExtractor_JQInner(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "results.json")
	os.WriteFile(path, []byte(`{"loss": 0.042}`), 0o644)

	ext, err := NewFileExtractor(path + ":jq:.loss")
	if err != nil {
		t.Fatal(err)
	}

	val, err := ext.Extract("ignored")
	if err != nil {
		t.Fatal(err)
	}
	if math.Abs(val-0.042) > 1e-9 {
		t.Errorf("got %f, want 0.042", val)
	}
}

func TestFileExtractor_LastNumberInner(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "output.txt")
	os.WriteFile(path, []byte("epoch 5\nfinal score: 0.95\n"), 0o644)

	ext, err := NewFileExtractor(path + ":last-number")
	if err != nil {
		t.Fatal(err)
	}

	val, err := ext.Extract("ignored")
	if err != nil {
		t.Fatal(err)
	}
	if math.Abs(val-0.95) > 1e-9 {
		t.Errorf("got %f, want 0.95", val)
	}
}

func TestFileExtractor_RegexInner(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "log.txt")
	os.WriteFile(path, []byte("score: 42.5\n"), 0o644)

	ext, err := NewFileExtractor(path + `:regex:score:\s+([0-9.]+)`)
	if err != nil {
		t.Fatal(err)
	}

	val, err := ext.Extract("ignored")
	if err != nil {
		t.Fatal(err)
	}
	if math.Abs(val-42.5) > 1e-9 {
		t.Errorf("got %f, want 42.5", val)
	}
}

func TestFileExtractor_MissingFile(t *testing.T) {
	ext, err := NewFileExtractor("/nonexistent/file.json:last-number")
	if err != nil {
		t.Fatal(err)
	}

	_, err = ext.Extract("ignored")
	if err == nil {
		t.Error("expected error for missing file")
	}
}

func TestFileExtractor_EmptyFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "empty.txt")
	os.WriteFile(path, []byte(""), 0o644)

	ext, err := NewFileExtractor(path + ":last-number")
	if err != nil {
		t.Fatal(err)
	}

	_, err = ext.Extract("ignored")
	if err == nil {
		t.Error("expected error for empty file")
	}
}

func TestFileExtractor_NoInnerPattern(t *testing.T) {
	_, err := NewFileExtractor("results.json")
	if err == nil {
		t.Error("expected error for missing inner pattern")
	}
}

func TestFileExtractor_EmptyPath(t *testing.T) {
	_, err := NewFileExtractor(":last-number")
	if err == nil {
		t.Error("expected error for empty path")
	}
}

func TestNewExtractor_FilePrefix(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "data.json")
	os.WriteFile(path, []byte(`{"val": 1.5}`), 0o644)

	ext, err := NewExtractor("file:" + path + ":jq:.val")
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := ext.(*FileExtractor); !ok {
		t.Errorf("expected *FileExtractor, got %T", ext)
	}

	val, err := ext.Extract("ignored")
	if err != nil {
		t.Fatal(err)
	}
	if math.Abs(val-1.5) > 1e-9 {
		t.Errorf("got %f, want 1.5", val)
	}
}
