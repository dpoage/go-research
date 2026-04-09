package experiment

import (
	"math"
	"os"
	"path/filepath"
	"testing"
)

func TestFileSource_JQInner(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "results.json")
	os.WriteFile(path, []byte(`{"loss": 0.042}`), 0o644)

	ext, err := NewFileSource(path + ":jq:.loss")
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

func TestFileSource_LastNumberInner(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "output.txt")
	os.WriteFile(path, []byte("epoch 5\nfinal score: 0.95\n"), 0o644)

	ext, err := NewFileSource(path + ":last-number")
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

func TestFileSource_RegexInner(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "log.txt")
	os.WriteFile(path, []byte("score: 42.5\n"), 0o644)

	ext, err := NewFileSource(path + `:regex:score:\s+([0-9.]+)`)
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

func TestFileSource_MissingFile(t *testing.T) {
	ext, err := NewFileSource("/nonexistent/file.json:last-number")
	if err != nil {
		t.Fatal(err)
	}

	_, err = ext.Extract("ignored")
	if err == nil {
		t.Error("expected error for missing file")
	}
}

func TestFileSource_EmptyFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "empty.txt")
	os.WriteFile(path, []byte(""), 0o644)

	ext, err := NewFileSource(path + ":last-number")
	if err != nil {
		t.Fatal(err)
	}

	_, err = ext.Extract("ignored")
	if err == nil {
		t.Error("expected error for empty file")
	}
}

func TestFileSource_NoInnerPattern(t *testing.T) {
	_, err := NewFileSource("results.json")
	if err == nil {
		t.Error("expected error for missing inner pattern")
	}
}

func TestFileSource_EmptyPath(t *testing.T) {
	_, err := NewFileSource(":last-number")
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
	if _, ok := ext.(*FileSource); !ok {
		t.Errorf("expected *FileSource, got %T", ext)
	}

	val, err := ext.Extract("ignored")
	if err != nil {
		t.Fatal(err)
	}
	if math.Abs(val-1.5) > 1e-9 {
		t.Errorf("got %f, want 1.5", val)
	}
}

func TestNewFileSourceFromParts(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "metrics.txt")
	os.WriteFile(path, []byte("result: 3.14\n"), 0o644)

	inner, err := NewRegexExtractor(`result:\s+([0-9.]+)`)
	if err != nil {
		t.Fatal(err)
	}

	fs := NewFileSourceFromParts(path, inner)

	val, err := fs.Extract("ignored")
	if err != nil {
		t.Fatal(err)
	}
	if math.Abs(val-3.14) > 1e-9 {
		t.Errorf("got %f, want 3.14", val)
	}
}
