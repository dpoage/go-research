package cmd

import (
	"context"
	"fmt"
	"time"

	flag "github.com/spf13/pflag"

	"github.com/dpoage/go-research/config"
	"github.com/dpoage/go-research/experiment"
	"github.com/dpoage/go-research/llm"
	"github.com/dpoage/go-research/tools"
)

func runRun(ctx context.Context, gf globalFlags, args []string) error {
	fs := flag.NewFlagSet("run", flag.ContinueOnError)
	maxIter := fs.Int("max-iter", 0, "maximum iterations (0 = unlimited)")
	resultFile := fs.String("results", "results.tsv", "path to TSV result log")
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

	sandbox, err := tools.NewSandbox(".", cfg.Files)
	if err != nil {
		return fmt.Errorf("create sandbox: %w", err)
	}

	executor := tools.NewExecutor(sandbox, 30*time.Second)

	eval, err := experiment.NewEval(cfg.Eval.Command, cfg.Eval.Metric, cfg.Eval.Timeout)
	if err != nil {
		return fmt.Errorf("create eval: %w", err)
	}

	git := experiment.NewGit(cfg.Git.Enabled)

	logger, err := experiment.NewResultLogger(*resultFile)
	if err != nil {
		return fmt.Errorf("create result logger: %w", err)
	}

	if cfg.Git.Enabled {
		branch, err := git.CreateBranch(cfg.Git.BranchPrefix)
		if err != nil {
			return fmt.Errorf("create branch: %w", err)
		}
		if !gf.quiet {
			fmt.Printf("Created branch: %s\n", branch)
		}
	}

	if !gf.quiet {
		fmt.Printf("Config: %s/%s, eval=%q, direction=%s\n",
			cfg.Provider.Backend, cfg.Provider.Model,
			cfg.Eval.Command, cfg.Eval.Direction)
		if *maxIter > 0 {
			fmt.Printf("Max iterations: %d\n", *maxIter)
		}
	}

	loop := &experiment.Loop{
		Config:   cfg,
		Provider: provider,
		Executor: executor,
		Eval:     eval,
		Git:      git,
		Logger:   logger,
	}

	return loop.Run(ctx, *maxIter)
}
