package llm

import (
	"net/http"
	"testing"
	"time"
)

func TestNewHTTPClient(t *testing.T) {
	c := newHTTPClient()
	if c == nil {
		t.Fatal("newHTTPClient returned nil")
	}
	if c.Timeout != httpTimeout {
		t.Errorf("Timeout = %v, want %v", c.Timeout, httpTimeout)
	}
	transport, ok := c.Transport.(*http.Transport)
	if !ok {
		t.Fatalf("Transport is %T, want *http.Transport", c.Transport)
	}
	if transport.IdleConnTimeout != httpIdleConnTimeout {
		t.Errorf("IdleConnTimeout = %v, want %v", transport.IdleConnTimeout, httpIdleConnTimeout)
	}
	if transport.TLSHandshakeTimeout != httpTLSHandshakeTimeout {
		t.Errorf("TLSHandshakeTimeout = %v, want %v", transport.TLSHandshakeTimeout, httpTLSHandshakeTimeout)
	}
}

func TestResolveAPIKey(t *testing.T) {
	tests := []struct {
		name        string
		keyEnv      string
		keyVal      string
		fallbackEnv string
		fallbackVal string
		wantKey     string
		wantErr     bool
	}{
		{
			name:    "primary env set",
			keyEnv:  "TEST_LLM_PRIMARY_KEY",
			keyVal:  "primary-secret",
			wantKey: "primary-secret",
		},
		{
			name:        "primary env not set falls back to fallback",
			keyEnv:      "",
			fallbackEnv: "TEST_LLM_FALLBACK_KEY",
			fallbackVal: "fallback-secret",
			wantKey:     "fallback-secret",
		},
		{
			name:    "primary env name given but var not set returns error",
			keyEnv:  "TEST_LLM_MISSING_KEY",
			keyVal:  "",
			wantErr: true,
		},
		{
			name:        "both envs empty returns error",
			keyEnv:      "",
			fallbackEnv: "TEST_LLM_MISSING_FALLBACK",
			fallbackVal: "",
			wantErr:     true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.keyEnv != "" && tt.keyVal != "" {
				t.Setenv(tt.keyEnv, tt.keyVal)
			}
			if tt.fallbackEnv != "" && tt.fallbackVal != "" {
				t.Setenv(tt.fallbackEnv, tt.fallbackVal)
			}

			got, err := resolveAPIKey(tt.keyEnv, tt.fallbackEnv)
			if tt.wantErr {
				if err == nil {
					t.Fatal("expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tt.wantKey {
				t.Errorf("key = %q, want %q", got, tt.wantKey)
			}
		})
	}
}

func TestHTTPTimeout(t *testing.T) {
	if httpTimeout != 5*time.Minute {
		t.Errorf("httpTimeout = %v, want 5m", httpTimeout)
	}
}
