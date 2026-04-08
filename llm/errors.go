package llm

import "fmt"

// APIError represents an HTTP error from an LLM API.
type APIError struct {
	StatusCode int
	Body       string
}

func (e *APIError) Error() string {
	return fmt.Sprintf("API error (status %d): %s", e.StatusCode, e.Body)
}

// Retryable returns true for transient HTTP status codes (429, 500, 502, 503, 529).
func (e *APIError) Retryable() bool {
	switch e.StatusCode {
	case 429, 500, 502, 503, 529:
		return true
	default:
		return false
	}
}
