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

func TestNewOpenAI_WithEnvKey(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", "test-openai-key")

	cfg := config.ProviderConfig{
		Backend:   config.BackendOpenAI,
		Model:     "gpt-4",
		MaxTokens: 256,
	}
	o, err := NewOpenAI(cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if o == nil {
		t.Fatal("NewOpenAI returned nil")
	}
	if o.apiKey != "test-openai-key" {
		t.Errorf("apiKey = %q, want %q", o.apiKey, "test-openai-key")
	}
	if o.model != "gpt-4" {
		t.Errorf("model = %q, want %q", o.model, "gpt-4")
	}
	if o.url != defaultOpenAIURL {
		t.Errorf("url = %q, want default %q", o.url, defaultOpenAIURL)
	}
	if o.maxTokens != 256 {
		t.Errorf("maxTokens = %d, want 256", o.maxTokens)
	}
	if o.client == nil {
		t.Error("client is nil")
	}
}

func TestNewOpenAI_WithURLOverride(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", "test-key")

	cfg := config.ProviderConfig{
		Backend: config.BackendOpenAI,
		Model:   "gpt-4",
		URL:     "http://localhost:9090",
	}
	o, err := NewOpenAI(cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if o.url != "http://localhost:9090" {
		t.Errorf("url = %q, want %q", o.url, "http://localhost:9090")
	}
}

func TestNewOpenAI_NoAPIKey(t *testing.T) {
	cfg := config.ProviderConfig{
		Backend:   config.BackendOpenAI,
		APIKeyEnv: "TEST_OPENAI_MISSING_KEY",
		Model:     "gpt-4",
	}
	_, err := NewOpenAI(cfg)
	if err == nil {
		t.Fatal("expected error when API key env var is not set")
	}
}

func TestOpenAI_Complete_TextResponse(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "Bearer test-key" {
			t.Errorf("Authorization = %q, want %q", got, "Bearer test-key")
		}

		body, _ := io.ReadAll(r.Body)
		var req openaiRequest
		if err := json.Unmarshal(body, &req); err != nil {
			t.Fatalf("unmarshal request: %v", err)
		}
		if req.Model != "gpt-4" {
			t.Errorf("model = %q, want %q", req.Model, "gpt-4")
		}
		// System message should be first in messages.
		if len(req.Messages) < 2 {
			t.Fatalf("expected at least 2 messages, got %d", len(req.Messages))
		}
		if req.Messages[0].Role != "system" || req.Messages[0].Content != "You are helpful." {
			t.Errorf("system message = %+v", req.Messages[0])
		}
		if req.Messages[1].Role != "user" || req.Messages[1].Content != "Hi" {
			t.Errorf("user message = %+v", req.Messages[1])
		}

		resp := openaiResponse{
			Choices: []openaiChoice{{
				Message:      openaiMsg{Role: "assistant", Content: "Hello!"},
				FinishReason: "stop",
			}},
		}
		resp.Usage.PromptTokens = 10
		resp.Usage.CompletionTokens = 5
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer srv.Close()

	o := &OpenAI{
		apiKey:    "test-key",
		model:     "gpt-4",
		url:       srv.URL,
		maxTokens: 1024,
		client:    srv.Client(),
	}

	resp, err := o.Complete(context.Background(), &Request{
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

func TestOpenAI_Complete_ToolUse(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		var req openaiRequest
		json.Unmarshal(body, &req)

		if len(req.Tools) != 1 {
			t.Errorf("tools count = %d, want 1", len(req.Tools))
		}
		if req.Tools[0].Type != "function" {
			t.Errorf("tool type = %q, want %q", req.Tools[0].Type, "function")
		}
		if req.Tools[0].Function.Name != "read_file" {
			t.Errorf("tool name = %q, want %q", req.Tools[0].Function.Name, "read_file")
		}

		resp := openaiResponse{
			Choices: []openaiChoice{{
				Message: openaiMsg{
					Role:    "assistant",
					Content: "I'll read the file.",
					ToolCalls: []openaiToolCall{{
						ID:   "call_123",
						Type: "function",
						Function: openaiToolCallFunction{
							Name:      "read_file",
							Arguments: `{"path":"train.py"}`,
						},
					}},
				},
				FinishReason: "tool_calls",
			}},
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer srv.Close()

	o := &OpenAI{
		apiKey:    "test-key",
		model:     "gpt-4",
		url:       srv.URL,
		maxTokens: 1024,
		client:    srv.Client(),
	}

	schema := json.RawMessage(`{"type":"object","properties":{"path":{"type":"string"}}}`)
	resp, err := o.Complete(context.Background(), &Request{
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
	if blocks[0].Name != "read_file" || blocks[0].ID != "call_123" {
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

func TestOpenAI_Complete_ToolResultRoundtrip(t *testing.T) {
	callCount := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		body, _ := io.ReadAll(r.Body)
		var req openaiRequest
		json.Unmarshal(body, &req)

		var resp openaiResponse
		if callCount == 1 {
			resp = openaiResponse{
				Choices: []openaiChoice{{
					Message: openaiMsg{
						Role: "assistant",
						ToolCalls: []openaiToolCall{{
							ID:   "call_1",
							Type: "function",
							Function: openaiToolCallFunction{
								Name:      "read_file",
								Arguments: `{"path":"x.txt"}`,
							},
						}},
					},
					FinishReason: "tool_calls",
				}},
			}
		} else {
			// Verify that the tool result was sent as role "tool" with tool_call_id.
			var foundTool bool
			for _, m := range req.Messages {
				if m.Role == "tool" && m.ToolCallID == "call_1" && m.Content == "hello" {
					foundTool = true
				}
			}
			if !foundTool {
				t.Error("expected tool message with call_1 and content 'hello'")
			}
			resp = openaiResponse{
				Choices: []openaiChoice{{
					Message:      openaiMsg{Role: "assistant", Content: "The file contains hello."},
					FinishReason: "stop",
				}},
			}
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer srv.Close()

	o := &OpenAI{
		apiKey:    "test-key",
		model:     "gpt-4",
		url:       srv.URL,
		maxTokens: 1024,
		client:    srv.Client(),
	}

	// Turn 1: user message -> tool_use.
	messages := []Message{NewTextMessage("user", "Read x.txt")}
	resp1, err := o.Complete(context.Background(), &Request{
		Messages: messages,
		Tools: []ToolDef{{
			Name:        "read_file",
			Description: "Read a file",
			InputSchema: json.RawMessage(`{"type":"object","properties":{"path":{"type":"string"}}}`),
		}},
	})
	if err != nil {
		t.Fatalf("turn 1 error: %v", err)
	}
	if resp1.StopReason != StopToolUse {
		t.Fatalf("turn 1 stop_reason = %q", resp1.StopReason)
	}

	// Turn 2: append assistant response + tool_result -> end_turn.
	messages = append(messages, Message{Role: "assistant", Content: resp1.Content})
	messages = append(messages, NewToolResultMessage("call_1", "hello", false))

	resp2, err := o.Complete(context.Background(), &Request{
		Messages: messages,
		Tools: []ToolDef{{
			Name:        "read_file",
			Description: "Read a file",
			InputSchema: json.RawMessage(`{"type":"object","properties":{"path":{"type":"string"}}}`),
		}},
	})
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

func TestOpenAI_Complete_APIError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		w.Write([]byte(`{"error":{"type":"invalid_request","message":"bad"}}`))
	}))
	defer srv.Close()

	o := &OpenAI{
		apiKey:    "test-key",
		model:     "gpt-4",
		url:       srv.URL,
		maxTokens: 1024,
		client:    srv.Client(),
	}

	_, err := o.Complete(context.Background(), &Request{
		Messages: []Message{NewTextMessage("user", "hi")},
	})
	if err == nil {
		t.Fatal("expected error for 400 response")
	}
	apiErr, ok := err.(*APIError)
	if !ok {
		t.Fatalf("expected *APIError, got %T", err)
	}
	if apiErr.StatusCode != 400 {
		t.Errorf("status = %d, want 400", apiErr.StatusCode)
	}
	if apiErr.Retryable() {
		t.Error("400 should not be retryable")
	}
}

func TestOpenAI_Complete_RetryableError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
		w.Write([]byte(`{"error":{"type":"rate_limit","message":"slow down"}}`))
	}))
	defer srv.Close()

	o := &OpenAI{
		apiKey:    "test-key",
		model:     "gpt-4",
		url:       srv.URL,
		maxTokens: 1024,
		client:    srv.Client(),
	}

	_, err := o.Complete(context.Background(), &Request{
		Messages: []Message{NewTextMessage("user", "hi")},
	})
	if err == nil {
		t.Fatal("expected error for 429 response")
	}
	apiErr, ok := err.(*APIError)
	if !ok {
		t.Fatalf("expected *APIError, got %T", err)
	}
	if !apiErr.Retryable() {
		t.Error("429 should be retryable")
	}
}

func TestOpenAI_Complete_SendError(t *testing.T) {
	// Use a server that immediately closes the connection to trigger send error.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hj, ok := w.(http.Hijacker)
		if !ok {
			t.Error("server does not support hijacking")
			return
		}
		conn, _, _ := hj.Hijack()
		conn.Close()
	}))
	defer srv.Close()

	o := &OpenAI{
		apiKey:    "test-key",
		model:     "gpt-4",
		url:       srv.URL,
		maxTokens: 1024,
		client:    srv.Client(),
	}

	_, err := o.Complete(context.Background(), &Request{
		Messages: []Message{NewTextMessage("user", "hi")},
	})
	if err == nil {
		t.Fatal("expected error when connection is closed")
	}
}

func TestOpenAI_Complete_InvalidJSONResponse(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte("not valid json"))
	}))
	defer srv.Close()

	o := &OpenAI{
		apiKey:    "test-key",
		model:     "gpt-4",
		url:       srv.URL,
		maxTokens: 1024,
		client:    srv.Client(),
	}

	_, err := o.Complete(context.Background(), &Request{
		Messages: []Message{NewTextMessage("user", "hi")},
	})
	if err == nil {
		t.Fatal("expected error for invalid JSON response")
	}
}

func TestOpenAI_Complete_APILevelError(t *testing.T) {
	// A 200 response that contains an error field in the body.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		resp := openaiResponse{}
		resp.Error = &struct {
			Message string `json:"message"`
			Type    string `json:"type"`
		}{Message: "model overloaded", Type: "server_error"}
		json.NewEncoder(w).Encode(resp)
	}))
	defer srv.Close()

	o := &OpenAI{
		apiKey:    "test-key",
		model:     "gpt-4",
		url:       srv.URL,
		maxTokens: 1024,
		client:    srv.Client(),
	}

	_, err := o.Complete(context.Background(), &Request{
		Messages: []Message{NewTextMessage("user", "hi")},
	})
	if err == nil {
		t.Fatal("expected error for API-level error in 200 response")
	}
}

func TestOpenAI_Complete_NoChoices(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(openaiResponse{Choices: nil})
	}))
	defer srv.Close()

	o := &OpenAI{
		apiKey:    "test-key",
		model:     "gpt-4",
		url:       srv.URL,
		maxTokens: 1024,
		client:    srv.Client(),
	}

	_, err := o.Complete(context.Background(), &Request{
		Messages: []Message{NewTextMessage("user", "hi")},
	})
	if err == nil {
		t.Fatal("expected error for empty choices")
	}
}

func TestOpenAI_Complete_MaxTokensOverride(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		var req openaiRequest
		json.Unmarshal(body, &req)
		if req.MaxTokens != 256 {
			t.Errorf("max_tokens = %d, want 256", req.MaxTokens)
		}
		resp := openaiResponse{
			Choices: []openaiChoice{{
				Message:      openaiMsg{Role: "assistant", Content: "ok"},
				FinishReason: "stop",
			}},
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer srv.Close()

	o := &OpenAI{
		apiKey:    "test-key",
		model:     "gpt-4",
		url:       srv.URL,
		maxTokens: 1024,
		client:    srv.Client(),
	}

	_, err := o.Complete(context.Background(), &Request{
		Messages:  []Message{NewTextMessage("user", "hi")},
		MaxTokens: 256,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestConvertToOpenAIMsgs_User(t *testing.T) {
	m := NewTextMessage("user", "hello")
	msgs := convertToOpenAIMsgs(m)
	if len(msgs) != 1 {
		t.Fatalf("got %d messages, want 1", len(msgs))
	}
	if msgs[0].Role != "user" || msgs[0].Content != "hello" {
		t.Errorf("unexpected message: %+v", msgs[0])
	}
}

func TestConvertToOpenAIMsgs_AssistantWithToolCalls(t *testing.T) {
	m := Message{
		Role: RoleAssistant,
		Content: []ContentBlock{
			{Type: BlockText, Text: "Let me read that."},
			{Type: BlockToolUse, ID: "call_1", Name: "read_file", Input: json.RawMessage(`{"path":"x.txt"}`)},
		},
	}
	msgs := convertToOpenAIMsgs(m)
	if len(msgs) != 1 {
		t.Fatalf("got %d messages, want 1", len(msgs))
	}
	if msgs[0].Role != "assistant" {
		t.Errorf("role = %q, want assistant", msgs[0].Role)
	}
	if msgs[0].Content != "Let me read that." {
		t.Errorf("content = %q", msgs[0].Content)
	}
	if len(msgs[0].ToolCalls) != 1 {
		t.Fatalf("tool_calls count = %d, want 1", len(msgs[0].ToolCalls))
	}
	tc := msgs[0].ToolCalls[0]
	if tc.ID != "call_1" || tc.Function.Name != "read_file" {
		t.Errorf("unexpected tool call: %+v", tc)
	}
}

func TestConvertToOpenAIMsgs_ToolResults(t *testing.T) {
	m := NewToolResultMessage("call_1", "file contents", false)
	msgs := convertToOpenAIMsgs(m)
	if len(msgs) != 1 {
		t.Fatalf("got %d messages, want 1", len(msgs))
	}
	if msgs[0].Role != "tool" {
		t.Errorf("role = %q, want tool", msgs[0].Role)
	}
	if msgs[0].ToolCallID != "call_1" {
		t.Errorf("tool_call_id = %q, want call_1", msgs[0].ToolCallID)
	}
	if msgs[0].Content != "file contents" {
		t.Errorf("content = %q", msgs[0].Content)
	}
}

func TestConvertToOpenAIMsgs_ToolResultsWithText(t *testing.T) {
	// A consolidated message with tool_result blocks AND a text block (budget reminder).
	m := Message{
		Role: RoleUser,
		Content: []ContentBlock{
			{Type: BlockToolResult, ID: "call_1", Content: "file contents"},
			{Type: BlockToolResult, ID: "call_2", Content: "command output"},
			{Type: BlockText, Text: "[3 rounds remaining.]"},
		},
	}
	msgs := convertToOpenAIMsgs(m)
	// Should produce 2 tool messages + 1 user text message.
	if len(msgs) != 3 {
		t.Fatalf("got %d messages, want 3", len(msgs))
	}
	if msgs[0].Role != "tool" || msgs[0].ToolCallID != "call_1" {
		t.Errorf("msgs[0] = %+v, want tool with call_1", msgs[0])
	}
	if msgs[1].Role != "tool" || msgs[1].ToolCallID != "call_2" {
		t.Errorf("msgs[1] = %+v, want tool with call_2", msgs[1])
	}
	if msgs[2].Role != "user" || msgs[2].Content != "[3 rounds remaining.]" {
		t.Errorf("msgs[2] = %+v, want user text with budget message", msgs[2])
	}
}

func TestMapOpenAIStopReason(t *testing.T) {
	tests := []struct {
		input string
		want  StopReason
	}{
		{"stop", StopEndTurn},
		{"tool_calls", StopToolUse},
		{"length", StopMaxTokens},
		{"unknown", StopReason("unknown")},
	}
	for _, tt := range tests {
		got := mapOpenAIStopReason(tt.input)
		if got != tt.want {
			t.Errorf("mapOpenAIStopReason(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}
