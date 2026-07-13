package mcp

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

func resolveConfigPath(path string) (string, error) {
	current, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	seen := make(map[string]bool)
	for range 64 {
		current = filepath.Clean(current)
		if seen[current] {
			return "", fmt.Errorf("symlink loop at %s", current)
		}
		seen[current] = true
		info, err := os.Lstat(current)
		if errors.Is(err, os.ErrNotExist) {
			return current, nil
		}
		if err != nil {
			return "", err
		}
		if info.Mode()&os.ModeSymlink == 0 {
			if !info.Mode().IsRegular() {
				return "", fmt.Errorf("config path is not a regular file: %s", current)
			}
			return current, nil
		}
		target, err := os.Readlink(current)
		if err != nil {
			return "", err
		}
		if !filepath.IsAbs(target) {
			target = filepath.Join(filepath.Dir(current), target)
		}
		current = target
	}
	return "", fmt.Errorf("symlink chain exceeds 64 links: %s", path)
}

func readConfig(path string) ([]byte, bool, os.FileMode, error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, false, 0, nil
	}
	if err != nil {
		return nil, false, 0, err
	}
	info, err := os.Stat(path)
	if err != nil {
		return nil, false, 0, err
	}
	return data, true, info.Mode().Perm(), nil
}

func verifyUnchanged(plan *filePlan) error {
	if !plan.changed {
		return nil
	}
	current, exists, _, err := readConfig(plan.resolvedPath)
	if err != nil {
		return fmt.Errorf("recheck MCP config %s: %w", plan.requestedPath, err)
	}
	if exists != plan.existed || !bytes.Equal(current, plan.original) {
		return fmt.Errorf("MCP config changed during operation: %s", plan.requestedPath)
	}
	return nil
}

func atomicWrite(path string, data []byte, mode os.FileMode) error {
	temporary, err := os.CreateTemp(filepath.Dir(path), ".agents-switch-*")
	if err != nil {
		return err
	}
	temporaryPath := temporary.Name()
	defer os.Remove(temporaryPath)
	if _, err := temporary.Write(data); err != nil {
		temporary.Close()
		return err
	}
	if err := temporary.Chmod(mode.Perm()); err != nil {
		temporary.Close()
		return err
	}
	if err := temporary.Sync(); err != nil {
		temporary.Close()
		return err
	}
	if err := temporary.Close(); err != nil {
		return err
	}
	if err := os.Rename(temporaryPath, path); err != nil {
		return err
	}
	if directory, err := os.Open(filepath.Dir(path)); err == nil {
		_ = directory.Sync()
		_ = directory.Close()
	}
	return nil
}

func ensureParentDirectory(path string) ([]string, error) {
	missing := make([]string, 0)
	for current := path; ; current = filepath.Dir(current) {
		info, err := os.Stat(current)
		if err == nil {
			if !info.IsDir() {
				return nil, fmt.Errorf("%s exists and is not a directory", current)
			}
			break
		}
		if !errors.Is(err, os.ErrNotExist) {
			return nil, err
		}
		missing = append(missing, current)
		parent := filepath.Dir(current)
		if parent == current {
			break
		}
	}
	if err := os.MkdirAll(path, 0o755); err != nil {
		return nil, err
	}
	return missing, nil
}

func cleanupDirectories(paths []string) {
	for _, path := range paths {
		_ = os.Remove(path)
	}
}

func rollbackPlans(plans []*filePlan) error {
	var rollbackErrors []error
	for index := len(plans) - 1; index >= 0; index-- {
		plan := plans[index]
		current, exists, _, err := readConfig(plan.resolvedPath)
		if err != nil {
			rollbackErrors = append(rollbackErrors, err)
			continue
		}
		if !exists || !bytes.Equal(current, plan.result) {
			rollbackErrors = append(rollbackErrors, fmt.Errorf("refuse to overwrite concurrently changed config during rollback: %s", plan.requestedPath))
			continue
		}
		if plan.existed {
			if err := atomicWrite(plan.resolvedPath, plan.original, plan.originalMode); err != nil {
				rollbackErrors = append(rollbackErrors, err)
			}
		} else if err := os.Remove(plan.resolvedPath); err != nil && !errors.Is(err, os.ErrNotExist) {
			rollbackErrors = append(rollbackErrors, err)
		}
		cleanupDirectories(plan.createdDirs)
	}
	return errors.Join(rollbackErrors...)
}
