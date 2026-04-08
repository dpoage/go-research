package cmd

import (
	"context"
	"testing"
)

func TestRun_NoArgs(t *testing.T) {
	code := Run(context.Background(), nil)
	if code != 0 {
		t.Errorf("exit code = %d, want 0", code)
	}
}

func TestRun_Help(t *testing.T) {
	for _, arg := range []string{"help", "--help", "-h"} {
		code := Run(context.Background(), []string{arg})
		if code != 0 {
			t.Errorf("'%s' exit code = %d, want 0", arg, code)
		}
	}
}

func TestRun_Version(t *testing.T) {
	code := Run(context.Background(), []string{"version"})
	if code != 0 {
		t.Errorf("exit code = %d, want 0", code)
	}
}

func TestRun_UnknownCommand(t *testing.T) {
	code := Run(context.Background(), []string{"nonexistent"})
	if code != 1 {
		t.Errorf("exit code = %d, want 1", code)
	}
}

func TestParseGlobalFlags(t *testing.T) {
	gf, remaining := parseGlobalFlags([]string{"--config", "custom.yaml", "--quiet", "run", "--foo"})

	if gf.config != "custom.yaml" {
		t.Errorf("config = %q, want %q", gf.config, "custom.yaml")
	}
	if !gf.quiet {
		t.Error("quiet = false, want true")
	}
	if len(remaining) != 2 || remaining[0] != "run" {
		t.Errorf("remaining = %v, want [run --foo]", remaining)
	}
}
