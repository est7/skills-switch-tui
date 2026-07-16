package project

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
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
		gitEntry := filepath.Join(current, ".git")
		info, err := os.Lstat(gitEntry)
		if err == nil {
			if info.IsDir() {
				return current, nil
			}
			if info.Mode().IsRegular() {
				data, readErr := os.ReadFile(gitEntry)
				gitdir, found := strings.CutPrefix(strings.TrimSpace(string(data)), "gitdir:")
				if found {
					gitdir = strings.TrimSpace(gitdir)
					if !filepath.IsAbs(gitdir) {
						gitdir = filepath.Join(current, gitdir)
					}
					if target, statErr := os.Stat(gitdir); statErr == nil && target.IsDir() {
						return current, nil
					}
				}
				if readErr != nil {
					return "", fmt.Errorf("read %s: %w", gitEntry, readErr)
				}
			}
			err = os.ErrNotExist
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
