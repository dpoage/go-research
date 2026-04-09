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

// runDeps holds the resolved dependencies for a run, split out from runRun
// so tests can construct these directly with mocks.
type runDeps struct {
	cfg      *config.Config
	provider llm.Provider
	executor *tools.Executor
	eval     *experiment.Eval
	git      *experiment.Git
	logger   *experiment.ResultLogger
	maxIter  int
	verbose  bool
}

func parseRunFlags(args []string) (maxIter int, resultFile string, verbose bool, err error) {
	fs := flag.NewFlagSet("run", flag.ContinueOnError)
	mi := fs.Int("max-iter", 0, "maximum iterations (0 = unlimited)")
	rf := fs.String("results", defaultResultsFile, "path to TSV result log")
	v := fs.BoolP("verbose", "v", false, "show full agent and tool output")
	if err = fs.Parse(args); err != nil {
		return
	}
	return *mi, *rf, *v, nil
}

func buildRunDeps(gf globalFlags, maxIter int, resultFile string, verbose bool) (*runDeps, error) {
	cfg, err := config.Load(gf.config)
	if err != nil {
		return nil, err
	}

	provider, err := llm.NewProvider(cfg.Provider)
	if err != nil {
		return nil, fmt.Errorf("create provider: %w", err)
	}

	sandbox, err := tools.NewSandbox(".", cfg.Files)
	if err != nil {
		return nil, fmt.Errorf("create sandbox: %w", err)
	}

	eval, err := experiment.NewEval(cfg.Eval)
	if err != nil {
		return nil, fmt.Errorf("create eval: %w", err)
	}

	logger, err := experiment.NewResultLogger(resultFile)
	if err != nil {
		return nil, fmt.Errorf("create result logger: %w", err)
	}

	return &runDeps{
		cfg:      cfg,
		provider: provider,
		executor: tools.NewExecutor(sandbox, defaultToolTimeout),
		eval:     eval,
		git:      experiment.NewGit(cfg.Git.Enabled, ".", cfg.Files),
		logger:   logger,
		maxIter:  maxIter,
		verbose:  verbose,
	}, nil
}

func executeRun(ctx context.Context, d *runDeps, quiet bool) error {
	if d.cfg.Git.Enabled {
		branch, err := d.git.CreateBranch(d.cfg.Git.BranchPrefix)
		if err != nil {
			return fmt.Errorf("create branch: %w", err)
		}
		if !quiet {
			fmt.Printf("Created branch: %s\n", branch)
		}
	}

	if !quiet {
		fmt.Printf("Config: %v/%s, eval=%q, direction=%v\n",
			d.cfg.Provider.Backend, d.cfg.Provider.Model,
			d.cfg.Eval.Command, d.cfg.Eval.Direction)
		if d.maxIter > 0 {
			fmt.Printf("Max iterations: %d\n", d.maxIter)
		}
	}

	var observer experiment.Observer
	if d.verbose {
		observer = experiment.VerboseObserver{}
	} else {
		observer = experiment.NewStatusLineObserver()
	}

	loop := experiment.NewLoop(experiment.LoopParams{
		Config:   d.cfg,
		Provider: d.provider,
		Executor: d.executor,
		Eval:     d.eval,
		Git:      d.git,
		Logger:   d.logger,
		Observer: observer,
	})
	return loop.Run(ctx, d.maxIter)
}

func runRun(ctx context.Context, gf globalFlags, args []string) error {
	maxIter, resultFile, verbose, err := parseRunFlags(args)
	if err != nil {
		return err
	}

	d, err := buildRunDeps(gf, maxIter, resultFile, verbose)
	if err != nil {
		return err
	}

	return executeRun(ctx, d, gf.quiet)
}
