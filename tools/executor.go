package tools

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"time"
)

// Tool names recognized by the executor.
const (
	ToolReadFile   = "read_file"
	ToolWriteFile  = "write_file"
	ToolRunCommand = "run_command"
	ToolDone       = "done"
)

// Executor dispatches tool calls and enforces sandbox rules.
type Executor struct {
	sandbox        *Sandbox
	commandTimeout time.Duration
}

// NewExecutor creates an Executor with the given sandbox and command timeout.
func NewExecutor(sandbox *Sandbox, cmdTimeout time.Duration) *Executor {
	return &Executor{sandbox: sandbox, commandTimeout: cmdTimeout}
}

type readFileInput struct {
	Path string `json:"path"`
}

type writeFileInput struct {
	Path    string `json:"path"`
	Content string `json:"content"`
}

type runCommandInput struct {
	Command string `json:"command"`
}

// Result is the output of a tool execution.
type Result struct {
	Output  string
	IsError bool
}

func (e *Executor) Dispatch(ctx context.Context, name string, input json.RawMessage) Result {
	switch name {
	case ToolReadFile:
		return e.readFile(input)
	case ToolWriteFile:
		return e.writeFile(input)
	case ToolRunCommand:
		return e.runCommand(ctx, input)
	default:
		return Result{Output: fmt.Sprintf("unknown tool: %q", name), IsError: true}
	}
}

func (e *Executor) readFile(input json.RawMessage) Result {
	var in readFileInput
	if err := json.Unmarshal(input, &in); err != nil {
		return Result{Output: fmt.Sprintf("parse input: %s", err), IsError: true}
	}
	if in.Path == "" {
		return Result{Output: "path is required", IsError: true}
	}

	if err := e.sandbox.CheckRead(in.Path); err != nil {
		return Result{Output: err.Error(), IsError: true}
	}

	data, err := os.ReadFile(in.Path)
	if err != nil {
		return Result{Output: fmt.Sprintf("read file: %s", err), IsError: true}
	}
	return Result{Output: string(data)}
}

func (e *Executor) writeFile(input json.RawMessage) Result {
	var in writeFileInput
	if err := json.Unmarshal(input, &in); err != nil {
		return Result{Output: fmt.Sprintf("parse input: %s", err), IsError: true}
	}
	if in.Path == "" {
		return Result{Output: "path is required", IsError: true}
	}

	if err := e.sandbox.CheckWrite(in.Path); err != nil {
		return Result{Output: err.Error(), IsError: true}
	}

	if err := os.WriteFile(in.Path, []byte(in.Content), 0644); err != nil {
		return Result{Output: fmt.Sprintf("write file: %s", err), IsError: true}
	}
	return Result{Output: fmt.Sprintf("wrote %d bytes to %s", len(in.Content), in.Path)}
}

func (e *Executor) runCommand(ctx context.Context, input json.RawMessage) Result {
	var in runCommandInput
	if err := json.Unmarshal(input, &in); err != nil {
		return Result{Output: fmt.Sprintf("parse input: %s", err), IsError: true}
	}
	if in.Command == "" {
		return Result{Output: "command is required", IsError: true}
	}

	if e.commandTimeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, e.commandTimeout)
		defer cancel()
	}

	cmd := exec.CommandContext(ctx, "sh", "-c", in.Command)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()

	output := stdout.String()
	if stderr.Len() > 0 {
		if output != "" {
			output += "\n"
		}
		output += "stderr: " + stderr.String()
	}

	if err != nil {
		if output != "" {
			output += "\n"
		}
		output += fmt.Sprintf("error: %s", err)
		return Result{Output: output, IsError: true}
	}
	return Result{Output: output}
}
