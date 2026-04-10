package experiment

import (
	"fmt"
	"math"
	"os"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/dpoage/go-research/display"
)

// StatusLineObserver renders experiment progress as a single updating terminal line.
type StatusLineObserver struct {
	kept            []float64
	best            float64
	nKeep           int
	nDisc           int
	nErr            int
	phase           string
	lastLen         int
	lastRounds      int
	lastInputTokens int
}

// NewStatusLineObserver creates a StatusLineObserver.
func NewStatusLineObserver() *StatusLineObserver {
	return &StatusLineObserver{best: math.NaN()}
}

func (o *StatusLineObserver) IterationStart(iter, maxIter int) {
	if maxIter > 0 {
		o.phase = fmt.Sprintf("iter %d/%d", iter, maxIter)
	} else {
		o.phase = fmt.Sprintf("iter %d", iter)
	}
	o.render("thinking...")
}

func (o *StatusLineObserver) AgentText(_ string) {}

func (o *StatusLineObserver) ToolCall(name, _ string) {
	o.render(name)
}

func (o *StatusLineObserver) EvalStarted() {
	o.render("evaluating...")
}

func (o *StatusLineObserver) EvalResult(_ int, _ float64, _ time.Duration) {}

func (o *StatusLineObserver) Improvement(_ int, metric, _ float64) {
	o.best = metric
	o.kept = append(o.kept, metric)
	o.nKeep++
	o.render("kept")
}

func (o *StatusLineObserver) NoImprovement(_ int, _ float64, _ float64) {
	o.nDisc++
	o.render("reverted")
}

func (o *StatusLineObserver) IterationError(_ int, _ error) {
	o.nErr++
	o.render("error")
}

func (o *StatusLineObserver) ToolLoopComplete(_ int, stats ToolLoopStats) {
	o.lastRounds = stats.Rounds
	o.lastInputTokens = stats.InputTokens
}

func (o *StatusLineObserver) Warning(_ string) {}

func (o *StatusLineObserver) Complete(bestMetric float64) {
	o.clearLine()
	if math.IsNaN(bestMetric) {
		fmt.Fprintf(os.Stderr, "Done. No successful iterations.\n")
	} else {
		spark := display.Sparkline(o.kept)
		fmt.Fprintf(os.Stderr, "Done. best: %.6f %s (%d kept, %d discarded, %d errors)\n",
			bestMetric, spark, o.nKeep, o.nDisc, o.nErr)
	}
}

func (o *StatusLineObserver) render(activity string) {
	var b strings.Builder

	b.WriteString(o.phase)

	if !math.IsNaN(o.best) {
		fmt.Fprintf(&b, " │ best: %.6f", o.best)
	}

	if spark := display.Sparkline(o.kept); spark != "" {
		fmt.Fprintf(&b, " %s", spark)
	}

	counts := fmt.Sprintf(" │ %dk %dd %de", o.nKeep, o.nDisc, o.nErr)
	b.WriteString(counts)

	fmt.Fprintf(&b, " │ %s", activity)

	line := b.String()
	width := utf8.RuneCountInString(line)

	// Pad with spaces to overwrite any previous longer line.
	if width < o.lastLen {
		line += strings.Repeat(" ", o.lastLen-width)
	}
	o.lastLen = width

	fmt.Fprintf(os.Stderr, "\r%s", line)
}

func (o *StatusLineObserver) clearLine() {
	if o.lastLen > 0 {
		fmt.Fprintf(os.Stderr, "\r%s\r", strings.Repeat(" ", o.lastLen))
	}
}
