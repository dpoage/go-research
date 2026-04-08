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
}

// NewGit creates a Git helper. If enabled is false, all operations are no-ops.
func NewGit(enabled bool) *Git {
	return &Git{enabled: enabled}
}

// CreateBranch creates and checks out a new branch with the given prefix.
// Returns the branch name.
func (g *Git) CreateBranch(prefix string) (string, error) {
	if !g.enabled {
		return "", nil
	}

	name := fmt.Sprintf("%s%s", prefix, time.Now().UTC().Format("20060102-150405"))
	if err := gitExec("checkout", "-b", name); err != nil {
		return "", fmt.Errorf("create branch %s: %w", name, err)
	}
	return name, nil
}

// Commit stages all changes and commits with the given message.
func (g *Git) Commit(msg string) error {
	if !g.enabled {
		return nil
	}

	if err := gitExec("add", "-A"); err != nil {
		return fmt.Errorf("git add: %w", err)
	}

	// Check if there's anything to commit.
	if err := gitExec("diff", "--cached", "--quiet"); err == nil {
		return nil // nothing staged
	}

	if err := gitExec("commit", "-m", msg); err != nil {
		return fmt.Errorf("git commit: %w", err)
	}
	return nil
}

// Revert discards all uncommitted changes in tracked files and removes untracked files.
func (g *Git) Revert() error {
	if !g.enabled {
		return nil
	}

	if err := gitExec("checkout", "."); err != nil {
		return fmt.Errorf("git checkout .: %w", err)
	}
	if err := gitExec("clean", "-fd"); err != nil {
		return fmt.Errorf("git clean: %w", err)
	}
	return nil
}

// CurrentBranch returns the name of the current branch.
func (g *Git) CurrentBranch() (string, error) {
	if !g.enabled {
		return "", nil
	}

	out, err := gitOutput("rev-parse", "--abbrev-ref", "HEAD")
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
	return gitExec("checkout", name)
}

func gitExec(args ...string) error {
	cmd := exec.Command("git", args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("%s: %s", err, strings.TrimSpace(string(out)))
	}
	return nil
}

func gitOutput(args ...string) (string, error) {
	cmd := exec.Command("git", args...)
	out, err := cmd.Output()
	if err != nil {
		return "", err
	}
	return string(out), nil
}
