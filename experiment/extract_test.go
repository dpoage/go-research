package experiment

import (
	"math"
	"testing"
)

func TestNewExtractor_BareRegex(t *testing.T) {
	ext, err := NewExtractor(`accuracy:\s+(\d+\.\d+)`)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := ext.(*RegexExtractor); !ok {
		t.Errorf("expected *RegexExtractor, got %T", ext)
	}
}

func TestNewExtractor_RegexPrefix(t *testing.T) {
	ext, err := NewExtractor(`regex:accuracy:\s+(\d+\.\d+)`)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := ext.(*RegexExtractor); !ok {
		t.Errorf("expected *RegexExtractor, got %T", ext)
	}
}

func TestNewExtractor_JQPrefix(t *testing.T) {
	ext, err := NewExtractor("jq:.results.loss")
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := ext.(*JQExtractor); !ok {
		t.Errorf("expected *JQExtractor, got %T", ext)
	}
}

func TestNewExtractor_LastNumber(t *testing.T) {
	ext, err := NewExtractor("last-number")
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := ext.(*LastNumberExtractor); !ok {
		t.Errorf("expected *LastNumberExtractor, got %T", ext)
	}
}

func TestNewExtractor_InvalidRegex(t *testing.T) {
	_, err := NewExtractor(`(unclosed`)
	if err == nil {
		t.Error("expected error for invalid regex")
	}
}

func TestNewExtractor_NoCaptureGroup(t *testing.T) {
	_, err := NewExtractor(`accuracy`)
	if err == nil {
		t.Error("expected error for regex without capture group")
	}
}

func TestRegexExtractor_Extract(t *testing.T) {
	ext, err := NewRegexExtractor(`loss:\s+(\d+\.\d+)`)
	if err != nil {
		t.Fatal(err)
	}

	val, err := ext.Extract("epoch 1\nloss: 0.042\n")
	if err != nil {
		t.Fatal(err)
	}
	if math.Abs(val-0.042) > 1e-9 {
		t.Errorf("got %f, want 0.042", val)
	}
}

func TestRegexExtractor_NoMatch(t *testing.T) {
	ext, err := NewRegexExtractor(`loss:\s+(\d+\.\d+)`)
	if err != nil {
		t.Fatal(err)
	}

	_, err = ext.Extract("no metric here")
	if err == nil {
		t.Error("expected error for no match")
	}
}

func TestRegexExtractor_NonNumericCapture(t *testing.T) {
	ext, err := NewRegexExtractor(`name:\s+(\w+)`)
	if err != nil {
		t.Fatal(err)
	}

	_, err = ext.Extract("name: alice")
	if err == nil {
		t.Error("expected error for non-numeric capture")
	}
}
