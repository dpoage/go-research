package experiment

import (
	"context"
	"fmt"
	"regexp"
	"strings"

	"github.com/dpoage/go-research/llm"
	"github.com/dpoage/go-research/tools"
)

// maxOrientationFreeRounds caps read-only tool calls during the orientation
// phase to prevent the model from endlessly exploring.
const maxOrientationFreeRounds = 5

// orientationToolDefs returns the read-only subset of tools available during
// the orientation phase. No mutating tools are provided.
func orientationToolDefs() []llm.ToolDef {
	return []llm.ToolDef{
		{
			Name:        tools.ToolReadFile,
			Description: "Read file contents. Use this to examine files before deciding on a change.",
			InputSchema: readFileSchema,
		},
		{
			Name:        tools.ToolGrep,
			Description: "Search file contents with grep -rn. Use this to find patterns in the codebase.",
			InputSchema: grepSchema,
		},
	}
}

// OrientationBrief is the structured output from the orientation phase.
type OrientationBrief struct {
	Working string // What is currently working well and why.
	Failing string // What has been tried and failed, hypothesis for why.
	Gap     string // Single biggest gap between current output and target metric.
	Change  string // One specific, testable change to try next.
	Risk    string // What could go wrong with this change.
}

// historyEntry summarizes a past iteration for the orientation prompt.
type historyEntry struct {
	Iteration int
	Summary   string
	Metric    float64
	Best      float64 // Best metric at the time of this iteration.
	Status    Status
}

// buildOrientationPrompt constructs the user message for the orientation LLM call.
func (l *Loop) buildOrientationPrompt(iter int, bestMetric float64, history []historyEntry) string {
	var b strings.Builder
	fmt.Fprintf(&b, "## Orientation — Iteration %d\n\n", iter)
	b.WriteString("You are in a READ-ONLY investigation phase. You have access to read_file and grep only. You MUST NOT suggest edits, write code, or call done.\n\n")

	// Current state.
	b.WriteString("### Current State\n")
	fmt.Fprintf(&b, "- **Best metric:** %.6f (%v is better)\n", bestMetric, l.config.Eval.Direction)
	fmt.Fprintf(&b, "- **Eval command:** `%s`\n", l.config.Eval.Command)
	fmt.Fprintf(&b, "- **Editable files:** %v\n", l.config.Files)

	// Iteration history with deltas.
	if len(history) > 0 {
		b.WriteString("\n### Iteration History\n")
		for _, h := range history {
			delta := h.Metric - h.Best
			sign := "+"
			if delta < 0 {
				sign = ""
			}
			if h.Summary != "" {
				fmt.Fprintf(&b, "- Iteration %d: %s → metric=%g (best was %g, delta=%s%g) [%s]\n",
					h.Iteration, h.Summary, h.Metric, h.Best, sign, delta, h.Status)
			} else {
				fmt.Fprintf(&b, "- Iteration %d: metric=%g (best was %g, delta=%s%g) [%s]\n",
					h.Iteration, h.Metric, h.Best, sign, delta, h.Status)
			}
		}
	} else {
		b.WriteString("\n### Iteration History\nNo prior iterations. Read the target files to understand the current implementation before assessing.\n")
	}

	// Instructions.
	b.WriteString(`
### Your Task

Step 1: Read at least one file to ground your analysis in the actual code. Blind assessment without investigation is not useful.

Step 2: Produce your assessment in EXACTLY this format:

<orientation>
<working>What is currently working well and why. (1-2 sentences)</working>
<failing>What has been tried and failed, with your hypothesis for WHY it failed — not just that it did. (1-2 sentences, or "Nothing yet" if first iteration)</failing>
<gap>The single biggest gap between current output and the target metric. (1 sentence)</gap>
<change>One specific, testable change to try next. Must differ MECHANISTICALLY from [discard] entries — not just in wording. (1 sentence, no code)</change>
<risk>What could go wrong with this change, and how the action phase should detect it. (1 sentence)</risk>
</orientation>

Constraints on <change>:
- SINGLE concrete change, not a compound ("do X and also Y").
- Must be mechanistically different from every [discard] entry — if doubling failed, tripling is the same mechanism.
- Must be specific enough that someone else could implement it without clarifying questions.
- No code snippets — describe the approach, not the diff.
`)

	return b.String()
}

// Pre-compiled regexes for parsing the orientation block and its fields.
var (
	orientationTagRe = regexp.MustCompile(`(?s)<orientation>\s*(.*?)\s*</orientation>`)
	fieldRegexes     = map[string]*regexp.Regexp{
		"working": regexp.MustCompile(`(?s)<working>\s*(.*?)\s*</working>`),
		"failing": regexp.MustCompile(`(?s)<failing>\s*(.*?)\s*</failing>`),
		"gap":     regexp.MustCompile(`(?s)<gap>\s*(.*?)\s*</gap>`),
		"change":  regexp.MustCompile(`(?s)<change>\s*(.*?)\s*</change>`),
		"risk":    regexp.MustCompile(`(?s)<risk>\s*(.*?)\s*</risk>`),
	}
)

// parseOrientationBrief extracts the structured brief from the model's response text.
// Returns a zero-value brief and false if the block is missing or malformed.
func parseOrientationBrief(text string) (OrientationBrief, bool) {
	m := orientationTagRe.FindStringSubmatch(text)
	if m == nil {
		return OrientationBrief{}, false
	}
	inner := m[1]

	extract := func(name string) string {
		fm := fieldRegexes[name].FindStringSubmatch(inner)
		if fm == nil {
			return ""
		}
		return strings.TrimSpace(fm[1])
	}

	brief := OrientationBrief{
		Working: extract("working"),
		Failing: extract("failing"),
		Gap:     extract("gap"),
		Change:  extract("change"),
		Risk:    extract("risk"),
	}

	if brief.Change == "" {
		return OrientationBrief{}, false
	}

	return brief, true
}

// formatBriefForAction formats the orientation brief as context for the action prompt.
func formatBriefForAction(brief OrientationBrief) string {
	var b strings.Builder
	b.WriteString("### Orientation Brief\n")
	fmt.Fprintf(&b, "- **Working:** %s\n", brief.Working)
	fmt.Fprintf(&b, "- **Failing:** %s\n", brief.Failing)
	fmt.Fprintf(&b, "- **Gap:** %s\n", brief.Gap)
	fmt.Fprintf(&b, "- **Planned change:** %s\n", brief.Change)
	fmt.Fprintf(&b, "- **Risk:** %s\n", brief.Risk)
	return b.String()
}

// runOrientation executes the orientation phase: a single LLM call (with possible
// read-only tool rounds) that produces an OrientationBrief. Token usage is returned
// for accounting. If the model fails to produce a valid brief, a fallback brief is
// returned so the action phase can still proceed.
func (l *Loop) runOrientation(ctx context.Context, system string, iter int, bestMetric float64, history []historyEntry) (OrientationBrief, ToolLoopStats, error) {
	prompt := l.buildOrientationPrompt(iter, bestMetric, history)
	messages := []llm.Message{llm.NewTextMessage(llm.RoleUser, prompt)}
	toolDefs := orientationToolDefs()

	var stats ToolLoopStats
	freeRounds := 0

	for {
		if ctx.Err() != nil {
			return OrientationBrief{}, stats, ctx.Err()
		}

		// After max free rounds, strip tools to force text output.
		reqTools := toolDefs
		if freeRounds >= maxOrientationFreeRounds {
			reqTools = nil
		}

		compressed := compressHistory(messages, keepRecentRounds)

		resp, err := l.provider.Complete(ctx, &llm.Request{
			System:    system,
			Messages:  compressed,
			Tools:     reqTools,
			MaxTokens: l.config.Provider.MaxTokens,
		})
		if err != nil {
			return OrientationBrief{}, stats, fmt.Errorf("orientation LLM call (round %d): %w", stats.Rounds+1, err)
		}

		stats.Rounds++
		stats.InputTokens += resp.Usage.InputTokens
		stats.OutputTokens += resp.Usage.OutputTokens

		messages = append(messages, llm.Message{Role: llm.RoleAssistant, Content: resp.Content})

		if text := resp.TextContent(); text != "" {
			l.observer.AgentText(text)
		}

		// Check for tool calls — only read-only tools are available.
		toolCalls := resp.ToolUseBlocks()
		if len(toolCalls) == 0 || resp.StopReason == llm.StopEndTurn {
			// Model is done — parse the brief from the text.
			text := resp.TextContent()
			brief, ok := parseOrientationBrief(text)
			if !ok {
				brief = OrientationBrief{
					Working: "Unable to assess — orientation did not complete",
					Failing: "Unable to assess",
					Gap:     "Unknown — read the target files to understand the metric",
					Change:  "Read the main source file and make the most impactful single change for the metric",
					Risk:    "Without orientation analysis, this change may duplicate a prior failed attempt",
				}
			}
			return brief, stats, nil
		}

		// Dispatch read-only tool calls.
		var resultBlocks []llm.ContentBlock
		for _, tc := range toolCalls {
			result := l.executor.Dispatch(ctx, tc.Name, tc.Input)
			output := result.Output
			if len(output) > maxToolOutput {
				output = output[:maxToolOutput] + "\n... (truncated)"
			}
			l.observer.ToolCall(tc.Name, output)
			resultBlocks = append(resultBlocks, llm.ContentBlock{
				Type: llm.BlockToolResult, ID: tc.ID, Content: output, IsError: result.IsError,
			})
		}

		freeRounds++
		if freeRounds >= maxOrientationFreeRounds {
			resultBlocks = append(resultBlocks, llm.ContentBlock{
				Type: llm.BlockText, Text: "[Orientation tool limit reached. Produce your <orientation> block now.]",
			})
		}

		messages = append(messages, llm.Message{Role: llm.RoleUser, Content: resultBlocks})
	}
}

// buildIterHistory converts past iterOutcomes into historyEntry slices for the
// orientation prompt. It keeps at most the last maxHistory entries.
func buildIterHistory(outcomes []iterOutcome, maxHistory int) []historyEntry {
	start := 0
	if len(outcomes) > maxHistory {
		start = len(outcomes) - maxHistory
	}
	var entries []historyEntry
	for i := start; i < len(outcomes); i++ {
		o := outcomes[i]
		// Skip error outcomes — they don't have meaningful metrics
		// and would clutter the orientation history.
		if o.Status == StatusError {
			continue
		}
		entries = append(entries, historyEntry{
			Iteration: i + 1, // 1-indexed
			Summary:   o.Summary,
			Metric:    o.Metric,
			Best:      o.BestAtTime,
			Status:    o.Status,
		})
	}
	return entries
}
