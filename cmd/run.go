package cmd

import (
	"context"
	"fmt"

	flag "github.com/spf13/pflag"

	"github.com/dpoage/go-research/config"
	"github.com/dpoage/go-research/llm"
)

func runRun(ctx context.Context, gf globalFlags, args []string) error {
	fs := flag.NewFlagSet("run", flag.ContinueOnError)
	if err := fs.Parse(args); err != nil {
		return err
	}

	cfg, err := config.Load(gf.config)
	if err != nil {
		return err
	}

	provider, err := llm.NewProvider(cfg.Provider)
	if err != nil {
		return fmt.Errorf("create provider: %w", err)
	}

	// TODO: wire up experiment loop (Phase 3).
	_ = provider
	_ = ctx

	if !gf.quiet {
		fmt.Println("config loaded successfully")
		fmt.Printf("  program:   %s\n", cfg.Program)
		fmt.Printf("  files:     %v\n", cfg.Files)
		fmt.Printf("  eval:      %s\n", cfg.Eval.Command)
		fmt.Printf("  direction: %s\n", cfg.Eval.Direction)
		fmt.Printf("  timeout:   %s\n", cfg.Eval.Timeout)
		fmt.Printf("  provider:  %s/%s\n", cfg.Provider.Backend, cfg.Provider.Model)
	}

	fmt.Println("\nexperiment loop not yet implemented (see Phase 3)")
	return nil
}
