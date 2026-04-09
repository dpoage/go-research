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

const (
	// defaultToolTimeout is the maximum duration for run_command tool calls.
	defaultToolTimeout = 30 * time.Second

	defaultResultsFile = "results.tsv"
)

func runRun(ctx context.Context, gf globalFlags, args []string) error {
	fs := flag.NewFlagSet("run", flag.ContinueOnError)
	maxIter := fs.Int("max-iter", 0, "maximum iterations (0 = unlimited)")
	resultFile := fs.String("results", defaultResultsFile, "path to TSV result log")
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

	executor := tools.NewExecutor(sandbox, defaultToolTimeout)

	eval, err := experiment.NewEval(cfg.Eval)
	if err != nil {
		return fmt.Errorf("create eval: %w", err)
	}

	git := experiment.NewGit(cfg.Git.Enabled, ".", cfg.Files)

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
		fmt.Printf("Config: %v/%s, eval=%q, direction=%v\n",
			cfg.Provider.Backend, cfg.Provider.Model,
			cfg.Eval.Command, cfg.Eval.Direction)
		if *maxIter > 0 {
			fmt.Printf("Max iterations: %d\n", *maxIter)
		}
	}

	loop := experiment.NewLoop(cfg, provider, executor, eval, git, logger, *resultFile)
	return loop.Run(ctx, *maxIter)
}
