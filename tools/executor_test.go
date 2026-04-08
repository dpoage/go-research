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
