package cmd

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	flag "github.com/spf13/pflag"

	"github.com/dpoage/go-research/config"
	"github.com/dpoage/go-research/experiment"
)

// checkResult holds the outcome of a single validation check.
type checkResult struct {
	name string
	ok   bool
	msg  string // non-empty on failure
}

func (c checkResult) String() string {
	if c.ok {
		return fmt.Sprintf("  pass  %s", c.name)
	}
	return fmt.Sprintf("  FAIL  %s: %s", c.name, c.msg)
}

func runValidate(gf globalFlags, args []string) error {
	fs := flag.NewFlagSet("validate", flag.ContinueOnError)
	if err := fs.Parse(args); err != nil {
		return err
	}

	var checks []checkResult

	// 1. Config parses correctly.
	cfg, err := config.Load(gf.config)
	if err != nil {
		checks = append(checks, checkResult{name: "config parses", ok: false, msg: err.Error()})
		printChecks(checks)
		return fmt.Errorf("validation failed")
	}
	checks = append(checks, checkResult{name: "config parses", ok: true})

	// 2. Referenced files exist.
	checks = append(checks, checkFiles(cfg)...)

	// 3. API key env var is set.
	checks = append(checks, checkAPIKey(cfg))

	// 4. Eval command runs successfully and metric regex extracts a value.
	evalChecks := checkEval(cfg)
	checks = append(checks, evalChecks...)

	printChecks(checks)

	for _, c := range checks {
		if !c.ok {
			return fmt.Errorf("validation failed")
		}
	}
	return nil
}

func checkFiles(cfg *config.Config) []checkResult {
	var missing []string
	if _, err := os.Stat(cfg.Program); err != nil {
		missing = append(missing, cfg.Program)
	}
	for _, f := range cfg.Files {
		if _, err := os.Stat(f); err != nil {
			missing = append(missing, f)
		}
	}
	if strings.HasPrefix(cfg.Eval.Source, "file:") {
		sourcePath := cfg.Eval.Source[5:]
		if _, err := os.Stat(sourcePath); err != nil {
			missing = append(missing, sourcePath)
		}
	}
	if len(missing) > 0 {
		return []checkResult{{
			name: "files exist",
			ok:   false,
			msg:  "missing " + strings.Join(missing, ", "),
		}}
	}
	return []checkResult{{name: "files exist", ok: true}}
}

func checkAPIKey(cfg *config.Config) checkResult {
	if cfg.Provider.APIKeyEnv == "" {
		return checkResult{name: "api key set", ok: false, msg: "provider.api_key_env not configured"}
	}
	if os.Getenv(cfg.Provider.APIKeyEnv) == "" {
		return checkResult{name: "api key set", ok: false, msg: fmt.Sprintf("$%s is not set", cfg.Provider.APIKeyEnv)}
	}
	return checkResult{name: "api key set", ok: true}
}

func checkEval(cfg *config.Config) []checkResult {
	ev, err := experiment.NewEval(cfg.Eval.Command, cfg.Eval.Metric, cfg.Eval.Source, cfg.Eval.Timeout.Duration)
	if err != nil {
		return []checkResult{{name: "eval command runs", ok: false, msg: err.Error()}}
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	result := ev.Run(ctx)
	if result.Error != nil {
		errMsg := result.Error.Error()
		// If the error mentions "eval command failed", it's a command failure.
		// Otherwise it's a metric extraction failure (the command ran but
		// the extractor couldn't find a metric in the output).
		if strings.Contains(errMsg, "eval command failed") {
			return []checkResult{
				{name: "eval command runs", ok: false, msg: errMsg},
			}
		}
		return []checkResult{
			{name: "eval command runs", ok: true},
			{name: "metric extracted", ok: false, msg: errMsg},
		}
	}

	return []checkResult{
		{name: "eval command runs", ok: true},
		{name: "metric extracted", ok: true, msg: fmt.Sprintf("value=%.6g", result.Metric)},
	}
}

func printChecks(checks []checkResult) {
	for _, c := range checks {
		fmt.Println(c)
	}
}
