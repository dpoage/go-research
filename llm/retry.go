package llm

import (
	"context"
	"errors"
	"math/rand/v2"
	"time"
)

const maxBackoff = 30 * time.Second

// RetryProvider wraps a Provider with exponential backoff retry for transient errors.
type RetryProvider struct {
	inner      Provider
	maxRetries int
	baseDelay  time.Duration
}

// NewRetryProvider wraps inner with retry logic. maxRetries is the number of
// retries after the first attempt (so total attempts = maxRetries + 1).
func NewRetryProvider(inner Provider, maxRetries int, baseDelay time.Duration) *RetryProvider {
	return &RetryProvider{
		inner:      inner,
		maxRetries: maxRetries,
		baseDelay:  baseDelay,
	}
}

func (r *RetryProvider) Complete(ctx context.Context, req *Request) (*Response, error) {
	var lastErr error
	for attempt := 0; attempt <= r.maxRetries; attempt++ {
		resp, err := r.inner.Complete(ctx, req)
		if err == nil {
			return resp, nil
		}
		lastErr = err

		// Only retry if the error is retryable.
		if !isRetryable(err) {
			return nil, err
		}

		// Don't sleep after the last attempt.
		if attempt == r.maxRetries {
			break
		}

		delay := r.backoff(attempt)
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(delay):
		}
	}
	return nil, lastErr
}

// backoff returns the delay for the given attempt using exponential backoff with jitter.
func (r *RetryProvider) backoff(attempt int) time.Duration {
	delay := r.baseDelay << uint(attempt)
	if delay <= 0 || delay > maxBackoff {
		delay = maxBackoff
	}
	jitter := time.Duration(rand.Int64N(int64(delay)))
	return delay + jitter
}

// isRetryable checks if any error in the chain implements Retryable() and returns true.
func isRetryable(err error) bool {
	var re interface{ Retryable() bool }
	if errors.As(err, &re) {
		return re.Retryable()
	}
	return false
}
