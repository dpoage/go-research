package llm

import (
	"context"
	"errors"
	"testing"
	"time"
)

// mockProvider is a test helper that returns responses from a list.
type mockProvider struct {
	results []mockResult
	calls   int
}

type mockResult struct {
	resp *Response
	err  error
}

func (m *mockProvider) Complete(_ context.Context, _ *Request) (*Response, error) {
	if m.calls >= len(m.results) {
		return nil, errors.New("no more mock results")
	}
	r := m.results[m.calls]
	m.calls++
	return r.resp, r.err
}

func TestRetryProvider_Success_NoRetry(t *testing.T) {
	want := &Response{StopReason: StopEndTurn}
	mp := &mockProvider{results: []mockResult{
		{resp: want},
	}}

	rp := NewRetryProvider(mp, 3, time.Millisecond)
	got, err := rp.Complete(context.Background(), &Request{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != want {
		t.Errorf("got %v, want %v", got, want)
	}
	if mp.calls != 1 {
		t.Errorf("calls = %d, want 1", mp.calls)
	}
}

func TestRetryProvider_TransientThenSuccess(t *testing.T) {
	want := &Response{StopReason: StopEndTurn}
	mp := &mockProvider{results: []mockResult{
		{err: &APIError{StatusCode: 429, Body: "rate limit"}},
		{err: &APIError{StatusCode: 502, Body: "bad gateway"}},
		{resp: want},
	}}

	rp := NewRetryProvider(mp, 3, time.Millisecond)
	got, err := rp.Complete(context.Background(), &Request{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != want {
		t.Errorf("got %v, want %v", got, want)
	}
	if mp.calls != 3 {
		t.Errorf("calls = %d, want 3", mp.calls)
	}
}

func TestRetryProvider_AllRetriesExhausted(t *testing.T) {
	mp := &mockProvider{results: []mockResult{
		{err: &APIError{StatusCode: 500, Body: "error 1"}},
		{err: &APIError{StatusCode: 500, Body: "error 2"}},
		{err: &APIError{StatusCode: 500, Body: "error 3"}},
		{err: &APIError{StatusCode: 500, Body: "error 4"}},
	}}

	rp := NewRetryProvider(mp, 3, time.Millisecond)
	_, err := rp.Complete(context.Background(), &Request{})
	if err == nil {
		t.Fatal("expected error after all retries exhausted")
	}
	if mp.calls != 4 { // 1 initial + 3 retries
		t.Errorf("calls = %d, want 4", mp.calls)
	}
	apiErr, ok := err.(*APIError)
	if !ok {
		t.Fatalf("expected *APIError, got %T", err)
	}
	if apiErr.Body != "error 4" {
		t.Errorf("last error body = %q, want %q", apiErr.Body, "error 4")
	}
}

func TestRetryProvider_NonRetryableError(t *testing.T) {
	mp := &mockProvider{results: []mockResult{
		{err: &APIError{StatusCode: 400, Body: "bad request"}},
	}}

	rp := NewRetryProvider(mp, 3, time.Millisecond)
	_, err := rp.Complete(context.Background(), &Request{})
	if err == nil {
		t.Fatal("expected error for non-retryable error")
	}
	if mp.calls != 1 {
		t.Errorf("calls = %d, want 1 (should not retry)", mp.calls)
	}
}

func TestRetryProvider_NonAPIError(t *testing.T) {
	mp := &mockProvider{results: []mockResult{
		{err: errors.New("connection refused")},
	}}

	rp := NewRetryProvider(mp, 3, time.Millisecond)
	_, err := rp.Complete(context.Background(), &Request{})
	if err == nil {
		t.Fatal("expected error")
	}
	if mp.calls != 1 {
		t.Errorf("calls = %d, want 1 (plain errors are not retryable)", mp.calls)
	}
}

func TestRetryProvider_ContextCancellation(t *testing.T) {
	mp := &mockProvider{results: []mockResult{
		{err: &APIError{StatusCode: 429, Body: "rate limit"}},
		{err: &APIError{StatusCode: 429, Body: "rate limit"}},
		{err: &APIError{StatusCode: 429, Body: "rate limit"}},
		{err: &APIError{StatusCode: 429, Body: "rate limit"}},
	}}

	ctx, cancel := context.WithCancel(context.Background())
	// Use a longer base delay so the context cancel happens during the wait.
	rp := NewRetryProvider(mp, 3, 10*time.Second)

	done := make(chan error, 1)
	go func() {
		_, err := rp.Complete(ctx, &Request{})
		done <- err
	}()

	// Give it time to fail first attempt and start waiting.
	time.Sleep(50 * time.Millisecond)
	cancel()

	err := <-done
	if !errors.Is(err, context.Canceled) {
		t.Errorf("expected context.Canceled, got %v", err)
	}
}

func TestRetryProvider_RetryableStatusCodes(t *testing.T) {
	retryable := []int{429, 500, 502, 503, 529}
	for _, code := range retryable {
		err := &APIError{StatusCode: code, Body: "err"}
		if !err.Retryable() {
			t.Errorf("status %d should be retryable", code)
		}
	}

	nonRetryable := []int{400, 401, 403, 404, 422}
	for _, code := range nonRetryable {
		err := &APIError{StatusCode: code, Body: "err"}
		if err.Retryable() {
			t.Errorf("status %d should not be retryable", code)
		}
	}
}
