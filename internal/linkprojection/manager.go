package linkprojection

import (
	"errors"
	"os"
	"path/filepath"
	"strings"

	"github.com/est7/skills-switch-tui/internal/linktransaction"
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

type Manager struct {
	Label string
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
	changes := make([]linktransaction.Change, 0)
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
		_, legacyManaged, legacyErr := matchingLegacySource(file.TargetPath, file.LegacySourcePaths)
		if legacyErr != nil {
			conflicts = append(conflicts, Conflict{Path: file.TargetPath, Reason: legacyErr.Error()})
			continue
		}
		if conflict && !legacyManaged {
			conflicts = append(conflicts, Conflict{Path: file.TargetPath, Reason: "target is not this managed symlink"})
			continue
		}
		if legacyManaged {
			originalTarget, readErr := os.Readlink(file.TargetPath)
			if readErr != nil {
				conflicts = append(conflicts, Conflict{Path: file.TargetPath, Reason: "read managed legacy link: " + readErr.Error()})
				continue
			}
			if enabled {
				changes = append(changes, linktransaction.Replace(file.TargetPath, originalTarget, file.SourcePath))
			} else {
				changes = append(changes, linktransaction.Remove(file.TargetPath, originalTarget))
			}
			continue
		}
		if enabled && !exists {
			changes = append(changes, linktransaction.Create(file.TargetPath, file.SourcePath))
		}
		if !enabled && exists && matches {
			originalTarget, readErr := os.Readlink(file.TargetPath)
			if readErr != nil {
				conflicts = append(conflicts, Conflict{Path: file.TargetPath, Reason: "read managed link: " + readErr.Error()})
				continue
			}
			changes = append(changes, linktransaction.Remove(file.TargetPath, originalTarget))
		}
	}
	if len(conflicts) > 0 {
		return m.conflictError(conflicts)
	}

	engine := linktransaction.Engine{
		Label:       m.label(),
		MatchTarget: linktransaction.EquivalentTarget,
		Conflict: func(path, reason string) error {
			return m.conflictError([]Conflict{{Path: path, Reason: reason}})
		},
		ValidateTarget: func(target string) error {
			info, err := os.Stat(target)
			if err == nil && info.Mode().IsRegular() {
				return nil
			}
			reason := "source file changed after preflight"
			if err != nil {
				reason += ": " + err.Error()
			}
			return m.conflictError([]Conflict{{Path: target, Reason: reason}})
		},
	}
	_, err := engine.Execute(changes)
	return err
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
