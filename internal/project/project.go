package project

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

var ErrNotGitProject = errors.New("not inside a Git project")

func FindRoot(start string) (string, error) {
	absStart, err := filepath.Abs(start)
	if err != nil {
		return "", fmt.Errorf("resolve start directory: %w", err)
	}
	info, err := os.Stat(absStart)
	if err != nil {
		return "", fmt.Errorf("stat start directory: %w", err)
	}
	current := absStart
	if !info.IsDir() {
		current = filepath.Dir(current)
	}

	for {
		_, err := os.Lstat(filepath.Join(current, ".git"))
		if err == nil {
			return current, nil
		}
		if !errors.Is(err, os.ErrNotExist) {
			return "", fmt.Errorf("inspect %s: %w", current, err)
		}
		parent := filepath.Dir(current)
		if parent == current {
			return "", fmt.Errorf("%w: %s", ErrNotGitProject, absStart)
		}
		current = parent
	}
}
