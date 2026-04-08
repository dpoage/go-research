package config

import (
	"fmt"
	"time"

	"gopkg.in/yaml.v3"
)

// Duration wraps time.Duration to support YAML string unmarshaling (e.g. "5m", "30s").
type Duration struct {
	time.Duration
}

// UnmarshalYAML parses a duration string like "5m" or "30s".
func (d *Duration) UnmarshalYAML(value *yaml.Node) error {
	var s string
	if err := value.Decode(&s); err != nil {
		// Try as integer (nanoseconds).
		var ns int64
		if err2 := value.Decode(&ns); err2 != nil {
			return fmt.Errorf("cannot parse duration: %w", err)
		}
		d.Duration = time.Duration(ns)
		return nil
	}
	dur, err := time.ParseDuration(s)
	if err != nil {
		return fmt.Errorf("invalid duration %q: %w", s, err)
	}
	d.Duration = dur
	return nil
}

// MarshalYAML writes the duration as a human-readable string.
func (d Duration) MarshalYAML() (any, error) {
	return d.Duration.String(), nil
}
