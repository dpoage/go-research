package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"

	"github.com/dpoage/go-research/config"
)

const defaultOpenAIURL = "https://api.openai.com/v1/chat/completions"

// OpenAI implements Provider using the OpenAI Chat Completions API.
type OpenAI struct {
	apiKey    string
	model     string
	url       string
	maxTokens int
	client    *http.Client
}

// NewOpenAI creates an OpenAI provider from the given config.
func NewOpenAI(cfg config.ProviderConfig) (*OpenAI, error) {
	apiKey, err := resolveAPIKey(cfg.APIKeyEnv, "OPENAI_API_KEY")
	if err != nil {
		return nil, err
	}

	url := defaultOpenAIURL
	if cfg.URL != "" {
		url = cfg.URL
	}

	return &OpenAI{
		apiKey:    apiKey,
		model:     cfg.Model,
		url:       url,
		maxTokens: cfg.MaxTokens,
		client:    newHTTPClient(),
	}, nil
}

// Wire format types for the OpenAI Chat Completions API.

type openaiRequest struct {
	Model     string       `json:"model"`
	Messages  []openaiMsg  `json:"messages"`
	Tools     []openaiTool `json:"tools,omitempty"`
	MaxTokens int          `json:"max_tokens,omitempty"`
}

type openaiMsg struct {
	Role       string           `json:"role"`
	Content    string           `json:"content,omitempty"`
	ToolCalls  []openaiToolCall `json:"tool_calls,omitempty"`
	ToolCallID string           `json:"tool_call_id,omitempty"`
}

type openaiTool struct {
	Type     string             `json:"type"`
	Function openaiToolFunction `json:"function"`
}

type openaiToolFunction struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	Parameters  json.RawMessage `json:"parameters"`
}

type openaiToolCall struct {
	ID       string                 `json:"id"`
	Type     string                 `json:"type"`
	Function openaiToolCallFunction `json:"function"`
}

type openaiToolCallFunction struct {
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}

type openaiResponse struct {
	Choices []openaiChoice `json:"choices"`
	Usage   struct {
		PromptTokens     int `json:"prompt_tokens"`
		CompletionTokens int `json:"completion_tokens"`
	} `json:"usage"`
	Error *struct {
		Message string `json:"message"`
		Type    string `json:"type"`
	} `json:"error"`
}

type openaiChoice struct {
	Message      openaiMsg `json:"message"`
	FinishReason string    `json:"finish_reason"`
}

func (o *OpenAI) Complete(ctx context.Context, req *Request) (*Response, error) {
	maxTokens := o.maxTokens
	if req.MaxTokens > 0 {
		maxTokens = req.MaxTokens
	}

	or := openaiRequest{
		Model:     o.model,
		MaxTokens: maxTokens,
	}

	// System message goes first in the messages array.
	if req.System != "" {
		or.Messages = append(or.Messages, openaiMsg{
			Role:    "system",
			Content: req.System,
		})
	}

	// Convert our generic messages to OpenAI wire format.
	for _, m := range req.Messages {
		msgs := convertToOpenAIMsgs(m)
		or.Messages = append(or.Messages, msgs...)
	}

	// Convert tools.
	for _, t := range req.Tools {
		or.Tools = append(or.Tools, openaiTool{
			Type: "function",
			Function: openaiToolFunction{
				Name:        t.Name,
				Description: t.Description,
				Parameters:  t.InputSchema,
			},
		})
	}

	body, err := json.Marshal(or)
	if err != nil {
		return nil, fmt.Errorf("marshal request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, "POST", o.url, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", "Bearer "+o.apiKey)

	httpResp, err := o.client.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("send request: %w", err)
	}
	defer httpResp.Body.Close()

	respBody, err := io.ReadAll(httpResp.Body)
	if err != nil {
		return nil, fmt.Errorf("read response: %w", err)
	}

	if httpResp.StatusCode != http.StatusOK {
		return nil, &APIError{
			StatusCode: httpResp.StatusCode,
			Body:       string(respBody),
		}
	}

	var or2 openaiResponse
	if err := json.Unmarshal(respBody, &or2); err != nil {
		return nil, fmt.Errorf("unmarshal response: %w", err)
	}

	if or2.Error != nil {
		return nil, fmt.Errorf("openai error: %s: %s", or2.Error.Type, or2.Error.Message)
	}

	if len(or2.Choices) == 0 {
		return nil, fmt.Errorf("openai: no choices in response")
	}

	choice := or2.Choices[0]
	resp := &Response{
		StopReason: mapOpenAIStopReason(choice.FinishReason),
		Usage: Usage{
			InputTokens:  or2.Usage.PromptTokens,
			OutputTokens: or2.Usage.CompletionTokens,
		},
	}

	// Convert content.
	if choice.Message.Content != "" {
		resp.Content = append(resp.Content, ContentBlock{
			Type: BlockText,
			Text: choice.Message.Content,
		})
	}

	// Convert tool calls.
	for _, tc := range choice.Message.ToolCalls {
		resp.Content = append(resp.Content, ContentBlock{
			Type:  BlockToolUse,
			ID:    tc.ID,
			Name:  tc.Function.Name,
			Input: json.RawMessage(tc.Function.Arguments),
		})
	}

	return resp, nil
}

// convertToOpenAIMsgs converts a generic Message to one or more OpenAI wire messages.
// A single generic message may contain mixed content (text + tool_use, or tool_results),
// which need different representations in OpenAI's format.
func convertToOpenAIMsgs(m Message) []openaiMsg {
	// Check if this message has tool_result blocks — each becomes a separate "tool" message.
	var toolResults []openaiMsg
	var textParts string
	var toolCalls []openaiToolCall

	for _, b := range m.Content {
		switch b.Type {
		case BlockText:
			textParts += b.Text
		case BlockToolUse:
			toolCalls = append(toolCalls, openaiToolCall{
				ID:   b.ID,
				Type: "function",
				Function: openaiToolCallFunction{
					Name:      b.Name,
					Arguments: string(b.Input),
				},
			})
		case BlockToolResult:
			toolResults = append(toolResults, openaiMsg{
				Role:       "tool",
				Content:    b.Content,
				ToolCallID: b.ID,
			})
		}
	}

	var msgs []openaiMsg

	// Assistant message with text and/or tool_calls.
	if m.Role == RoleAssistant {
		msg := openaiMsg{
			Role:      "assistant",
			Content:   textParts,
			ToolCalls: toolCalls,
		}
		msgs = append(msgs, msg)
	} else if len(toolResults) > 0 {
		// Tool result messages (role "user" in our generic format, role "tool" in OpenAI).
		msgs = append(msgs, toolResults...)
		// If the same message also had text blocks (e.g. budget reminders),
		// emit them as a separate user message.
		if textParts != "" {
			msgs = append(msgs, openaiMsg{Role: "user", Content: textParts})
		}
	} else {
		// Plain user message.
		msgs = append(msgs, openaiMsg{
			Role:    m.Role,
			Content: textParts,
		})
	}

	return msgs
}

func mapOpenAIStopReason(reason string) StopReason {
	switch reason {
	case "stop":
		return StopEndTurn
	case "tool_calls":
		return StopToolUse
	case "length":
		return StopMaxTokens
	default:
		return StopReason(reason)
	}
}
