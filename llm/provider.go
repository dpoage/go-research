// Package llm defines the pluggable LLM provider interface and message types.
package llm

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/dpoage/go-research/config"
)

// Provider is the interface for LLM backends that support tool use.
type Provider interface {
	Complete(ctx context.Context, req *Request) (*Response, error)
}

// Request is a completion request sent to the provider.
type Request struct {
	System    string
	Messages  []Message
	Tools     []ToolDef
	MaxTokens int
}

// Message is a single message in a conversation.
type Message struct {
	Role    string         `json:"role"`
	Content []ContentBlock `json:"content"`
}

// ContentBlock is a union type: text, tool_use, or tool_result.
type ContentBlock struct {
	Type    string          `json:"type"`
	Text    string          `json:"text,omitempty"`
	ID      string          `json:"id,omitempty"`
	Name    string          `json:"name,omitempty"`
	Input   json.RawMessage `json:"input,omitempty"`
	Content string          `json:"content,omitempty"`
	IsError bool            `json:"is_error,omitempty"`
}

// ToolDef defines a tool the LLM can call.
type ToolDef struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	InputSchema json.RawMessage `json:"input_schema"`
}

// StopReason indicates why the model stopped generating.
type StopReason string

const (
	StopEndTurn   StopReason = "end_turn"
	StopToolUse   StopReason = "tool_use"
	StopMaxTokens StopReason = "max_tokens"
)

// Response is the model's response to a completion request.
type Response struct {
	Content    []ContentBlock
	StopReason StopReason
	Usage      Usage
}

// Usage tracks token consumption.
type Usage struct {
	InputTokens  int
	OutputTokens int
}

// ToolUseBlocks returns only the tool_use content blocks from a response.
func (r *Response) ToolUseBlocks() []ContentBlock {
	var blocks []ContentBlock
	for _, b := range r.Content {
		if b.Type == "tool_use" {
			blocks = append(blocks, b)
		}
	}
	return blocks
}

// TextContent returns the concatenated text from all text blocks.
func (r *Response) TextContent() string {
	var s string
	for _, b := range r.Content {
		if b.Type == "text" {
			s += b.Text
		}
	}
	return s
}

// NewProvider creates a Provider from the given config.
func NewProvider(cfg config.ProviderConfig) (Provider, error) {
	switch cfg.Backend {
	case "anthropic":
		return NewAnthropic(cfg)
	case "openai":
		return nil, fmt.Errorf("openai backend not yet implemented")
	default:
		return nil, fmt.Errorf("unknown provider backend: %q", cfg.Backend)
	}
}

// NewTextMessage creates a simple text message.
func NewTextMessage(role, text string) Message {
	return Message{
		Role: role,
		Content: []ContentBlock{{
			Type: "text",
			Text: text,
		}},
	}
}

// NewToolResultMessage creates a tool_result message.
func NewToolResultMessage(toolUseID, content string, isError bool) Message {
	return Message{
		Role: "user",
		Content: []ContentBlock{{
			Type:    "tool_result",
			ID:      toolUseID,
			Content: content,
			IsError: isError,
		}},
	}
}
