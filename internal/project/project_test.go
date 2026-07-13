package project

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestFindRootUsesNearestGitProject(t *testing.T) {
	outer := t.TempDir()
	if err := os.Mkdir(filepath.Join(outer, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	inner := filepath.Join(outer, "packages", "app")
	if err := os.MkdirAll(filepath.Join(inner, "src", "feature"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(inner, ".git"), []byte("gitdir: /tmp/worktree\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	got, err := FindRoot(filepath.Join(inner, "src", "feature"))
	if err != nil {
		t.Fatalf("FindRoot() error = %v", err)
	}
	if got != inner {
		t.Fatalf("FindRoot() = %q, want %q", got, inner)
	}
}

func TestFindRootRejectsDirectoryOutsideGitProject(t *testing.T) {
	_, err := FindRoot(t.TempDir())
	if !errors.Is(err, ErrNotGitProject) {
		t.Fatalf("FindRoot() error = %v, want ErrNotGitProject", err)
	}
}
