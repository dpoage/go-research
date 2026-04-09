package config

import (
	"testing"
	"time"

	"gopkg.in/yaml.v3"
)

func TestDuration_UnmarshalYAML(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		want    time.Duration
		wantErr bool
	}{
		{name: "minutes string", input: `"5m"`, want: 5 * time.Minute},
		{name: "seconds string", input: `"30s"`, want: 30 * time.Second},
		{name: "hours string", input: `"1h"`, want: time.Hour},
		{name: "complex string", input: `"1h30m"`, want: 90 * time.Minute},
		{name: "invalid duration string", input: `"notaduration"`, wantErr: true},
		// A mapping node causes Decode(&s) to fail, which triggers the integer
		// fallback path. Decode(&ns) also fails for a mapping, so an error is
		// returned that wraps the original string-decode error.
		{name: "mapping node causes decode error", input: `{foo: bar}`, wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var d Duration
			err := yaml.Unmarshal([]byte(tt.input), &d)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("expected error for input %s", tt.input)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if d.Duration != tt.want {
				t.Errorf("got %v, want %v", d.Duration, tt.want)
			}
		})
	}
}

func TestDuration_MarshalYAML(t *testing.T) {
	tests := []struct {
		name  string
		input time.Duration
		want  string
	}{
		{name: "5 minutes", input: 5 * time.Minute, want: "5m0s\n"},
		{name: "30 seconds", input: 30 * time.Second, want: "30s\n"},
		{name: "zero", input: 0, want: "0s\n"},
		{name: "1 hour", input: time.Hour, want: "1h0m0s\n"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			d := Duration{Duration: tt.input}
			data, err := yaml.Marshal(d)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if string(data) != tt.want {
				t.Errorf("got %q, want %q", data, tt.want)
			}
		})
	}
}
