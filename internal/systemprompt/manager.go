package systemprompt

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/est7/skills-switch-tui/internal/client"
)

type State string

const (
	StateDisabled State = "disabled"
	StateEnabled  State = "enabled"
	StatePartial  State = "partial"
	StateConflict State = "conflict"
	StateBroken   State = "broken"
)

type Conflict struct {
	Path   string
	Reason string
}

type ConflictError struct {
	Conflicts []Conflict
}

func (e *ConflictError) Error() string {
	parts := make([]string, 0, len(e.Conflicts))
	for _, conflict := range e.Conflicts {
		parts = append(parts, conflict.Path+": "+conflict.Reason)
	}
	return "system prompt conflicts: " + strings.Join(parts, "; ")
}

type Manager struct {
	userHome    string
	clients     client.Registry
	beforeApply func(change)
}

func NewManager(userHome string, clients client.Registry) Manager {
	return Manager{userHome: userHome, clients: clients}
}

func (m Manager) State(group Group) (State, error) {
	targetRoot, err := m.clients.UserPromptTargetDir(m.userHome, group.Client)
	if err != nil {
		return "", err
	}
	enabled := 0
	for _, file := range group.Files {
		if info, err := os.Stat(file.SourcePath); err != nil || !info.Mode().IsRegular() {
			return StateBroken, nil
		}
		target := filepath.Join(targetRoot, file.RelativePath)
		matches, exists, conflict, err := inspectLink(target, file.SourcePath)
		if err != nil {
			return "", err
		}
		if conflict {
			return StateConflict, nil
		}
		if exists && matches {
			enabled++
		}
	}
	switch {
	case enabled == 0:
		return StateDisabled, nil
	case enabled == len(group.Files):
		return StateEnabled, nil
	default:
		return StatePartial, nil
	}
}

type changeAction int

const (
	createLink changeAction = iota
	removeLink
)

type change struct {
	action changeAction
	path   string
	target string
}

func (m Manager) SetEnabled(groups []Group, enabled bool) error {
	changes := make([]change, 0)
	conflicts := make([]Conflict, 0)
	seenTargets := make(map[string]string)
	for _, group := range groups {
		targetRoot, err := m.clients.UserPromptTargetDir(m.userHome, group.Client)
		if err != nil {
			return err
		}
		for _, file := range group.Files {
			info, err := os.Stat(file.SourcePath)
			if err != nil || !info.Mode().IsRegular() {
				reason := "source file is unavailable"
				if err != nil {
					reason += ": " + err.Error()
				}
				conflicts = append(conflicts, Conflict{Path: file.SourcePath, Reason: reason})
				continue
			}
			target := filepath.Join(targetRoot, file.RelativePath)
			if previous, exists := seenTargets[target]; exists && filepath.Clean(previous) != filepath.Clean(file.SourcePath) {
				conflicts = append(conflicts, Conflict{Path: target, Reason: "multiple prompt sources map to the same target"})
				continue
			}
			seenTargets[target] = file.SourcePath
			matches, exists, conflict, err := inspectLink(target, file.SourcePath)
			if err != nil {
				conflicts = append(conflicts, Conflict{Path: target, Reason: err.Error()})
				continue
			}
			if conflict {
				conflicts = append(conflicts, Conflict{Path: target, Reason: "target is not this managed symlink"})
				continue
			}
			if enabled && !exists {
				changes = append(changes, change{action: createLink, path: target, target: file.SourcePath})
			}
			if !enabled && exists && matches {
				changes = append(changes, change{action: removeLink, path: target, target: file.SourcePath})
			}
		}
	}
	if len(conflicts) > 0 {
		return &ConflictError{Conflicts: conflicts}
	}

	createdDirs := make([]string, 0)
	executed := make([]change, 0, len(changes))
	for _, next := range changes {
		if m.beforeApply != nil {
			m.beforeApply(next)
		}
		if err := validateChange(next); err != nil {
			return failApply(err, executed, createdDirs)
		}
		if next.action == createLink {
			created, err := ensureDirectory(filepath.Dir(next.path))
			if err != nil {
				return failApply(fmt.Errorf("create system prompt directory: %w", err), executed, createdDirs)
			}
			createdDirs = append(createdDirs, created...)
			if err := validateChange(next); err != nil {
				return failApply(err, executed, createdDirs)
			}
			if err := os.Symlink(next.target, next.path); err != nil {
				return failApply(fmt.Errorf("create system prompt link %s: %w", next.path, err), executed, createdDirs)
			}
		} else if err := os.Remove(next.path); err != nil {
			return failApply(fmt.Errorf("remove system prompt link %s: %w", next.path, err), executed, createdDirs)
		}
		executed = append(executed, next)
	}
	return nil
}

func validateChange(next change) error {
	info, err := os.Stat(next.target)
	if err != nil || !info.Mode().IsRegular() {
		reason := "source file changed after preflight"
		if err != nil {
			reason += ": " + err.Error()
		}
		return &ConflictError{Conflicts: []Conflict{{Path: next.target, Reason: reason}}}
	}
	matches, exists, conflict, err := inspectLink(next.path, next.target)
	if err != nil {
		return &ConflictError{Conflicts: []Conflict{{Path: next.path, Reason: "inspect after preflight: " + err.Error()}}}
	}
	valid := next.action == createLink && !exists && !conflict || next.action == removeLink && exists && matches && !conflict
	if valid {
		return nil
	}
	return &ConflictError{Conflicts: []Conflict{{Path: next.path, Reason: "target changed after preflight"}}}
}

func failApply(applyErr error, executed []change, createdDirs []string) error {
	rollbackErr := rollbackChanges(executed)
	cleanupDirectories(createdDirs)
	if rollbackErr == nil {
		return applyErr
	}
	return errors.Join(applyErr, fmt.Errorf("rollback system prompt changes: %w", rollbackErr))
}

func inspectLink(path, source string) (matches, exists, conflict bool, err error) {
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return false, false, false, nil
	}
	if err != nil {
		return false, false, false, err
	}
	if info.Mode()&os.ModeSymlink == 0 {
		return false, true, true, nil
	}
	target, err := os.Readlink(path)
	if err != nil {
		return false, true, false, err
	}
	if !filepath.IsAbs(target) {
		target = filepath.Join(filepath.Dir(path), target)
	}
	if filepath.Clean(target) != filepath.Clean(source) {
		return false, true, true, nil
	}
	return true, true, false, nil
}

func ensureDirectory(path string) ([]string, error) {
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

func rollbackChanges(changes []change) error {
	var rollbackErrors []error
	for index := len(changes) - 1; index >= 0; index-- {
		next := changes[index]
		matches, exists, conflict, err := inspectLink(next.path, next.target)
		if err != nil {
			rollbackErrors = append(rollbackErrors, fmt.Errorf("inspect %s: %w", next.path, err))
			continue
		}
		if next.action == createLink {
			if !exists {
				continue
			}
			if conflict || !matches {
				rollbackErrors = append(rollbackErrors, fmt.Errorf("preserve concurrently changed target %s", next.path))
				continue
			}
			if err := os.Remove(next.path); err != nil {
				rollbackErrors = append(rollbackErrors, fmt.Errorf("remove %s: %w", next.path, err))
			}
			continue
		}
		if exists {
			if conflict || !matches {
				rollbackErrors = append(rollbackErrors, fmt.Errorf("preserve concurrently changed target %s", next.path))
			}
			continue
		}
		if err := os.Symlink(next.target, next.path); err != nil {
			rollbackErrors = append(rollbackErrors, fmt.Errorf("restore %s: %w", next.path, err))
		}
	}
	return errors.Join(rollbackErrors...)
}

func cleanupDirectories(paths []string) {
	for _, path := range paths {
		_ = os.Remove(path)
	}
}
