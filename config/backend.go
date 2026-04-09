package config

import (
	"fmt"

	"gopkg.in/yaml.v3"
)

// Backend identifies the LLM provider.
type Backend string

const (
	BackendAnthropic Backend = "anthropic"
	BackendOpenAI    Backend = "openai"
)

// UnmarshalYAML rejects any value other than "anthropic" or "openai".
func (b *Backend) UnmarshalYAML(value *yaml.Node) error {
	var s string
	if err := value.Decode(&s); err != nil {
		return fmt.Errorf("backend: %w", err)
	}
	switch Backend(s) {
	case BackendAnthropic, BackendOpenAI:
		*b = Backend(s)
		return nil
	default:
		return fmt.Errorf("backend must be %q or %q, got %q", BackendAnthropic, BackendOpenAI, s)
	}
}

// String returns the backend as a string.
func (b Backend) String() string {
	return string(b)
}
