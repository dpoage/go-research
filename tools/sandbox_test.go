package tools

import (
	"testing"
)

func TestSandbox_CheckWrite_Allowed(t *testing.T) {
	sb, err := NewSandbox("/project", []string{"src/main.py", "src/utils.py"})
	if err != nil {
		t.Fatal(err)
	}

	if err := sb.CheckWrite("src/main.py"); err != nil {
		t.Errorf("expected write to allowed file to succeed, got: %v", err)
	}
}

func TestSandbox_CheckWrite_Denied(t *testing.T) {
	sb, err := NewSandbox("/project", []string{"src/main.py"})
	if err != nil {
		t.Fatal(err)
	}

	if err := sb.CheckWrite("src/secret.py"); err == nil {
		t.Error("expected write to non-allowed file to be denied")
	}
}

func TestSandbox_CheckWrite_AbsolutePath(t *testing.T) {
	sb, err := NewSandbox("/project", []string{"src/main.py"})
	if err != nil {
		t.Fatal(err)
	}

	// Absolute path that resolves to allowed file.
	if err := sb.CheckWrite("/project/src/main.py"); err != nil {
		t.Errorf("expected absolute path to allowed file to succeed, got: %v", err)
	}

	// Absolute path outside allowed set.
	if err := sb.CheckWrite("/etc/passwd"); err == nil {
		t.Error("expected write to /etc/passwd to be denied")
	}
}

func TestSandbox_CheckRead_AlwaysSucceeds(t *testing.T) {
	sb, err := NewSandbox("/project", []string{"src/main.py"})
	if err != nil {
		t.Fatal(err)
	}

	if err := sb.CheckRead("anything.txt"); err != nil {
		t.Errorf("expected read to always succeed, got: %v", err)
	}
}

func TestSandbox_EmptyFileList(t *testing.T) {
	sb, err := NewSandbox("/project", nil)
	if err != nil {
		t.Fatal(err)
	}

	if err := sb.CheckWrite("any_file.txt"); err == nil {
		t.Error("expected all writes denied with empty file list")
	}
}

func TestNewSandbox_AbsoluteFileInList(t *testing.T) {
	dir := t.TempDir()
	absFile := dir + "/absolute.txt"

	// Pass an absolute path in the files list — NewSandbox should store it
	// cleaned and CheckWrite using that same absolute path must succeed.
	sb, err := NewSandbox(dir, []string{absFile})
	if err != nil {
		t.Fatalf("NewSandbox: %v", err)
	}

	if err := sb.CheckWrite(absFile); err != nil {
		t.Errorf("expected write to absolute allowed path to succeed, got: %v", err)
	}

	// A different absolute path must still be denied.
	if err := sb.CheckWrite(dir + "/other.txt"); err == nil {
		t.Error("expected write to unlisted absolute path to be denied")
	}
}
