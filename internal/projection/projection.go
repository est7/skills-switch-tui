package projection

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/est7/skills-switch-tui/internal/catalog"
	"github.com/est7/skills-switch-tui/internal/client"
)

type Manager struct {
	projectRoot string
	clients     client.Registry
}

func New(projectRoot string, clients client.Registry) Manager {
	return Manager{projectRoot: projectRoot, clients: clients}
}

type State string

const (
	StateDisabled            State = "disabled"
	StateEnabled             State = "enabled"
	StateIncompatible        State = "incompatible"
	StateIncompatibleEnabled State = "incompatible_enabled"
	StateConflict            State = "conflict"
	StateBroken              State = "broken"
)

func (m Manager) State(skill catalog.Skill, client catalog.Client) (State, error) {
	supported := skill.Supports(client)
	linkPath, err := m.TargetPath(skill, client)
	if err != nil {
		return "", err
	}
	info, err := os.Lstat(linkPath)
	if errors.Is(err, os.ErrNotExist) {
		if !supported {
			return StateIncompatible, nil
		}
		return StateDisabled, nil
	}
	if err != nil {
		return "", fmt.Errorf("inspect projection %s: %w", linkPath, err)
	}
	if info.Mode()&os.ModeSymlink == 0 {
		return StateConflict, nil
	}
	target, err := os.Readlink(linkPath)
	if err != nil {
		return StateBroken, nil
	}
	if !filepath.IsAbs(target) {
		target = filepath.Join(filepath.Dir(linkPath), target)
	}
	if filepath.Clean(target) != filepath.Clean(skill.Path) {
		return StateConflict, nil
	}
	if _, err := os.Stat(filepath.Join(target, "SKILL.md")); err != nil {
		return StateBroken, nil
	}
	if !supported {
		return StateIncompatibleEnabled, nil
	}
	return StateEnabled, nil
}

func (m Manager) TargetPath(skill catalog.Skill, client catalog.Client) (string, error) {
	targetDir, err := m.clients.TargetDir(m.projectRoot, client)
	if err != nil {
		return "", err
	}
	return filepath.Join(targetDir, skill.Name), nil
}

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
		parts = append(parts, fmt.Sprintf("%s: %s", conflict.Path, conflict.Reason))
	}
	return "projection conflicts: " + strings.Join(parts, "; ")
}

type action int

const (
	createLink action = iota
	removeLink
)

type change struct {
	action         action
	path           string
	target         string
	originalTarget string
}

type Operation struct {
	Skills  []catalog.Skill
	Client  catalog.Client
	Enabled bool
}

func (m Manager) SetEnabled(skills []catalog.Skill, client catalog.Client, enabled bool) error {
	return m.Apply([]Operation{{Skills: skills, Client: client, Enabled: enabled}})
}

func (m Manager) Apply(operations []Operation) error {
	changes := make([]change, 0)
	conflicts := make([]Conflict, 0)
	for _, operation := range operations {
		targetDir, err := m.clients.TargetDir(m.projectRoot, operation.Client)
		if err != nil {
			return err
		}
		planned, operationConflicts := plan(operation.Skills, operation.Client, targetDir, operation.Enabled)
		changes = append(changes, planned...)
		conflicts = append(conflicts, operationConflicts...)
	}
	if len(conflicts) > 0 {
		return &ConflictError{Conflicts: conflicts}
	}
	if len(changes) == 0 {
		return nil
	}

	createdDirectories := make([]string, 0)
	for _, next := range changes {
		if next.action != createLink {
			continue
		}
		created, err := ensureDirectory(filepath.Dir(next.path))
		if err != nil {
			cleanupDirectories(append(createdDirectories, created...))
			return fmt.Errorf("create projection directory: %w", err)
		}
		createdDirectories = append(createdDirectories, created...)
	}

	executed := make([]change, 0, len(changes))
	for _, next := range changes {
		if err := apply(next); err != nil {
			rollbackErr := rollback(executed)
			cleanupDirectories(createdDirectories)
			operationErr := fmt.Errorf("apply projection change %s: %w", next.path, err)
			if rollbackErr != nil {
				return errors.Join(operationErr, fmt.Errorf("rollback projection: %w", rollbackErr))
			}
			return operationErr
		}
		executed = append(executed, next)
	}
	return nil
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

func cleanupDirectories(paths []string) {
	for _, path := range paths {
		_ = os.Remove(path)
	}
}

func plan(skills []catalog.Skill, client catalog.Client, targetDir string, enabled bool) ([]change, []Conflict) {
	changes := make([]change, 0, len(skills))
	conflicts := make([]Conflict, 0)
	seenNames := make(map[string]string)

	for _, skill := range skills {
		linkPath := filepath.Join(targetDir, skill.Name)
		if previousID, exists := seenNames[skill.Name]; exists && previousID != skill.ID {
			conflicts = append(conflicts, Conflict{Path: linkPath, Reason: "multiple selected skills use the same name"})
			continue
		}
		seenNames[skill.Name] = skill.ID

		if enabled && !skill.Supports(client) {
			conflicts = append(conflicts, Conflict{Path: linkPath, Reason: "skill is incompatible with " + string(client)})
			continue
		}
		if info, err := os.Stat(filepath.Join(skill.Path, "SKILL.md")); enabled && (err != nil || info.IsDir()) {
			reason := "source SKILL.md is unavailable"
			if err != nil {
				reason += ": " + err.Error()
			}
			conflicts = append(conflicts, Conflict{Path: linkPath, Reason: reason})
			continue
		}

		info, err := os.Lstat(linkPath)
		if errors.Is(err, os.ErrNotExist) {
			if enabled {
				changes = append(changes, change{action: createLink, path: linkPath, target: skill.Path})
			}
			continue
		}
		if err != nil {
			conflicts = append(conflicts, Conflict{Path: linkPath, Reason: err.Error()})
			continue
		}
		if info.Mode()&os.ModeSymlink == 0 {
			conflicts = append(conflicts, Conflict{Path: linkPath, Reason: "path exists and is not a symlink"})
			continue
		}

		originalTarget, err := os.Readlink(linkPath)
		if err != nil {
			conflicts = append(conflicts, Conflict{Path: linkPath, Reason: "read symlink: " + err.Error()})
			continue
		}
		resolvedTarget := originalTarget
		if !filepath.IsAbs(resolvedTarget) {
			resolvedTarget = filepath.Join(filepath.Dir(linkPath), resolvedTarget)
		}
		if filepath.Clean(resolvedTarget) != filepath.Clean(skill.Path) {
			conflicts = append(conflicts, Conflict{Path: linkPath, Reason: "symlink is not managed by this skill"})
			continue
		}
		if !enabled {
			changes = append(changes, change{
				action:         removeLink,
				path:           linkPath,
				target:         skill.Path,
				originalTarget: originalTarget,
			})
		}
	}

	return changes, conflicts
}

func apply(next change) error {
	switch next.action {
	case createLink:
		return os.Symlink(next.target, next.path)
	case removeLink:
		return os.Remove(next.path)
	default:
		return errors.New("unknown projection action")
	}
}

func rollback(executed []change) error {
	var rollbackErrors []error
	for index := len(executed) - 1; index >= 0; index-- {
		previous := executed[index]
		var err error
		switch previous.action {
		case createLink:
			err = os.Remove(previous.path)
		case removeLink:
			err = os.Symlink(previous.originalTarget, previous.path)
		}
		if err != nil {
			rollbackErrors = append(rollbackErrors, fmt.Errorf("restore %s: %w", previous.path, err))
		}
	}
	return errors.Join(rollbackErrors...)
}
