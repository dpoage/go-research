package cmd

import (
	"errors"
	"fmt"
	"os"
	"text/tabwriter"

	flag "github.com/spf13/pflag"
)

func runHistory(gf globalFlags, args []string) error {
	fs := flag.NewFlagSet("history", flag.ContinueOnError)
	resultFile := fs.String("results", defaultResultsFile, "path to TSV result log")
	if err := fs.Parse(args); err != nil {
		return err
	}

	rows, err := parseResults(*resultFile)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("no results found (%s does not exist)", *resultFile)
		}
		return fmt.Errorf("parse results: %w", err)
	}

	w := tabwriter.NewWriter(os.Stdout, 0, 4, 2, ' ', 0)
	fmt.Fprintln(w, "ITER\tMETRIC\tSTATUS\tELAPSED\tTIMESTAMP\tNOTE")
	for _, r := range rows {
		fmt.Fprintf(w, "%d\t%.6f\t%s\t%dms\t%s\t%s\n",
			r.Iteration, r.Metric, r.Status, r.ElapsedMs, r.Timestamp, r.Note)
	}
	w.Flush()

	return nil
}
