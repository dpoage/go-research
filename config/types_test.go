package config

import (
	"testing"

	"gopkg.in/yaml.v3"
)

func TestDirection_UnmarshalYAML(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		want    Direction
		wantErr bool
	}{
		{name: "minimize", input: `"minimize"`, want: DirectionMinimize},
		{name: "maximize", input: `"maximize"`, want: DirectionMaximize},
		{name: "invalid", input: `"sideways"`, wantErr: true},
		{name: "empty", input: `""`, wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var d Direction
			err := yaml.Unmarshal([]byte(tt.input), &d)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("expected error for %s", tt.input)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if d != tt.want {
				t.Errorf("got %q, want %q", d, tt.want)
			}
		})
	}
}

func TestDirection_String(t *testing.T) {
	if DirectionMinimize.String() != "minimize" {
		t.Errorf("got %q", DirectionMinimize.String())
	}
	if DirectionMaximize.String() != "maximize" {
		t.Errorf("got %q", DirectionMaximize.String())
	}
}

func TestBackend_UnmarshalYAML(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		want    Backend
		wantErr bool
	}{
		{name: "anthropic", input: `"anthropic"`, want: BackendAnthropic},
		{name: "openai", input: `"openai"`, want: BackendOpenAI},
		{name: "invalid", input: `"gemini"`, wantErr: true},
		{name: "empty", input: `""`, wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var b Backend
			err := yaml.Unmarshal([]byte(tt.input), &b)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("expected error for %s", tt.input)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if b != tt.want {
				t.Errorf("got %q, want %q", b, tt.want)
			}
		})
	}
}

func TestBackend_String(t *testing.T) {
	if BackendAnthropic.String() != "anthropic" {
		t.Errorf("got %q", BackendAnthropic.String())
	}
	if BackendOpenAI.String() != "openai" {
		t.Errorf("got %q", BackendOpenAI.String())
	}
}

func TestSource_UnmarshalYAML(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		want    Source
		wantErr bool
	}{
		{name: "stdout explicit", input: `"stdout"`, want: Source{Kind: "stdout"}},
		{name: "empty defaults stdout", input: `""`, want: Source{Kind: "stdout"}},
		{name: "file with path", input: `"file:results.json"`, want: Source{Kind: "file", Path: "results.json"}},
		{name: "file missing path", input: `"file:"`, wantErr: true},
		{name: "unknown scheme", input: `"ftp:somewhere"`, wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var s Source
			err := yaml.Unmarshal([]byte(tt.input), &s)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("expected error for %s", tt.input)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if s != tt.want {
				t.Errorf("got %+v, want %+v", s, tt.want)
			}
		})
	}
}

func TestSource_IsFile(t *testing.T) {
	if SourceStdout.IsFile() {
		t.Error("stdout should not be file")
	}
	s := Source{Kind: "file", Path: "out.json"}
	if !s.IsFile() {
		t.Error("file source should be file")
	}
}

func TestSource_String(t *testing.T) {
	if SourceStdout.String() != "stdout" {
		t.Errorf("got %q", SourceStdout.String())
	}
	s := Source{Kind: "file", Path: "out.json"}
	if s.String() != "file:out.json" {
		t.Errorf("got %q", s.String())
	}
}

func TestSource_MarshalYAML(t *testing.T) {
	tests := []struct {
		name string
		src  Source
		want string
	}{
		{name: "stdout", src: SourceStdout, want: "stdout\n"},
		{name: "file", src: Source{Kind: "file", Path: "out.json"}, want: "file:out.json\n"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			data, err := yaml.Marshal(tt.src)
			if err != nil {
				t.Fatal(err)
			}
			if string(data) != tt.want {
				t.Errorf("got %q, want %q", data, tt.want)
			}
		})
	}
}
