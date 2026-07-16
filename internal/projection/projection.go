package projection

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/est7/skills-switch-tui/internal/catalog"
	"github.com/est7/skills-switch-tui/internal/client"
	"github.com/est7/skills-switch-tui/internal/linktransaction"
)

type Manager struct {
	projectRoot     string
	userHome        string
	clients         client.Registry
	providersByPath map[string]catalog.Skill
	beforeApply     func(change)
}

func New(projectRoot string, loaded catalog.Catalog) Manager {
	return NewWithUserHome(projectRoot, "", loaded)
}

func NewWithUserHome(projectRoot, userHome string, loaded catalog.Catalog) Manager {
	providersByPath := make(map[string]catalog.Skill)
	for _, source := range loaded.Sources {
		if source.IsArchived() {
			continue
		}
		for _, skill := range source.Skills {
			providersByPath[filepath.Clean(skill.Path)] = skill
		}
	}
	return Manager{
		projectRoot:     projectRoot,
		userHome:        userHome,
		clients:         loaded.Clients,
		providersByPath: providersByPath,
	}
}

type State string

type Scope string

const (
	ScopeProject Scope = "project"
	ScopeGlobal  Scope = "global"
)

// Health is a structured diagnosis of one concrete projection scope. State is
// the stable UI/API category; Cause retains the exact filesystem failure or
// conflict explanation needed by doctor output and troubleshooting.
type Health struct {
	State  State
	Scope  Scope
	Client catalog.Client
	Path   string
	Target string
	Cause  error
}

const (
	StateDisabled            State = "disabled"
	StateEnabled             State = "enabled"
	StateIncompatible        State = "incompatible"
	StateIncompatibleEnabled State = "incompatible_enabled"
	StateConflict            State = "conflict"
	StateBroken              State = "broken"
	StateGlobal              State = "global"
	StateDuplicate           State = "duplicate"
)

func (m Manager) State(skill catalog.Skill, client catalog.Client) (State, error) {
	return m.StateAt(skill, client, ScopeProject)
}

func (m Manager) StateAt(skill catalog.Skill, client catalog.Client, scope Scope) (State, error) {
	definition, _ := m.clients.Definition(client)
	if scope == ScopeProject && m.userHome != "" && definition.UserSkillsDir != "" {
		globalPath, err := m.TargetPathAt(skill, client, ScopeGlobal)
		if err != nil {
			return "", err
		}
		if _, err := os.Lstat(globalPath); err == nil {
			projectPath, pathErr := m.TargetPathAt(skill, client, ScopeProject)
			if pathErr != nil {
				return "", pathErr
			}
			if _, projectErr := os.Lstat(projectPath); projectErr == nil {
				return StateDuplicate, nil
			} else if !errors.Is(projectErr, os.ErrNotExist) {
				return "", fmt.Errorf("inspect project projection %s: %w", projectPath, projectErr)
			}
			globalState, stateErr := m.directState(skill, client, ScopeGlobal)
			if stateErr != nil {
				return "", stateErr
			}
			if globalState == StateEnabled {
				return StateGlobal, nil
			}
			return globalState, nil
		} else if !errors.Is(err, os.ErrNotExist) {
			return "", fmt.Errorf("inspect global projection %s: %w", globalPath, err)
		}
	}
	return m.directState(skill, client, scope)
}

func (m Manager) directState(skill catalog.Skill, client catalog.Client, scope Scope) (State, error) {
	health, err := m.HealthAt(skill, client, scope)
	return health.State, err
}

func (m Manager) HealthAt(skill catalog.Skill, client catalog.Client, scope Scope) (Health, error) {
	supported := skill.Supports(client)
	linkPath, err := m.TargetPathAt(skill, client, scope)
	if err != nil {
		return Health{}, err
	}
	health := Health{Scope: scope, Client: client, Path: linkPath}
	info, err := os.Lstat(linkPath)
	if errors.Is(err, os.ErrNotExist) {
		if !supported {
			health.State = StateIncompatible
			return health, nil
		}
		health.State = StateDisabled
		return health, nil
	}
	if err != nil {
		return Health{}, fmt.Errorf("inspect projection %s: %w", linkPath, err)
	}
	if info.Mode()&os.ModeSymlink == 0 {
		health.State = StateConflict
		health.Cause = fmt.Errorf("path exists and is not a symlink")
		return health, nil
	}
	target, err := os.Readlink(linkPath)
	if err != nil {
		health.State = StateBroken
		health.Cause = fmt.Errorf("read projection target: %w", err)
		return health, nil
	}
	health.Target = target
	if !filepath.IsAbs(target) {
		target = filepath.Join(filepath.Dir(linkPath), target)
	}
	health.Target = filepath.Clean(target)
	if filepath.Clean(target) != filepath.Clean(skill.Path) {
		if m.isManagedAlternative(target, skill) {
			if !supported {
				health.State = StateIncompatible
				return health, nil
			}
			health.State = StateDisabled
			return health, nil
		}
		health.State = StateConflict
		health.Cause = fmt.Errorf("target %s is not the selected provider %s", health.Target, filepath.Clean(skill.Path))
		return health, nil
	}
	if _, err := os.Stat(filepath.Join(target, "SKILL.md")); err != nil {
		health.State = StateBroken
		health.Cause = fmt.Errorf("inspect provider SKILL.md: %w", err)
		return health, nil
	}
	if !supported {
		health.State = StateIncompatibleEnabled
		health.Cause = fmt.Errorf("skill does not support client %s", client)
		return health, nil
	}
	health.State = StateEnabled
	return health, nil
}

func (m Manager) TargetPath(skill catalog.Skill, client catalog.Client) (string, error) {
	return m.TargetPathAt(skill, client, ScopeProject)
}

func (m Manager) TargetPathAt(skill catalog.Skill, client catalog.Client, scope Scope) (string, error) {
	var targetDir string
	var err error
	if scope == ScopeGlobal {
		if m.userHome == "" {
			return "", errors.New("global skill scope requires a user home")
		}
		targetDir, err = m.clients.UserSkillsTargetDir(m.userHome, client)
	} else {
		targetDir, err = m.clients.TargetDir(m.projectRoot, client)
	}
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
	replaceLink
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
	Scope   Scope
}

// Retirement is an applied, reversible set of source-owned link removals. It
// exists for source lifecycle operations that must retire projections before a
// Git/catalog mutation and restore them if that later mutation fails.
type Retirement struct {
	applied linktransaction.Applied
}

func (r Retirement) Restore() error {
	return r.applied.Restore()
}

// RetireSource atomically removes exact symlinks into source from the requested
// scopes. User-owned files, links to another provider, and absent paths are not
// part of the transaction.
func (m Manager) RetireSource(source catalog.Source, scopes ...Scope) (Retirement, error) {
	changes := make([]change, 0)
	var conflicts []Conflict
	for _, scope := range scopes {
		for _, clientID := range m.clients.IDs() {
			if !m.SupportsScope(clientID, scope) {
				continue
			}
			var targetDir string
			var err error
			if scope == ScopeGlobal {
				targetDir, err = m.clients.UserSkillsTargetDir(m.userHome, clientID)
			} else {
				targetDir, err = m.clients.TargetDir(m.projectRoot, clientID)
			}
			if err != nil {
				return Retirement{}, err
			}
			planned, plannedConflicts := planRetireSource(source.Skills, targetDir)
			changes = append(changes, planned...)
			conflicts = append(conflicts, plannedConflicts...)
		}
	}
	if len(conflicts) > 0 {
		return Retirement{}, &ConflictError{Conflicts: conflicts}
	}
	applied, err := m.executeChanges(changes)
	if err != nil {
		return Retirement{}, err
	}
	return Retirement{applied: applied}, nil
}

func (m Manager) SetEnabled(skills []catalog.Skill, client catalog.Client, enabled bool) error {
	return m.SetEnabledAt(skills, client, enabled, ScopeProject)
}

func (m Manager) SetEnabledAt(skills []catalog.Skill, client catalog.Client, enabled bool, scope Scope) error {
	return m.Apply([]Operation{{Skills: skills, Client: client, Enabled: enabled, Scope: scope}})
}

func (m Manager) SupportsScope(clientID catalog.Client, scope Scope) bool {
	definition, ok := m.clients.Definition(clientID)
	if !ok {
		return false
	}
	if scope == ScopeGlobal {
		return m.userHome != "" && definition.UserSkillsDir != ""
	}
	return definition.ProjectSkillsDir != ""
}

func (m Manager) Apply(operations []Operation) error {
	changes := make([]change, 0)
	conflicts := make([]Conflict, 0)
	for _, operation := range operations {
		scope := operation.Scope
		if scope == "" {
			scope = ScopeProject
		}
		var targetDir string
		var err error
		if scope == ScopeGlobal {
			if m.userHome == "" {
				return errors.New("global skill scope requires a user home")
			}
			targetDir, err = m.clients.UserSkillsTargetDir(m.userHome, operation.Client)
		} else {
			targetDir, err = m.clients.TargetDir(m.projectRoot, operation.Client)
		}
		if err != nil {
			return err
		}
		definition, _ := m.clients.Definition(operation.Client)
		if scope == ScopeProject && operation.Enabled && m.userHome != "" && definition.UserSkillsDir != "" {
			globalDir, globalErr := m.clients.UserSkillsTargetDir(m.userHome, operation.Client)
			if globalErr != nil {
				return globalErr
			}
			for _, skill := range operation.Skills {
				path := filepath.Join(globalDir, skill.Name)
				if _, globalErr := os.Lstat(path); globalErr == nil {
					conflicts = append(conflicts, Conflict{Path: path, Reason: "skill is globally configured; disable global scope first"})
				} else if !errors.Is(globalErr, os.ErrNotExist) {
					conflicts = append(conflicts, Conflict{Path: path, Reason: globalErr.Error()})
				}
			}
		}
		if scope == ScopeGlobal && operation.Enabled {
			projectDir, projectErr := m.clients.TargetDir(m.projectRoot, operation.Client)
			if projectErr != nil {
				return projectErr
			}
			retirements, retirementConflicts := m.planRetireProject(operation.Skills, projectDir)
			changes = append(changes, retirements...)
			conflicts = append(conflicts, retirementConflicts...)
		}
		planned, operationConflicts := m.plan(operation.Skills, operation.Client, targetDir, operation.Enabled)
		changes = append(changes, planned...)
		conflicts = append(conflicts, operationConflicts...)
	}
	if len(conflicts) > 0 {
		return &ConflictError{Conflicts: conflicts}
	}
	_, err := m.executeChanges(changes)
	return err
}

func (m Manager) executeChanges(changes []change) (linktransaction.Applied, error) {
	transactionChanges := make([]linktransaction.Change, 0, len(changes))
	for _, next := range changes {
		action := linktransaction.Create
		if next.action == removeLink {
			action = linktransaction.Remove
		} else if next.action == replaceLink {
			action = linktransaction.Replace
		}
		transactionChanges = append(transactionChanges, linktransaction.Change{
			Action:         action,
			Path:           next.path,
			Target:         next.target,
			OriginalTarget: next.originalTarget,
		})
	}
	engine := linktransaction.Engine{
		Label: "skill projection",
		Conflict: func(path, reason string) error {
			return &ConflictError{Conflicts: []Conflict{{Path: path, Reason: reason}}}
		},
		ValidateSource: func(next linktransaction.Change) error {
			info, err := os.Stat(filepath.Join(next.Target, "SKILL.md"))
			if err == nil && info.Mode().IsRegular() {
				return nil
			}
			reason := "source SKILL.md changed after preflight"
			if err != nil {
				reason += ": " + err.Error()
			}
			return &ConflictError{Conflicts: []Conflict{{Path: next.Target, Reason: reason}}}
		},
	}
	if m.beforeApply != nil {
		engine.BeforeApply = func(next linktransaction.Change) {
			action := createLink
			if next.Action == linktransaction.Remove {
				action = removeLink
			} else if next.Action == linktransaction.Replace {
				action = replaceLink
			}
			m.beforeApply(change{action: action, path: next.Path, target: next.Target, originalTarget: next.OriginalTarget})
		}
	}
	return engine.Execute(transactionChanges)
}

func planRetireSource(skills []catalog.Skill, targetDir string) ([]change, []Conflict) {
	changes := make([]change, 0)
	conflicts := make([]Conflict, 0)
	for _, skill := range skills {
		path := filepath.Join(targetDir, skill.Name)
		info, err := os.Lstat(path)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			conflicts = append(conflicts, Conflict{Path: path, Reason: err.Error()})
			continue
		}
		if info.Mode()&os.ModeSymlink == 0 {
			continue
		}
		original, err := os.Readlink(path)
		if err != nil {
			conflicts = append(conflicts, Conflict{Path: path, Reason: "read symlink: " + err.Error()})
			continue
		}
		resolved := original
		if !filepath.IsAbs(resolved) {
			resolved = filepath.Join(filepath.Dir(path), resolved)
		}
		if filepath.Clean(resolved) != filepath.Clean(skill.Path) {
			continue
		}
		changes = append(changes, change{
			action:         removeLink,
			path:           path,
			target:         skill.Path,
			originalTarget: original,
		})
	}
	return changes, conflicts
}

func (m Manager) plan(skills []catalog.Skill, client catalog.Client, targetDir string, enabled bool) ([]change, []Conflict) {
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
			if m.isManagedAlternative(resolvedTarget, skill) {
				if enabled {
					changes = append(changes, change{
						action:         replaceLink,
						path:           linkPath,
						target:         skill.Path,
						originalTarget: originalTarget,
					})
				}
				continue
			}
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

func (m Manager) planRetireProject(skills []catalog.Skill, targetDir string) ([]change, []Conflict) {
	changes := make([]change, 0, len(skills))
	conflicts := make([]Conflict, 0)
	for _, skill := range skills {
		path := filepath.Join(targetDir, skill.Name)
		info, err := os.Lstat(path)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			conflicts = append(conflicts, Conflict{Path: path, Reason: err.Error()})
			continue
		}
		if info.Mode()&os.ModeSymlink == 0 {
			conflicts = append(conflicts, Conflict{Path: path, Reason: "project skill blocks global promotion and is not a managed symlink"})
			continue
		}
		original, err := os.Readlink(path)
		if err != nil {
			conflicts = append(conflicts, Conflict{Path: path, Reason: "read project symlink: " + err.Error()})
			continue
		}
		resolved := original
		if !filepath.IsAbs(resolved) {
			resolved = filepath.Join(filepath.Dir(path), resolved)
		}
		provider, managed := m.providersByPath[filepath.Clean(resolved)]
		if !managed || provider.Name != skill.Name {
			conflicts = append(conflicts, Conflict{Path: path, Reason: "project skill blocks global promotion and is not catalog-managed"})
			continue
		}
		changes = append(changes, change{action: removeLink, path: path, target: resolved, originalTarget: original})
	}
	return changes, conflicts
}

func (m Manager) isManagedAlternative(target string, selected catalog.Skill) bool {
	provider, ok := m.providersByPath[filepath.Clean(target)]
	return ok && provider.Name == selected.Name && filepath.Clean(provider.Path) != filepath.Clean(selected.Path)
}
