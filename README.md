# go-research

An autonomous experiment loop for any domain. Point it at your code, give it an
eval command and a metric to optimize, and let an LLM iteratively propose
changes — keeping improvements, reverting failures.

Inspired by [Karpathy's autoresearch](https://github.com/karpathy/autoresearch),
generalized into a config-driven CLI.

## How it works

1. The LLM reads your code and proposes a change via tool calls (read/write/run).
2. The eval command runs and a metric is extracted from its output.
3. If the metric improves, the change is committed. Otherwise it's reverted.
4. Repeat.

## Install

```
go install github.com/dpoage/go-research@latest
```

## Quick start

```bash
# Scaffold a working counter example (no flags needed)
go-research init
go-research validate   # verify config, files, eval, API key
go-research run --max-iter 5
```

## Custom project

```bash
go-research init \
  --file train.py \
  --eval "python train.py" \
  --metric 'val_loss:\s+([0-9.]+)' \
  --direction minimize

# Edit research.yaml and program.md to taste, then:
go-research run
```

## Configuration

Everything lives in `research.yaml`:

```yaml
program: program.md          # Agent instructions

files:
  - train.py                 # Files the agent may edit

eval:
  command: "python train.py" # Run after each change
  # source: stdout           # or file:<path>
  metric: 'val_loss:\s+([0-9.]+)'  # Regex to extract a number
  direction: minimize        # or "maximize"
  timeout: 5m

provider:
  backend: anthropic         # or "openai"
  model: claude-sonnet-4-20250514
  api_key_env: ANTHROPIC_API_KEY
  max_tokens: 16384

git:
  enabled: true
  branch_prefix: "research/"
```

## Metric extraction

After each eval command runs, go-research extracts a numeric metric in two
steps: **source** (where to read text from) and **extractor** (how to pull a
number out of that text).

### Metric sources (`eval.source`)

| Value | Description |
|-------|-------------|
| `stdout` (default) | Combined stdout+stderr of the eval command |
| `file:<path>` | Read metric text from a file after eval runs |

### Metric extractors (`eval.metric`)

| Prefix | Description |
|--------|-------------|
| *(regex)* | Capture-group pattern applied to source text (default) |
| `jq:` | JSON dot-path (e.g. `jq:.results.val_bpb`, `jq:.[0].score`) |
| `last-number` | Last float in source text (integers, decimals, `1.23e-4`) |

### Example with source

```yaml
eval:
  command: "python train.py"
  source: "file:results.json"   # read metric from file
  metric: "jq:.val_loss"        # extract with JSON path
  direction: minimize
```

### `file:` shorthand in metric

The metric field also accepts `file:<path>:<extractor>` as a shorthand that
combines source and extractor in one field:

```yaml
metric: 'file:results.json:jq:.loss'
metric: 'file:output.txt:last-number'
metric: 'file:log.txt:regex:score:\s+([0-9.]+)'
```

This is equivalent to setting `source: file:<path>` and `metric: <extractor>`
separately. Using separate fields is clearer for new configs.

## Eval requirements

The eval command must:

- **Exit 0 on success.** A non-zero exit code is treated as a failed experiment
  (the change is reverted without checking the metric).
- **Produce a metric.** Print it to stdout/stderr, or write it to a file when
  `source` is set to `file:<path>`.

The command runs via `sh -c`, so it uses POSIX shell — not bash. If you need
bash features, call `bash -c "..."` or point at a bash script.

When `git.enabled` is true, the project directory must be a git repository.

## Commands

| Command    | Description                                      |
|------------|--------------------------------------------------|
| `init`     | Scaffold config and program files                |
| `validate` | Dry-run check: config, files, eval, API key      |
| `run`      | Start the autonomous experiment loop             |
| `status`   | Show current branch, best metric, iteration count|
| `history`  | Display formatted table of all results           |
| `version`  | Print version                                    |

## LLM backends

Both Anthropic and OpenAI are supported. Transient API errors (429, 5xx) are
retried with exponential backoff.

Set `provider.backend` to `anthropic` or `openai` and ensure the corresponding
API key environment variable is set.

## Git integration

When `git.enabled` is true, each run creates a branch (e.g.
`research/20260407-143022`). Improvements are committed; failed experiments are
reverted. Results are logged to `results.tsv`.

## License

MIT
