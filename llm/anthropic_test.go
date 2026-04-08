package llm

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/dpoage/go-research/config"
)

// configProviderConfig is a test helper to create a ProviderConfig.
func configProviderConfig(backend, model string) config.ProviderConfig {
	return config.ProviderConfig{
		Backend:   backend,
		Model:     model,
		MaxTokens: 1024,
	}
}

func TestAnthropic_Complete_TextResponse(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Verify request structure.
		if r.Header.Get("x-api-key") != "test-key" {
			t.Error("missing or wrong api key header")
		}
		if r.Header.Get("anthropic-version") != defaultAnthropicVersion {
			t.Error("missing or wrong anthropic-version header")
		}

		body, _ := io.ReadAll(r.Body)
		var req anthropicRequest
		if err := json.Unmarshal(body, &req); err != nil {
			t.Fatalf("unmarshal request: %v", err)
		}
		if req.Model != "claude-test" {
			t.Errorf("model = %q, want %q", req.Model, "claude-test")
		}
		if req.System != "You are helpful." {
			t.Errorf("system = %q, want %q", req.System, "You are helpful.")
		}

		resp := anthropicResponse{
			StopReason: "end_turn",
			Content: []anthropicContentBlock{
				{Type: "text", Text: "Hello!"},
			},
		}
		resp.Usage.InputTokens = 10
		resp.Usage.OutputTokens = 5
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer srv.Close()

	a := &Anthropic{
		apiKey:    "test-key",
		model:     "claude-test",
		url:       srv.URL,
		maxTokens: 1024,
		client:    srv.Client(),
	}

	resp, err := a.Complete(context.Background(), &Request{
		System: "You are helpful.",
		Messages: []Message{
			NewTextMessage("user", "Hi"),
		},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.StopReason != StopEndTurn {
		t.Errorf("stop_reason = %q, want %q", resp.StopReason, StopEndTurn)
	}
	if resp.TextContent() != "Hello!" {
		t.Errorf("text = %q, want %q", resp.TextContent(), "Hello!")
	}
	if resp.Usage.InputTokens != 10 || resp.Usage.OutputTokens != 5 {
		t.Errorf("usage = %+v", resp.Usage)
	}
}

func TestAnthropic_Complete_ToolUse(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		var req anthropicRequest
		json.Unmarshal(body, &req)

		// Verify tools were sent.
		if len(req.Tools) != 1 {
			t.Errorf("tools count = %d, want 1", len(req.Tools))
		}

		resp := anthropicResponse{
			StopReason: "tool_use",
			Content: []anthropicContentBlock{
				{Type: "text", Text: "I'll read the file."},
				{
					Type:  "tool_use",
					ID:    "toolu_123",
					Name:  "read_file",
					Input: json.RawMessage(`{"path":"train.py"}`),
				},
			},
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer srv.Close()

	a := &Anthropic{
		apiKey:    "test-key",
		model:     "claude-test",
		url:       srv.URL,
		maxTokens: 1024,
		client:    srv.Client(),
	}

	schema := json.RawMessage(`{"type":"object","properties":{"path":{"type":"string"}}}`)
	resp, err := a.Complete(context.Background(), &Request{
		Messages: []Message{NewTextMessage("user", "Read train.py")},
		Tools: []ToolDef{{
			Name:        "read_file",
			Description: "Read a file",
			InputSchema: schema,
		}},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.StopReason != StopToolUse {
		t.Errorf("stop_reason = %q, want %q", resp.StopReason, StopToolUse)
	}

	blocks := resp.ToolUseBlocks()
	if len(blocks) != 1 {
		t.Fatalf("tool_use blocks = %d, want 1", len(blocks))
	}
	if blocks[0].Name != "read_file" || blocks[0].ID != "toolu_123" {
		t.Errorf("unexpected tool_use block: %+v", blocks[0])
	}

	var input struct{ Path string }
	if err := json.Unmarshal(blocks[0].Input, &input); err != nil {
		t.Fatalf("unmarshal input: %v", err)
	}
	if input.Path != "train.py" {
		t.Errorf("path = %q, want %q", input.Path, "train.py")
	}
}

func TestAnthropic_Complete_ToolResultRoundtrip(t *testing.T) {
	// Simulate a two-turn conversation: tool_use -> tool_result -> end_turn.
	callCount := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		body, _ := io.ReadAll(r.Body)
		var req anthropicRequest
		json.Unmarshal(body, &req)

		var resp anthropicResponse
		if callCount == 1 {
			resp = anthropicResponse{
				StopReason: "tool_use",
				Content: []anthropicContentBlock{
					{Type: "tool_use", ID: "tu_1", Name: "read_file", Input: json.RawMessage(`{"path":"x.txt"}`)},
				},
			}
		} else {
			// Verify that the tool_result was sent correctly.
			if len(req.Messages) < 3 {
				t.Errorf("expected at least 3 messages in turn 2, got %d", len(req.Messages))
			}
			resp = anthropicResponse{
				StopReason: "end_turn",
				Content: []anthropicContentBlock{
					{Type: "text", Text: "The file contains hello."},
				},
			}
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer srv.Close()

	a := &Anthropic{
		apiKey:    "test-key",
		model:     "claude-test",
		url:       srv.URL,
		maxTokens: 1024,
		client:    srv.Client(),
	}

	// Turn 1: user message -> tool_use.
	messages := []Message{NewTextMessage("user", "Read x.txt")}
	resp1, err := a.Complete(context.Background(), &Request{Messages: messages})
	if err != nil {
		t.Fatalf("turn 1 error: %v", err)
	}
	if resp1.StopReason != StopToolUse {
		t.Fatalf("turn 1 stop_reason = %q", resp1.StopReason)
	}

	// Turn 2: append assistant response + tool_result -> end_turn.
	messages = append(messages, Message{Role: "assistant", Content: resp1.Content})
	messages = append(messages, NewToolResultMessage("tu_1", "hello", false))

	resp2, err := a.Complete(context.Background(), &Request{Messages: messages})
	if err != nil {
		t.Fatalf("turn 2 error: %v", err)
	}
	if resp2.StopReason != StopEndTurn {
		t.Errorf("turn 2 stop_reason = %q", resp2.StopReason)
	}
	if resp2.TextContent() != "The file contains hello." {
		t.Errorf("turn 2 text = %q", resp2.TextContent())
	}
}

func TestAnthropic_Complete_APIError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		w.Write([]byte(`{"error":{"type":"invalid_request","message":"bad"}}`))
	}))
	defer srv.Close()

	a := &Anthropic{
		apiKey:    "test-key",
		model:     "claude-test",
		url:       srv.URL,
		maxTokens: 1024,
		client:    srv.Client(),
	}

	_, err := a.Complete(context.Background(), &Request{
		Messages: []Message{NewTextMessage("user", "hi")},
	})
	if err == nil {
		t.Fatal("expected error for 400 response")
	}
}

func TestMarshalContentBlocks_ToolResult(t *testing.T) {
	blocks := []ContentBlock{{
		Type:    "tool_result",
		ID:      "tu_1",
		Content: "file contents",
		IsError: false,
	}}

	raw, err := marshalContentBlocks(blocks)
	if err != nil {
		t.Fatalf("marshal error: %v", err)
	}

	// Verify tool_use_id is used (not id) in the wire format.
	var out []map[string]any
	json.Unmarshal(raw, &out)
	if len(out) != 1 {
		t.Fatalf("expected 1 block, got %d", len(out))
	}
	if out[0]["tool_use_id"] != "tu_1" {
		t.Errorf("expected tool_use_id = tu_1, got %v", out[0]["tool_use_id"])
	}
	if _, hasID := out[0]["id"]; hasID {
		t.Error("tool_result should use tool_use_id, not id")
	}
}
