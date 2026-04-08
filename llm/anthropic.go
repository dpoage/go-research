package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"time"

	"github.com/dpoage/go-research/config"
)

const (
	defaultAnthropicURL     = "https://api.anthropic.com/v1/messages"
	defaultAnthropicVersion = "2023-06-01"
)

// Anthropic implements Provider using the Anthropic Messages API.
type Anthropic struct {
	apiKey    string
	model     string
	url       string
	maxTokens int
	client    *http.Client
}

// NewAnthropic creates an Anthropic provider from the given config.
func NewAnthropic(cfg config.ProviderConfig) (*Anthropic, error) {
	apiKey := os.Getenv(cfg.APIKeyEnv)
	if apiKey == "" && cfg.APIKeyEnv != "" {
		return nil, fmt.Errorf("environment variable %s is not set", cfg.APIKeyEnv)
	}
	if apiKey == "" {
		// Fall back to the standard env var.
		apiKey = os.Getenv("ANTHROPIC_API_KEY")
	}
	if apiKey == "" {
		return nil, fmt.Errorf("no API key: set %s or ANTHROPIC_API_KEY", cfg.APIKeyEnv)
	}

	url := defaultAnthropicURL
	if cfg.URL != "" {
		url = cfg.URL
	}

	return &Anthropic{
		apiKey:    apiKey,
		model:     cfg.Model,
		url:       url,
		maxTokens: cfg.MaxTokens,
		client: &http.Client{
			Timeout: 5 * time.Minute,
			Transport: &http.Transport{
				IdleConnTimeout:     90 * time.Second,
				TLSHandshakeTimeout: 10 * time.Second,
			},
		},
	}, nil
}

// anthropicRequest is the wire format for the Anthropic Messages API.
type anthropicRequest struct {
	Model     string           `json:"model"`
	MaxTokens int              `json:"max_tokens"`
	System    string           `json:"system,omitempty"`
	Messages  []anthropicMsg   `json:"messages"`
	Tools     []anthropicTool  `json:"tools,omitempty"`
}

type anthropicMsg struct {
	Role    string          `json:"role"`
	Content json.RawMessage `json:"content"`
}

type anthropicTool struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	InputSchema json.RawMessage `json:"input_schema"`
}

// anthropicResponse is the wire format for the response.
type anthropicResponse struct {
	Content    []anthropicContentBlock `json:"content"`
	StopReason string                  `json:"stop_reason"`
	Usage      struct {
		InputTokens  int `json:"input_tokens"`
		OutputTokens int `json:"output_tokens"`
	} `json:"usage"`
	Error *struct {
		Type    string `json:"type"`
		Message string `json:"message"`
	} `json:"error"`
}

type anthropicContentBlock struct {
	Type  string          `json:"type"`
	Text  string          `json:"text,omitempty"`
	ID    string          `json:"id,omitempty"`
	Name  string          `json:"name,omitempty"`
	Input json.RawMessage `json:"input,omitempty"`
}

func (a *Anthropic) Complete(ctx context.Context, req *Request) (*Response, error) {
	maxTokens := a.maxTokens
	if req.MaxTokens > 0 {
		maxTokens = req.MaxTokens
	}

	ar := anthropicRequest{
		Model:     a.model,
		MaxTokens: maxTokens,
		System:    req.System,
	}

	for _, m := range req.Messages {
		raw, err := marshalContentBlocks(m.Content)
		if err != nil {
			return nil, fmt.Errorf("marshal message: %w", err)
		}
		ar.Messages = append(ar.Messages, anthropicMsg{
			Role:    m.Role,
			Content: raw,
		})
	}

	for _, t := range req.Tools {
		ar.Tools = append(ar.Tools, anthropicTool{
			Name:        t.Name,
			Description: t.Description,
			InputSchema: t.InputSchema,
		})
	}

	body, err := json.Marshal(ar)
	if err != nil {
		return nil, fmt.Errorf("marshal request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, "POST", a.url, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("x-api-key", a.apiKey)
	httpReq.Header.Set("anthropic-version", defaultAnthropicVersion)

	httpResp, err := a.client.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("send request: %w", err)
	}
	defer httpResp.Body.Close()

	respBody, err := io.ReadAll(httpResp.Body)
	if err != nil {
		return nil, fmt.Errorf("read response: %w", err)
	}

	if httpResp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("anthropic API error (status %d): %s", httpResp.StatusCode, respBody)
	}

	var ar2 anthropicResponse
	if err := json.Unmarshal(respBody, &ar2); err != nil {
		return nil, fmt.Errorf("unmarshal response: %w", err)
	}

	if ar2.Error != nil {
		return nil, fmt.Errorf("anthropic error: %s: %s", ar2.Error.Type, ar2.Error.Message)
	}

	resp := &Response{
		StopReason: StopReason(ar2.StopReason),
		Usage: Usage{
			InputTokens:  ar2.Usage.InputTokens,
			OutputTokens: ar2.Usage.OutputTokens,
		},
	}

	for _, b := range ar2.Content {
		resp.Content = append(resp.Content, ContentBlock{
			Type:  b.Type,
			Text:  b.Text,
			ID:    b.ID,
			Name:  b.Name,
			Input: b.Input,
		})
	}

	return resp, nil
}

// marshalContentBlocks serializes content blocks for the Anthropic wire format.
// The Anthropic API accepts both a plain string and an array of objects for content.
// We always send the array form for consistency.
func marshalContentBlocks(blocks []ContentBlock) (json.RawMessage, error) {
	type wireBlock struct {
		Type       string          `json:"type"`
		Text       string          `json:"text,omitempty"`
		ID         string          `json:"id,omitempty"`
		Name       string          `json:"name,omitempty"`
		Input      json.RawMessage `json:"input,omitempty"`
		ToolUseID  string          `json:"tool_use_id,omitempty"`
		Content    string          `json:"content,omitempty"`
		IsError    bool            `json:"is_error,omitempty"`
	}

	var out []wireBlock
	for _, b := range blocks {
		wb := wireBlock{
			Type: b.Type,
		}
		switch b.Type {
		case BlockText:
			wb.Text = b.Text
		case BlockToolUse:
			wb.ID = b.ID
			wb.Name = b.Name
			wb.Input = b.Input
		case BlockToolResult:
			wb.ToolUseID = b.ID
			wb.Content = b.Content
			wb.IsError = b.IsError
		}
		out = append(out, wb)
	}
	return json.Marshal(out)
}
