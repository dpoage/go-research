// Package cmd implements the CLI for go-research.
package cmd

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"

	flag "github.com/spf13/pflag"
)

// globalFlags holds flags parsed before the subcommand.
type globalFlags struct {
	config string
	quiet  bool
}

func parseGlobalFlags(args []string) (globalFlags, []string) {
	var gf globalFlags

	fs := flag.NewFlagSet("go-research", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	fs.SetInterspersed(false)
	fs.StringVar(&gf.config, "config", "research.yaml", "config file path")
	fs.BoolVar(&gf.quiet, "quiet", false, "suppress non-essential output")

	_ = fs.Parse(args)
	return gf, fs.Args()
}

func usage() {
	fmt.Fprint(os.Stderr, `go-research - autonomous experiment loop for any domain

Usage:
  go-research [flags] <command> [command flags]

Flags:
  --config <path>    Config file (default: research.yaml)
  --quiet            Suppress non-essential output

Commands:
  init       Scaffold research.yaml and program.md
  validate   Dry-run config check
  run        Start the autonomous experiment loop
  status     Show current run state
  history    Display experiment results
  version    Print version

Run 'go-research <command> --help' for details.
`)
}

// Run is the CLI entry point. Returns an exit code.
func Run(ctx context.Context, args []string) int {
	if len(args) == 0 {
		usage()
		return 0
	}

	gf, remaining := parseGlobalFlags(args)

	if len(remaining) == 0 {
		usage()
		return 0
	}

	subcmd := remaining[0]
	subArgs := remaining[1:]

	var err error
	switch subcmd {
	case "help", "--help", "-h":
		usage()
		return 0
	case "version":
		fmt.Println("go-research v0.1.0-dev")
		return 0
	case "init":
		err = runInit(subArgs)
	case "validate":
		err = runValidate(gf, subArgs)
	case "run":
		err = runRun(ctx, gf, subArgs)
	case "status":
		err = runStatus(gf, subArgs)
	case "history":
		err = runHistory(gf, subArgs)
	default:
		fmt.Fprintf(os.Stderr, "unknown command: %s\n", subcmd)
		usage()
		return 1
	}

	if err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		return 1
	}
	return 0
}
