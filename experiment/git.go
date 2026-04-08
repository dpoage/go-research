package experiment

import (
	"fmt"
	"os/exec"
	"strings"
	"time"
)

// Git wraps git operations for experiment tracking.
type Git struct {
	enabled bool
	dir     string
	files   []string // files to stage on commit (scoped to sandbox)
}

// NewGit creates a Git helper. If enabled is false, all operations are no-ops.
// The files parameter controls which paths are staged on commit.
func NewGit(enabled bool, dir string, files []string) *Git {
	return &Git{enabled: enabled, dir: dir, files: files}
}

// CreateBranch creates and checks out a new branch with the given prefix.
// Returns the branch name.
func (g *Git) CreateBranch(prefix string) (string, error) {
	if !g.enabled {
		return "", nil
	}

	name := fmt.Sprintf("%s%s", prefix, time.Now().UTC().Format("20060102-150405"))
	if err := g.gitExec("checkout", "-b", name); err != nil {
		return "", fmt.Errorf("create branch %s: %w", name, err)
	}
	return name, nil
}

// Commit stages the allowed files and commits with the given message.
// Additional paths can be passed to stage alongside the configured files
// (e.g., the results log).
func (g *Git) Commit(msg string, extraFiles ...string) error {
	if !g.enabled {
		return nil
	}

	// Stage only the allowed files plus any extras (e.g., results.tsv).
	toStage := append(g.files, extraFiles...)
	if len(toStage) > 0 {
		args := append([]string{"add", "--"}, toStage...)
		if err := g.gitExec(args...); err != nil {
			return fmt.Errorf("git add: %w", err)
		}
	}

	// Check if there's anything to commit.
	if err := g.gitExec("diff", "--cached", "--quiet"); err == nil {
		return nil // nothing staged
	}

	if err := g.gitExec("commit", "-m", msg); err != nil {
		return fmt.Errorf("git commit: %w", err)
	}
	return nil
}

// Revert discards all uncommitted changes in tracked files and removes untracked files.
func (g *Git) Revert() error {
	if !g.enabled {
		return nil
	}

	if err := g.gitExec("checkout", "."); err != nil {
		return fmt.Errorf("git checkout .: %w", err)
	}
	if err := g.gitExec("clean", "-fd"); err != nil {
		return fmt.Errorf("git clean: %w", err)
	}
	return nil
}

// CurrentBranch returns the name of the current branch.
func (g *Git) CurrentBranch() (string, error) {
	if !g.enabled {
		return "", nil
	}

	out, err := g.gitOutput("rev-parse", "--abbrev-ref", "HEAD")
	if err != nil {
		return "", fmt.Errorf("get current branch: %w", err)
	}
	return strings.TrimSpace(out), nil
}

// CheckoutBranch switches to the named branch.
func (g *Git) CheckoutBranch(name string) error {
	if !g.enabled {
		return nil
	}
	return g.gitExec("checkout", name)
}

func (g *Git) newCmd(args ...string) *exec.Cmd {
	cmd := exec.Command("git", args...)
	if g.dir != "" {
		cmd.Dir = g.dir
	}
	return cmd
}

func (g *Git) gitExec(args ...string) error {
	cmd := g.newCmd(args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("%s: %s", err, strings.TrimSpace(string(out)))
	}
	return nil
}

func (g *Git) gitOutput(args ...string) (string, error) {
	out, err := g.newCmd(args...).Output()
	if err != nil {
		return "", err
	}
	return string(out), nil
}
