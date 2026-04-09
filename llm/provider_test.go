package llm

import (
	"testing"

	"github.com/dpoage/go-research/config"
)

func TestResponse_ToolUseBlocks(t *testing.T) {
	r := &Response{
		Content: []ContentBlock{
			{Type: "text", Text: "I'll read the file."},
			{Type: "tool_use", ID: "tu_1", Name: "read_file", Input: []byte(`{"path":"main.go"}`)},
			{Type: "text", Text: "And write it."},
			{Type: "tool_use", ID: "tu_2", Name: "write_file", Input: []byte(`{"path":"main.go","content":"x"}`)},
		},
	}

	blocks := r.ToolUseBlocks()
	if len(blocks) != 2 {
		t.Fatalf("got %d tool_use blocks, want 2", len(blocks))
	}
	if blocks[0].Name != "read_file" {
		t.Errorf("blocks[0].Name = %q, want %q", blocks[0].Name, "read_file")
	}
	if blocks[1].Name != "write_file" {
		t.Errorf("blocks[1].Name = %q, want %q", blocks[1].Name, "write_file")
	}
}

func TestResponse_TextContent(t *testing.T) {
	r := &Response{
		Content: []ContentBlock{
			{Type: "text", Text: "Hello "},
			{Type: "tool_use", ID: "tu_1", Name: "read_file"},
			{Type: "text", Text: "world"},
		},
	}

	got := r.TextContent()
	if got != "Hello world" {
		t.Errorf("TextContent() = %q, want %q", got, "Hello world")
	}
}

func TestNewTextMessage(t *testing.T) {
	m := NewTextMessage("user", "hello")
	if m.Role != "user" {
		t.Errorf("Role = %q, want %q", m.Role, "user")
	}
	if len(m.Content) != 1 || m.Content[0].Type != "text" || m.Content[0].Text != "hello" {
		t.Errorf("unexpected content: %+v", m.Content)
	}
}

func TestNewToolResultMessage(t *testing.T) {
	m := NewToolResultMessage("tu_1", "file contents here", false)
	if m.Role != "user" {
		t.Errorf("Role = %q, want %q", m.Role, "user")
	}
	if len(m.Content) != 1 {
		t.Fatalf("expected 1 block, got %d", len(m.Content))
	}
	b := m.Content[0]
	if b.Type != "tool_result" || b.ID != "tu_1" || b.Content != "file contents here" || b.IsError {
		t.Errorf("unexpected block: %+v", b)
	}
}

func TestNewProvider_UnknownBackend(t *testing.T) {
	_, err := NewProvider(configProviderConfig(config.Backend("unknown"), "model"))
	if err == nil {
		t.Fatal("expected error for unknown backend")
	}
}

func TestNewProvider_Anthropic(t *testing.T) {
	t.Setenv("ANTHROPIC_API_KEY", "test-anthropic-key")

	p, err := NewProvider(configProviderConfig(config.BackendAnthropic, "claude-3"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if p == nil {
		t.Fatal("NewProvider returned nil")
	}
	// The returned provider must be a *RetryProvider wrapping the Anthropic backend.
	rp, ok := p.(*RetryProvider)
	if !ok {
		t.Fatalf("expected *RetryProvider, got %T", p)
	}
	if _, ok := rp.inner.(*Anthropic); !ok {
		t.Errorf("expected inner provider to be *Anthropic, got %T", rp.inner)
	}
}

func TestNewProvider_OpenAI(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", "test-openai-key")

	p, err := NewProvider(configProviderConfig(config.BackendOpenAI, "gpt-4"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if p == nil {
		t.Fatal("NewProvider returned nil")
	}
	rp, ok := p.(*RetryProvider)
	if !ok {
		t.Fatalf("expected *RetryProvider, got %T", p)
	}
	if _, ok := rp.inner.(*OpenAI); !ok {
		t.Errorf("expected inner provider to be *OpenAI, got %T", rp.inner)
	}
}

func TestNewProvider_AnthropicMissingKey(t *testing.T) {
	cfg := config.ProviderConfig{
		Backend:   config.BackendAnthropic,
		APIKeyEnv: "TEST_PROVIDER_ANTHROPIC_MISSING_KEY",
		Model:     "claude-3",
	}
	_, err := NewProvider(cfg)
	if err == nil {
		t.Fatal("expected error when anthropic API key is missing")
	}
}

func TestNewProvider_OpenAIMissingKey(t *testing.T) {
	cfg := config.ProviderConfig{
		Backend:   config.BackendOpenAI,
		APIKeyEnv: "TEST_PROVIDER_OPENAI_MISSING_KEY",
		Model:     "gpt-4",
	}
	_, err := NewProvider(cfg)
	if err == nil {
		t.Fatal("expected error when openai API key is missing")
	}
}
