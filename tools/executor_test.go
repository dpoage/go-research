package tools

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestExecutor_ReadFile(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "hello.txt")
	if err := os.WriteFile(target, []byte("hello world"), 0644); err != nil {
		t.Fatal(err)
	}

	sb, _ := NewSandbox(dir, []string{"hello.txt"})
	exec := NewExecutor(sb, 0)

	input, _ := json.Marshal(readFileInput{Path: target})
	result := exec.Dispatch(context.Background(), ToolReadFile, input)

	if result.IsError {
		t.Fatalf("unexpected error: %s", result.Output)
	}
	if result.Output != "hello world" {
		t.Errorf("got %q, want %q", result.Output, "hello world")
	}
}

func TestExecutor_WriteFile_Allowed(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "out.txt")

	sb, _ := NewSandbox(dir, []string{"out.txt"})
	exec := NewExecutor(sb, 0)

	input, _ := json.Marshal(writeFileInput{Path: target, Content: "new content"})
	result := exec.Dispatch(context.Background(), ToolWriteFile, input)

	if result.IsError {
		t.Fatalf("unexpected error: %s", result.Output)
	}

	data, err := os.ReadFile(target)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "new content" {
		t.Errorf("file content = %q, want %q", data, "new content")
	}
}

func TestExecutor_WriteFile_Denied(t *testing.T) {
	dir := t.TempDir()
	sb, _ := NewSandbox(dir, []string{"allowed.txt"})
	exec := NewExecutor(sb, 0)

	input, _ := json.Marshal(writeFileInput{Path: filepath.Join(dir, "forbidden.txt"), Content: "hack"})
	result := exec.Dispatch(context.Background(), ToolWriteFile, input)

	if !result.IsError {
		t.Error("expected write to forbidden file to fail")
	}
}

func TestExecutor_RunCommand(t *testing.T) {
	sb, _ := NewSandbox("/tmp", nil)
	exec := NewExecutor(sb, 10*time.Second)

	input, _ := json.Marshal(runCommandInput{Command: "echo hello"})
	result := exec.Dispatch(context.Background(), ToolRunCommand, input)

	if result.IsError {
		t.Fatalf("unexpected error: %s", result.Output)
	}
	if result.Output != "hello\n" {
		t.Errorf("got %q, want %q", result.Output, "hello\n")
	}
}

func TestExecutor_RunCommand_Failure(t *testing.T) {
	sb, _ := NewSandbox("/tmp", nil)
	exec := NewExecutor(sb, 10*time.Second)

	input, _ := json.Marshal(runCommandInput{Command: "false"})
	result := exec.Dispatch(context.Background(), ToolRunCommand, input)

	if !result.IsError {
		t.Error("expected failing command to return error")
	}
}

func TestExecutor_UnknownTool(t *testing.T) {
	sb, _ := NewSandbox("/tmp", nil)
	exec := NewExecutor(sb, 0)

	result := exec.Dispatch(context.Background(), "nonexistent", nil)
	if !result.IsError {
		t.Error("expected unknown tool to return error")
	}
}

func TestExecutor_ReadFile_InvalidJSON(t *testing.T) {
	sb, _ := NewSandbox("/tmp", nil)
	exec := NewExecutor(sb, 0)

	result := exec.Dispatch(context.Background(), ToolReadFile, json.RawMessage(`not-json`))
	if !result.IsError {
		t.Error("expected invalid JSON to return error")
	}
}

func TestExecutor_ReadFile_EmptyPath(t *testing.T) {
	sb, _ := NewSandbox("/tmp", nil)
	exec := NewExecutor(sb, 0)

	input, _ := json.Marshal(readFileInput{Path: ""})
	result := exec.Dispatch(context.Background(), ToolReadFile, input)

	if !result.IsError {
		t.Error("expected empty path to return error")
	}
}

func TestExecutor_ReadFile_NonexistentFile(t *testing.T) {
	dir := t.TempDir()
	sb, _ := NewSandbox(dir, nil)
	exec := NewExecutor(sb, 0)

	input, _ := json.Marshal(readFileInput{Path: filepath.Join(dir, "missing.txt")})
	result := exec.Dispatch(context.Background(), ToolReadFile, input)

	if !result.IsError {
		t.Error("expected reading nonexistent file to return error")
	}
}

func TestExecutor_WriteFile_InvalidJSON(t *testing.T) {
	sb, _ := NewSandbox("/tmp", nil)
	exec := NewExecutor(sb, 0)

	result := exec.Dispatch(context.Background(), ToolWriteFile, json.RawMessage(`not-json`))
	if !result.IsError {
		t.Error("expected invalid JSON to return error")
	}
}

func TestExecutor_WriteFile_EmptyPath(t *testing.T) {
	sb, _ := NewSandbox("/tmp", nil)
	exec := NewExecutor(sb, 0)

	input, _ := json.Marshal(writeFileInput{Path: "", Content: "data"})
	result := exec.Dispatch(context.Background(), ToolWriteFile, input)

	if !result.IsError {
		t.Error("expected empty path to return error")
	}
}

func TestExecutor_WriteFile_OSError(t *testing.T) {
	dir := t.TempDir()
	// Create the target path as a directory so WriteFile fails with an OS error.
	target := filepath.Join(dir, "out.txt")
	if err := os.Mkdir(target, 0755); err != nil {
		t.Fatal(err)
	}

	sb, _ := NewSandbox(dir, []string{"out.txt"})
	exec := NewExecutor(sb, 0)

	input, _ := json.Marshal(writeFileInput{Path: target, Content: "data"})
	result := exec.Dispatch(context.Background(), ToolWriteFile, input)

	if !result.IsError {
		t.Error("expected write to a directory path to return error")
	}
}

func TestExecutor_RunCommand_InvalidJSON(t *testing.T) {
	sb, _ := NewSandbox("/tmp", nil)
	exec := NewExecutor(sb, 0)

	result := exec.Dispatch(context.Background(), ToolRunCommand, json.RawMessage(`not-json`))
	if !result.IsError {
		t.Error("expected invalid JSON to return error")
	}
}

func TestExecutor_RunCommand_EmptyCommand(t *testing.T) {
	sb, _ := NewSandbox("/tmp", nil)
	exec := NewExecutor(sb, 0)

	input, _ := json.Marshal(runCommandInput{Command: ""})
	result := exec.Dispatch(context.Background(), ToolRunCommand, input)

	if !result.IsError {
		t.Error("expected empty command to return error")
	}
}

func TestExecutor_RunCommand_NoTimeout(t *testing.T) {
	// commandTimeout == 0 skips the WithTimeout path; verify the command still runs.
	sb, _ := NewSandbox("/tmp", nil)
	exec := NewExecutor(sb, 0)

	input, _ := json.Marshal(runCommandInput{Command: "echo no-timeout"})
	result := exec.Dispatch(context.Background(), ToolRunCommand, input)

	if result.IsError {
		t.Fatalf("unexpected error: %s", result.Output)
	}
	if result.Output != "no-timeout\n" {
		t.Errorf("got %q, want %q", result.Output, "no-timeout\n")
	}
}

func TestExecutor_RunCommand_Stderr(t *testing.T) {
	sb, _ := NewSandbox("/tmp", nil)
	exec := NewExecutor(sb, 10*time.Second)

	input, _ := json.Marshal(runCommandInput{Command: "echo warning >&2"})
	result := exec.Dispatch(context.Background(), ToolRunCommand, input)

	// A command that only writes to stderr exits 0, so IsError should be false.
	if result.IsError {
		t.Fatalf("unexpected error: %s", result.Output)
	}
	if result.Output == "" {
		t.Error("expected stderr content in output, got empty string")
	}
}

func TestExecutor_RunCommand_StdoutAndStderr(t *testing.T) {
	sb, _ := NewSandbox("/tmp", nil)
	exec := NewExecutor(sb, 10*time.Second)

	// Command produces both stdout and stderr — exercises the
	// "stdout non-empty, prepend newline before stderr" branch.
	input, _ := json.Marshal(runCommandInput{Command: "echo out; echo err >&2"})
	result := exec.Dispatch(context.Background(), ToolRunCommand, input)

	if result.IsError {
		t.Fatalf("unexpected error: %s", result.Output)
	}
	if result.Output == "" {
		t.Error("expected combined stdout/stderr in output, got empty string")
	}
}

func TestExecutor_RunCommand_StderrAndFailure(t *testing.T) {
	sb, _ := NewSandbox("/tmp", nil)
	exec := NewExecutor(sb, 10*time.Second)

	// Command writes to both stderr and exits non-zero.
	input, _ := json.Marshal(runCommandInput{Command: "echo oops >&2; exit 1"})
	result := exec.Dispatch(context.Background(), ToolRunCommand, input)

	if !result.IsError {
		t.Error("expected failing command with stderr to return error")
	}
}

func TestExecutor_RunCommand_StdoutAndFailure(t *testing.T) {
	sb, _ := NewSandbox("/tmp", nil)
	exec := NewExecutor(sb, 10*time.Second)

	// Command writes to stdout and exits non-zero — exercises the
	// "output != empty, append newline before error" branch.
	input, _ := json.Marshal(runCommandInput{Command: "echo progress; exit 1"})
	result := exec.Dispatch(context.Background(), ToolRunCommand, input)

	if !result.IsError {
		t.Error("expected failing command to return error")
	}
	if result.Output == "" {
		t.Error("expected output to contain stdout content")
	}
}

func TestExecutor_RunCommand_Timeout(t *testing.T) {
	sb, _ := NewSandbox("/tmp", nil)
	exec := NewExecutor(sb, 50*time.Millisecond)

	input, _ := json.Marshal(runCommandInput{Command: "sleep 10"})
	result := exec.Dispatch(context.Background(), ToolRunCommand, input)

	if !result.IsError {
		t.Error("expected timed-out command to return error")
	}
}
