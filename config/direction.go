package config

import (
	"fmt"

	"gopkg.in/yaml.v3"
)

// Direction indicates whether the metric should be minimized or maximized.
type Direction string

const (
	DirectionMinimize Direction = "minimize"
	DirectionMaximize Direction = "maximize"
)

// UnmarshalYAML rejects any value other than "minimize" or "maximize".
func (d *Direction) UnmarshalYAML(value *yaml.Node) error {
	var s string
	if err := value.Decode(&s); err != nil {
		return fmt.Errorf("direction: %w", err)
	}
	switch Direction(s) {
	case DirectionMinimize, DirectionMaximize:
		*d = Direction(s)
		return nil
	default:
		return fmt.Errorf("direction must be %q or %q, got %q", DirectionMinimize, DirectionMaximize, s)
	}
}

// String returns the direction as a string.
func (d Direction) String() string {
	return string(d)
}
