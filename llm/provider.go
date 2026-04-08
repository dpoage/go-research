// Package llm defines the pluggable LLM provider interface and message types.
package llm

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/dpoage/go-research/config"
)

const (
	RoleUser      = "user"
	RoleAssistant = "assistant"

	BlockText       = "text"
	BlockToolUse    = "tool_use"
	BlockToolResult = "tool_result"
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

// ContentBlock is a union type discriminated by Type:
//   - "text":        Text is set
//   - "tool_use":    ID (call ID), Name, and Input are set
//   - "tool_result": ID (matching tool_use call ID), Content, and IsError are set
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
		if b.Type == BlockToolUse {
			blocks = append(blocks, b)
		}
	}
	return blocks
}

// TextContent returns the concatenated text from all text blocks.
func (r *Response) TextContent() string {
	var sb strings.Builder
	for _, b := range r.Content {
		if b.Type == BlockText {
			sb.WriteString(b.Text)
		}
	}
	return sb.String()
}

const (
	defaultMaxRetries = 3
	defaultBaseDelay  = 1 * time.Second
)

// NewProvider creates a Provider from the given config.
// The returned provider is wrapped with retry logic for transient errors.
func NewProvider(cfg config.ProviderConfig) (Provider, error) {
	var p Provider
	var err error

	switch cfg.Backend {
	case config.BackendAnthropic:
		p, err = NewAnthropic(cfg)
	case config.BackendOpenAI:
		p, err = NewOpenAI(cfg)
	default:
		return nil, fmt.Errorf("unknown provider backend: %q", cfg.Backend)
	}
	if err != nil {
		return nil, err
	}

	return NewRetryProvider(p, defaultMaxRetries, defaultBaseDelay), nil
}

// NewTextMessage creates a simple text message.
func NewTextMessage(role, text string) Message {
	return Message{
		Role: role,
		Content: []ContentBlock{{
			Type: BlockText,
			Text: text,
		}},
	}
}

// NewToolResultMessage creates a tool_result message.
func NewToolResultMessage(toolUseID, content string, isError bool) Message {
	return Message{
		Role: RoleUser,
		Content: []ContentBlock{{
			Type:    BlockToolResult,
			ID:      toolUseID,
			Content: content,
			IsError: isError,
		}},
	}
}
