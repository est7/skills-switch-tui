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
	worktreeGitDir := filepath.Join(outer, ".git", "worktrees", "app")
	if err := os.MkdirAll(worktreeGitDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(inner, "src", "feature"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(inner, ".git"), []byte("gitdir: "+worktreeGitDir+"\n"), 0o644); err != nil {
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

func TestFindRootIgnoresArbitraryDotGitFile(t *testing.T) {
	outer := t.TempDir()
	if err := os.Mkdir(filepath.Join(outer, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	inner := filepath.Join(outer, "nested")
	if err := os.MkdirAll(inner, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(inner, ".git"), []byte("not a gitdir\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	got, err := FindRoot(inner)
	if err != nil {
		t.Fatal(err)
	}
	if got != outer {
		t.Fatalf("FindRoot() = %q, want validated parent %q", got, outer)
	}
}

func TestFindRootRejectsDirectoryOutsideGitProject(t *testing.T) {
	_, err := FindRoot(t.TempDir())
	if !errors.Is(err, ErrNotGitProject) {
		t.Fatalf("FindRoot() error = %v, want ErrNotGitProject", err)
	}
}
