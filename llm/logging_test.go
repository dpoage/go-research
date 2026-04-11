package llm

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type stubProvider struct{}

func (m stubProvider) Complete(_ context.Context, _ *Request) (*Response, error) {
	return &Response{
		Content:    []ContentBlock{{Type: BlockText, Text: "hello"}},
		StopReason: StopEndTurn,
		Usage:      Usage{InputTokens: 10, OutputTokens: 5},
	}, nil
}

func TestLoggingProvider_WritesJSONL(t *testing.T) {
	dir := t.TempDir()
	lp, err := NewLoggingProvider(stubProvider{}, dir)
	if err != nil {
		t.Fatal(err)
	}

	resp, err := lp.Complete(context.Background(), &Request{
		System:    "test system",
		Messages:  []Message{NewTextMessage(RoleUser, "hi")},
		Tools:     []ToolDef{{Name: "read_file"}},
		MaxTokens: 100,
	})
	if err != nil {
		t.Fatal(err)
	}
	if resp.Content[0].Text != "hello" {
		t.Errorf("response not passed through: %v", resp)
	}

	data, err := os.ReadFile(lp.Path())
	if err != nil {
		t.Fatal(err)
	}

	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	if len(lines) != 1 {
		t.Fatalf("expected 1 line, got %d", len(lines))
	}

	var entry logEntry
	if err := json.Unmarshal([]byte(lines[0]), &entry); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}

	if entry.Seq != 1 {
		t.Errorf("seq = %d, want 1", entry.Seq)
	}
	if entry.Request.System != "test system" {
		t.Errorf("system = %q, want %q", entry.Request.System, "test system")
	}
	if len(entry.Request.Tools) != 1 || entry.Request.Tools[0] != "read_file" {
		t.Errorf("tools = %v, want [read_file]", entry.Request.Tools)
	}
	if entry.Response == nil {
		t.Fatal("response is nil")
	}
	if entry.Response.StopReason != StopEndTurn {
		t.Errorf("stop_reason = %q, want %q", entry.Response.StopReason, StopEndTurn)
	}
	if entry.Response.Usage.InputTokens != 10 {
		t.Errorf("input_tokens = %d, want 10", entry.Response.Usage.InputTokens)
	}
	if entry.Error != "" {
		t.Errorf("unexpected error: %s", entry.Error)
	}
}

func TestLoggingProvider_CreatesDir(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "nested", "dir")
	_, err := NewLoggingProvider(stubProvider{}, dir)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(dir); os.IsNotExist(err) {
		t.Error("directory was not created")
	}
}

func TestLoggingProvider_MultipleRequests(t *testing.T) {
	dir := t.TempDir()
	lp, err := NewLoggingProvider(stubProvider{}, dir)
	if err != nil {
		t.Fatal(err)
	}

	for i := 0; i < 3; i++ {
		_, err := lp.Complete(context.Background(), &Request{
			Messages:  []Message{NewTextMessage(RoleUser, "hi")},
			MaxTokens: 100,
		})
		if err != nil {
			t.Fatal(err)
		}
	}

	data, err := os.ReadFile(lp.Path())
	if err != nil {
		t.Fatal(err)
	}

	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	if len(lines) != 3 {
		t.Fatalf("expected 3 lines, got %d", len(lines))
	}

	// Verify sequence numbers.
	for i, line := range lines {
		var entry logEntry
		json.Unmarshal([]byte(line), &entry)
		if entry.Seq != i+1 {
			t.Errorf("line %d: seq = %d, want %d", i, entry.Seq, i+1)
		}
	}
}
