package experiment

import (
	"math"
	"testing"
)

func TestJQExtractor_SimpleKey(t *testing.T) {
	ext, err := NewJQExtractor(".loss")
	if err != nil {
		t.Fatal(err)
	}
	val, err := ext.Extract(`{"loss": 0.5}`)
	if err != nil {
		t.Fatal(err)
	}
	if math.Abs(val-0.5) > 1e-9 {
		t.Errorf("got %f, want 0.5", val)
	}
}

func TestJQExtractor_NestedPath(t *testing.T) {
	ext, err := NewJQExtractor(".results.val_bpb")
	if err != nil {
		t.Fatal(err)
	}
	val, err := ext.Extract(`{"results": {"val_bpb": 1.23}}`)
	if err != nil {
		t.Fatal(err)
	}
	if math.Abs(val-1.23) > 1e-9 {
		t.Errorf("got %f, want 1.23", val)
	}
}

func TestJQExtractor_ArrayIndex(t *testing.T) {
	ext, err := NewJQExtractor(".[0].score")
	if err != nil {
		t.Fatal(err)
	}
	val, err := ext.Extract(`[{"score": 42}]`)
	if err != nil {
		t.Fatal(err)
	}
	if val != 42 {
		t.Errorf("got %f, want 42", val)
	}
}

func TestJQExtractor_NoLeadingDot(t *testing.T) {
	ext, err := NewJQExtractor("loss")
	if err != nil {
		t.Fatal(err)
	}
	val, err := ext.Extract(`{"loss": 0.1}`)
	if err != nil {
		t.Fatal(err)
	}
	if math.Abs(val-0.1) > 1e-9 {
		t.Errorf("got %f, want 0.1", val)
	}
}

func TestJQExtractor_NonJSON(t *testing.T) {
	ext, err := NewJQExtractor(".loss")
	if err != nil {
		t.Fatal(err)
	}
	_, err = ext.Extract("not json")
	if err == nil {
		t.Error("expected error for non-JSON output")
	}
}

func TestJQExtractor_MissingKey(t *testing.T) {
	ext, err := NewJQExtractor(".missing")
	if err != nil {
		t.Fatal(err)
	}
	_, err = ext.Extract(`{"loss": 0.5}`)
	if err == nil {
		t.Error("expected error for missing key")
	}
}

func TestJQExtractor_NonNumericValue(t *testing.T) {
	ext, err := NewJQExtractor(".name")
	if err != nil {
		t.Fatal(err)
	}
	_, err = ext.Extract(`{"name": "alice"}`)
	if err == nil {
		t.Error("expected error for non-numeric value")
	}
}

func TestJQExtractor_ArrayOutOfRange(t *testing.T) {
	ext, err := NewJQExtractor(".[5].score")
	if err != nil {
		t.Fatal(err)
	}
	_, err = ext.Extract(`[{"score": 1}]`)
	if err == nil {
		t.Error("expected error for out-of-range index")
	}
}

func TestJQExtractor_EmptyPath(t *testing.T) {
	_, err := NewJQExtractor("")
	if err == nil {
		t.Error("expected error for empty path")
	}
}

func TestJQExtractor_IntegerValue(t *testing.T) {
	ext, err := NewJQExtractor(".count")
	if err != nil {
		t.Fatal(err)
	}
	val, err := ext.Extract(`{"count": 100}`)
	if err != nil {
		t.Fatal(err)
	}
	if val != 100 {
		t.Errorf("got %f, want 100", val)
	}
}
