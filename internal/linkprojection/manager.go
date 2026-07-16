package linkprojection

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

type State string

const (
	StateDisabled State = "disabled"
	StateEnabled  State = "enabled"
	StatePartial  State = "partial"
	StateConflict State = "conflict"
	StateBroken   State = "broken"
)

type File struct {
	SourcePath        string
	TargetPath        string
	LegacySourcePaths []string
}

type Conflict struct {
	Path   string
	Reason string
}

type ConflictError struct {
	Label     string
	Conflicts []Conflict
}

func (e *ConflictError) Error() string {
	parts := make([]string, 0, len(e.Conflicts))
	for _, conflict := range e.Conflicts {
		parts = append(parts, conflict.Path+": "+conflict.Reason)
	}
	return e.Label + " conflicts: " + strings.Join(parts, "; ")
}

type Action int

const (
	CreateLink Action = iota
	RemoveLink
)

type Change struct {
	Action Action
	Path   string
	Target string
}

type Manager struct {
	Label       string
	BeforeApply func(Change)
}

func (m Manager) State(files []File) (State, error) {
	enabled := 0
	for _, file := range files {
		if info, err := os.Stat(file.SourcePath); err != nil || !info.Mode().IsRegular() {
			return StateBroken, nil
		}
		matches, exists, conflict, err := inspectLink(file.TargetPath, file.SourcePath)
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
	case enabled == len(files):
		return StateEnabled, nil
	default:
		return StatePartial, nil
	}
}

func (m Manager) SetEnabled(files []File, enabled bool) error {
	changes := make([]Change, 0)
	conflicts := make([]Conflict, 0)
	seenTargets := make(map[string]string)
	for _, file := range files {
		if enabled {
			info, err := os.Stat(file.SourcePath)
			if err != nil || !info.Mode().IsRegular() {
				reason := "source file is unavailable"
				if err != nil {
					reason += ": " + err.Error()
				}
				conflicts = append(conflicts, Conflict{Path: file.SourcePath, Reason: reason})
				continue
			}
		}
		if previous, exists := seenTargets[file.TargetPath]; exists && filepath.Clean(previous) != filepath.Clean(file.SourcePath) {
			conflicts = append(conflicts, Conflict{Path: file.TargetPath, Reason: "multiple sources map to the same target"})
			continue
		}
		seenTargets[file.TargetPath] = file.SourcePath
		matches, exists, conflict, err := inspectLink(file.TargetPath, file.SourcePath)
		if err != nil {
			conflicts = append(conflicts, Conflict{Path: file.TargetPath, Reason: err.Error()})
			continue
		}
		legacySource, legacyManaged, legacyErr := matchingLegacySource(file.TargetPath, file.LegacySourcePaths)
		if legacyErr != nil {
			conflicts = append(conflicts, Conflict{Path: file.TargetPath, Reason: legacyErr.Error()})
			continue
		}
		if conflict && !legacyManaged {
			conflicts = append(conflicts, Conflict{Path: file.TargetPath, Reason: "target is not this managed symlink"})
			continue
		}
		if legacyManaged {
			changes = append(changes, Change{Action: RemoveLink, Path: file.TargetPath, Target: legacySource})
			if enabled {
				changes = append(changes, Change{Action: CreateLink, Path: file.TargetPath, Target: file.SourcePath})
			}
			continue
		}
		if enabled && !exists {
			changes = append(changes, Change{Action: CreateLink, Path: file.TargetPath, Target: file.SourcePath})
		}
		if !enabled && exists && matches {
			changes = append(changes, Change{Action: RemoveLink, Path: file.TargetPath, Target: file.SourcePath})
		}
	}
	if len(conflicts) > 0 {
		return m.conflictError(conflicts)
	}

	createdDirs := make([]string, 0)
	executed := make([]Change, 0, len(changes))
	for _, next := range changes {
		if m.BeforeApply != nil {
			m.BeforeApply(next)
		}
		if err := m.validateChange(next); err != nil {
			return m.failApply(err, executed, createdDirs)
		}
		if next.Action == CreateLink {
			created, err := ensureDirectory(filepath.Dir(next.Path))
			if err != nil {
				return m.failApply(fmt.Errorf("create %s directory: %w", m.label(), err), executed, createdDirs)
			}
			createdDirs = append(createdDirs, created...)
			if err := m.validateChange(next); err != nil {
				return m.failApply(err, executed, createdDirs)
			}
			if err := os.Symlink(next.Target, next.Path); err != nil {
				return m.failApply(fmt.Errorf("create %s link %s: %w", m.label(), next.Path, err), executed, createdDirs)
			}
		} else if err := os.Remove(next.Path); err != nil {
			return m.failApply(fmt.Errorf("remove %s link %s: %w", m.label(), next.Path, err), executed, createdDirs)
		}
		executed = append(executed, next)
	}
	return nil
}

func matchingLegacySource(path string, sources []string) (string, bool, error) {
	for _, source := range sources {
		matches, exists, conflict, err := inspectLink(path, source)
		if err != nil {
			return "", false, err
		}
		if exists && matches && !conflict {
			return source, true, nil
		}
	}
	return "", false, nil
}

func (m Manager) validateChange(next Change) error {
	if next.Action == CreateLink {
		info, err := os.Stat(next.Target)
		if err != nil || !info.Mode().IsRegular() {
			reason := "source file changed after preflight"
			if err != nil {
				reason += ": " + err.Error()
			}
			return m.conflictError([]Conflict{{Path: next.Target, Reason: reason}})
		}
	}
	matches, exists, conflict, err := inspectLink(next.Path, next.Target)
	if err != nil {
		return m.conflictError([]Conflict{{Path: next.Path, Reason: "inspect after preflight: " + err.Error()}})
	}
	valid := next.Action == CreateLink && !exists && !conflict || next.Action == RemoveLink && exists && matches && !conflict
	if valid {
		return nil
	}
	return m.conflictError([]Conflict{{Path: next.Path, Reason: "target changed after preflight"}})
}

func (m Manager) failApply(applyErr error, executed []Change, createdDirs []string) error {
	rollbackErr := rollbackChanges(executed)
	cleanupDirectories(createdDirs)
	if rollbackErr == nil {
		return applyErr
	}
	return errors.Join(applyErr, fmt.Errorf("rollback %s changes: %w", m.label(), rollbackErr))
}

func (m Manager) conflictError(conflicts []Conflict) error {
	return &ConflictError{Label: m.label(), Conflicts: conflicts}
}

func (m Manager) label() string {
	if strings.TrimSpace(m.Label) == "" {
		return "file projection"
	}
	return m.Label
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

func rollbackChanges(changes []Change) error {
	var rollbackErrors []error
	for index := len(changes) - 1; index >= 0; index-- {
		next := changes[index]
		matches, exists, conflict, err := inspectLink(next.Path, next.Target)
		if err != nil {
			rollbackErrors = append(rollbackErrors, fmt.Errorf("inspect %s: %w", next.Path, err))
			continue
		}
		if next.Action == CreateLink {
			if !exists {
				continue
			}
			if conflict || !matches {
				rollbackErrors = append(rollbackErrors, fmt.Errorf("preserve concurrently changed target %s", next.Path))
				continue
			}
			if err := os.Remove(next.Path); err != nil {
				rollbackErrors = append(rollbackErrors, fmt.Errorf("remove %s: %w", next.Path, err))
			}
			continue
		}
		if exists {
			if conflict || !matches {
				rollbackErrors = append(rollbackErrors, fmt.Errorf("preserve concurrently changed target %s", next.Path))
			}
			continue
		}
		if err := os.Symlink(next.Target, next.Path); err != nil {
			rollbackErrors = append(rollbackErrors, fmt.Errorf("restore %s: %w", next.Path, err))
		}
	}
	return errors.Join(rollbackErrors...)
}

func cleanupDirectories(paths []string) {
	for _, path := range paths {
		_ = os.Remove(path)
	}
}
