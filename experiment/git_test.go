package experiment

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func initTestRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()

	cmds := [][]string{
		{"git", "init"},
		{"git", "config", "user.email", "test@test.com"},
		{"git", "config", "user.name", "Test"},
	}
	for _, args := range cmds {
		cmd := exec.Command(args[0], args[1:]...)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("%v failed: %s: %s", args, err, out)
		}
	}

	// Create initial commit so we have a branch.
	f := filepath.Join(dir, "README.md")
	if err := os.WriteFile(f, []byte("# test\n"), 0644); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{
		{"git", "add", "."},
		{"git", "commit", "-m", "initial"},
	} {
		cmd := exec.Command(args[0], args[1:]...)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("%v failed: %s: %s", args, err, out)
		}
	}

	return dir
}

func chdir(t *testing.T, dir string) {
	t.Helper()
	orig, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chdir(orig) })
}

func TestGit_Disabled(t *testing.T) {
	g := NewGit(false, "", nil)

	branch, err := g.CreateBranch("test/")
	if err != nil {
		t.Fatal(err)
	}
	if branch != "" {
		t.Errorf("expected empty branch name when disabled, got %q", branch)
	}

	if err := g.Commit("test"); err != nil {
		t.Fatal(err)
	}
	if err := g.Revert(); err != nil {
		t.Fatal(err)
	}
}

func TestGit_CreateBranch(t *testing.T) {
	dir := initTestRepo(t)
	chdir(t, dir)

	g := NewGit(true, dir, []string{"README.md"})
	branch, err := g.CreateBranch("research/")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(branch, "research/") {
		t.Errorf("branch %q doesn't have expected prefix", branch)
	}

	current, err := g.CurrentBranch()
	if err != nil {
		t.Fatal(err)
	}
	if current != branch {
		t.Errorf("current branch = %q, want %q", current, branch)
	}
}

func TestGit_CommitAndRevert(t *testing.T) {
	dir := initTestRepo(t)
	chdir(t, dir)

	g := NewGit(true, dir, []string{"README.md"})

	// Modify a file and commit.
	target := filepath.Join(dir, "README.md")
	if err := os.WriteFile(target, []byte("modified\n"), 0644); err != nil {
		t.Fatal(err)
	}

	if err := g.Commit("test commit"); err != nil {
		t.Fatal(err)
	}

	// Verify committed.
	cmd := exec.Command("git", "log", "--oneline", "-1")
	cmd.Dir = dir
	out, err := cmd.Output()
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(out), "test commit") {
		t.Errorf("commit not found in log: %s", out)
	}

	// Now modify again and revert.
	if err := os.WriteFile(target, []byte("will be reverted\n"), 0644); err != nil {
		t.Fatal(err)
	}

	if err := g.Revert(); err != nil {
		t.Fatal(err)
	}

	data, err := os.ReadFile(target)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "modified\n" {
		t.Errorf("revert failed, got %q", data)
	}
}

func TestGit_CommitNoChanges(t *testing.T) {
	dir := initTestRepo(t)
	chdir(t, dir)

	g := NewGit(true, dir, []string{"README.md"})

	// Commit with no changes should be a no-op.
	if err := g.Commit("nothing"); err != nil {
		t.Fatal(err)
	}
}
