package llm

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// LoggingProvider wraps a Provider and writes every request/response
// exchange as JSONL to a file for prompt diagnostics.
type LoggingProvider struct {
	inner      Provider
	path       string
	mu         sync.Mutex
	seq        int
	lastSystem string
}

// NewLoggingProvider creates a provider that logs all exchanges to a JSONL
// file in the given directory. The file is named with a timestamp.
func NewLoggingProvider(inner Provider, dir string) (*LoggingProvider, error) {
	if err := os.MkdirAll(dir, 0755); err != nil {
		return nil, fmt.Errorf("create debug dir: %w", err)
	}
	name := fmt.Sprintf("llm-%s.jsonl", time.Now().UTC().Format("20060102-150405"))
	return &LoggingProvider{
		inner: inner,
		path:  filepath.Join(dir, name),
	}, nil
}

// Path returns the log file path.
func (l *LoggingProvider) Path() string {
	return l.path
}

type logEntry struct {
	Seq        int          `json:"seq"`
	Timestamp  string       `json:"ts"`
	DurationMs int64        `json:"duration_ms"`
	Request    logRequest   `json:"request"`
	Response   *logResponse `json:"response,omitempty"`
	Error      string       `json:"error,omitempty"`
}

type logRequest struct {
	System    string    `json:"system,omitempty"`
	Messages  []Message `json:"messages"`
	Tools     []string  `json:"tools,omitempty"`
	MaxTokens int       `json:"max_tokens"`
}

type logResponse struct {
	Content    []ContentBlock `json:"content"`
	StopReason StopReason     `json:"stop_reason"`
	Usage      Usage          `json:"usage"`
}

func (l *LoggingProvider) Complete(ctx context.Context, req *Request) (*Response, error) {
	l.mu.Lock()
	l.seq++
	seq := l.seq
	// Only include system prompt when it changes from the previous call.
	system := ""
	if req.System != l.lastSystem {
		system = req.System
		l.lastSystem = req.System
	}
	l.mu.Unlock()

	start := time.Now()
	resp, err := l.inner.Complete(ctx, req)
	elapsed := time.Since(start)

	var toolNames []string
	for _, t := range req.Tools {
		toolNames = append(toolNames, t.Name)
	}

	entry := logEntry{
		Seq:        seq,
		Timestamp:  start.UTC().Format(time.RFC3339),
		DurationMs: elapsed.Milliseconds(),
		Request: logRequest{
			System:    system,
			Messages:  req.Messages,
			Tools:     toolNames,
			MaxTokens: req.MaxTokens,
		},
	}

	if err != nil {
		entry.Error = err.Error()
	}
	if resp != nil {
		entry.Response = &logResponse{
			Content:    resp.Content,
			StopReason: resp.StopReason,
			Usage:      resp.Usage,
		}
	}

	l.append(entry)
	return resp, err
}

func (l *LoggingProvider) append(entry logEntry) {
	data, err := json.Marshal(entry)
	if err != nil {
		fmt.Fprintf(os.Stderr, "debug log: marshal failed: %v\n", err)
		return
	}
	data = append(data, '\n')

	l.mu.Lock()
	defer l.mu.Unlock()

	f, err := os.OpenFile(l.path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		fmt.Fprintf(os.Stderr, "debug log: open failed: %v\n", err)
		return
	}
	defer f.Close()

	if _, err := f.Write(data); err != nil {
		fmt.Fprintf(os.Stderr, "debug log: write failed: %v\n", err)
	}
}
