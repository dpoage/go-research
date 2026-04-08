package llm

import (
	"fmt"
	"net/http"
	"os"
	"time"
)

const (
	httpTimeout             = 5 * time.Minute
	httpIdleConnTimeout     = 90 * time.Second
	httpTLSHandshakeTimeout = 10 * time.Second
)

// newHTTPClient creates the shared HTTP client used by LLM providers.
func newHTTPClient() *http.Client {
	return &http.Client{
		Timeout: httpTimeout,
		Transport: &http.Transport{
			IdleConnTimeout:     httpIdleConnTimeout,
			TLSHandshakeTimeout: httpTLSHandshakeTimeout,
		},
	}
}

// resolveAPIKey loads an API key from the environment. It checks keyEnv first,
// then falls back to fallbackEnv. Returns an error if neither is set.
func resolveAPIKey(keyEnv, fallbackEnv string) (string, error) {
	apiKey := os.Getenv(keyEnv)
	if apiKey == "" && keyEnv != "" {
		return "", fmt.Errorf("environment variable %s is not set", keyEnv)
	}
	if apiKey == "" {
		apiKey = os.Getenv(fallbackEnv)
	}
	if apiKey == "" {
		return "", fmt.Errorf("no API key: set %s or %s", keyEnv, fallbackEnv)
	}
	return apiKey, nil
}
