package tools

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"
)

// Tool names recognized by the executor.
const (
	ToolReadFile   = "read_file"
	ToolWriteFile  = "write_file"
	ToolEditFile   = "edit_file"
	ToolGrep       = "grep"
	ToolRunCommand = "run_command"
	ToolRunEval    = "run_eval"
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
	Path   string `json:"path"`
	Offset int    `json:"offset,omitempty"` // 1-based start line (optional)
	Limit  int    `json:"limit,omitempty"`  // max lines to return (optional)
}

type writeFileInput struct {
	Path    string `json:"path"`
	Content string `json:"content"`
}

type editFileInput struct {
	Path string `json:"path"`
	Old  string `json:"old"`
	New  string `json:"new"`
}

type grepInput struct {
	Pattern string `json:"pattern"`
	Path    string `json:"path,omitempty"`
	Include string `json:"include,omitempty"`
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
	case ToolEditFile:
		return e.editFile(input)
	case ToolGrep:
		return e.grep(ctx, input)
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

	// If offset or limit is set, return only the requested line range.
	if in.Offset > 0 || in.Limit > 0 {
		lines := strings.Split(string(data), "\n")
		total := len(lines)
		start := 0
		if in.Offset > 0 {
			start = in.Offset - 1 // convert 1-based to 0-based
		}
		if start > total {
			start = total
		}
		end := total
		if in.Limit > 0 && start+in.Limit < total {
			end = start + in.Limit
		}
		header := fmt.Sprintf("[lines %d-%d of %d]\n", start+1, end, total)
		return Result{Output: header + strings.Join(lines[start:end], "\n")}
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

func (e *Executor) editFile(input json.RawMessage) Result {
	var in editFileInput
	if err := json.Unmarshal(input, &in); err != nil {
		return Result{Output: fmt.Sprintf("parse input: %s", err), IsError: true}
	}
	if in.Path == "" {
		return Result{Output: "path is required", IsError: true}
	}
	if in.Old == "" {
		return Result{Output: "old is required", IsError: true}
	}

	if err := e.sandbox.CheckWrite(in.Path); err != nil {
		return Result{Output: err.Error(), IsError: true}
	}

	data, err := os.ReadFile(in.Path)
	if err != nil {
		return Result{Output: fmt.Sprintf("read file: %s", err), IsError: true}
	}

	content := string(data)
	count := strings.Count(content, in.Old)
	if count == 0 {
		return Result{Output: "old string not found in file", IsError: true}
	}
	if count > 1 {
		return Result{Output: fmt.Sprintf("old string matches %d locations; must be unique", count), IsError: true}
	}

	updated := strings.Replace(content, in.Old, in.New, 1)
	if err := os.WriteFile(in.Path, []byte(updated), 0644); err != nil {
		return Result{Output: fmt.Sprintf("write file: %s", err), IsError: true}
	}
	return Result{Output: fmt.Sprintf("edited %s (%+d bytes)", in.Path, len(updated)-len(content))}
}

func (e *Executor) grep(ctx context.Context, input json.RawMessage) Result {
	var in grepInput
	if err := json.Unmarshal(input, &in); err != nil {
		return Result{Output: fmt.Sprintf("parse input: %s", err), IsError: true}
	}
	if in.Pattern == "" {
		return Result{Output: "pattern is required", IsError: true}
	}

	path := "."
	if in.Path != "" {
		path = in.Path
	}

	if err := e.sandbox.CheckRead(path); err != nil {
		return Result{Output: err.Error(), IsError: true}
	}

	args := []string{"-rn", "--color=never", "--max-count=200"}
	if in.Include != "" {
		args = append(args, "--include="+in.Include)
	}
	args = append(args, in.Pattern, path)

	if e.commandTimeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, e.commandTimeout)
		defer cancel()
	}

	cmd := exec.CommandContext(ctx, "grep", args...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()
	if err != nil {
		// grep exits 1 for no matches, 2+ for real errors.
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) && exitErr.ExitCode() == 1 {
			return Result{Output: "no matches"}
		}
		msg := stderr.String()
		if msg == "" {
			msg = err.Error()
		}
		return Result{Output: "grep error: " + msg, IsError: true}
	}
	return Result{Output: stdout.String()}
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
