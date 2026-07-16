package systemprompt

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"

	"github.com/est7/skills-switch-tui/internal/client"
	"github.com/est7/skills-switch-tui/internal/linkprojection"
)

type State string

const (
	StateDisabled State = "disabled"
	StateEnabled  State = "enabled"
	StatePartial  State = "partial"
	StateConflict State = "conflict"
	StateBroken   State = "broken"
	StateStale    State = "stale"
)

type Conflict = linkprojection.Conflict
type ConflictError = linkprojection.ConflictError

type BuildResult struct {
	GroupID string `json:"prompt"`
	Path    string `json:"path"`
	Bytes   int    `json:"bytes"`
	Changed bool   `json:"changed"`
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

type Manager struct {
	userHome    string
	clients     client.Registry
	beforeApply func(change)
}

func NewManager(userHome string, clients client.Registry) Manager {
	return Manager{userHome: userHome, clients: clients}
}

func (m Manager) State(group Group) (State, error) {
	if group.Mode == client.PromptConcat {
		return m.concatState(group)
	}
	files, err := m.projectionFiles([]Group{group})
	if err != nil {
		return "", err
	}
	state, err := (linkprojection.Manager{Label: "system prompt"}).State(files)
	return State(state), err
}

func (m Manager) Build(group Group) (BuildResult, error) {
	if group.Mode != client.PromptConcat {
		return BuildResult{}, fmt.Errorf("system prompt group %s uses %s projection and does not need a build", group.ID, group.Mode)
	}
	compiled, err := compile(group)
	if err != nil {
		return BuildResult{}, err
	}
	path := m.GeneratedPath(group)
	changed, err := writeAtomicallyIfChanged(path, compiled)
	if err != nil {
		return BuildResult{}, fmt.Errorf("build system prompt %s at %s: %w", group.ID, path, err)
	}
	return BuildResult{GroupID: group.ID, Path: path, Bytes: len(compiled), Changed: changed}, nil
}

func (m Manager) GeneratedPath(group Group) string {
	return filepath.Join(m.userHome, ".agents", "generated", "system-prompts", group.ID, group.EntryFile)
}

func (m Manager) SetEnabled(groups []Group, enabled bool) error {
	if enabled {
		for _, group := range groups {
			if group.Mode == client.PromptConcat {
				if _, err := m.Build(group); err != nil {
					return err
				}
			}
		}
	}
	files, err := m.projectionFiles(groups)
	if err != nil {
		return err
	}
	links := linkprojection.Manager{Label: "system prompt"}
	if m.beforeApply != nil {
		links.BeforeApply = func(next linkprojection.Change) {
			action := createLink
			if next.Action == linkprojection.RemoveLink {
				action = removeLink
			}
			m.beforeApply(change{action: action, path: next.Path, target: next.Target})
		}
	}
	return links.SetEnabled(files, enabled)
}

func (m Manager) projectionFiles(groups []Group) ([]linkprojection.File, error) {
	files := make([]linkprojection.File, 0)
	for _, group := range groups {
		targetRoot, err := m.clients.UserPromptTargetDir(m.userHome, group.Client)
		if err != nil {
			return nil, err
		}
		if group.Mode == client.PromptConcat {
			legacySources := make([]string, 0, 1)
			for _, file := range group.Files {
				if filepath.Clean(file.RelativePath) == filepath.Clean(group.EntryFile) {
					legacySources = append(legacySources, file.SourcePath)
					break
				}
			}
			files = append(files, linkprojection.File{
				SourcePath:        m.GeneratedPath(group),
				TargetPath:        filepath.Join(targetRoot, group.EntryFile),
				LegacySourcePaths: legacySources,
			})
			continue
		}
		for _, file := range group.Files {
			files = append(files, linkprojection.File{
				SourcePath: file.SourcePath,
				TargetPath: filepath.Join(targetRoot, file.RelativePath),
			})
		}
	}
	return files, nil
}

func (m Manager) concatState(group Group) (State, error) {
	targetRoot, err := m.clients.UserPromptTargetDir(m.userHome, group.Client)
	if err != nil {
		return "", err
	}
	generated := m.GeneratedPath(group)
	target := filepath.Join(targetRoot, group.EntryFile)
	matches, exists, conflict, err := inspectLink(target, generated)
	if err != nil {
		return "", err
	}
	if conflict {
		for _, file := range group.Files {
			if filepath.Clean(file.RelativePath) != filepath.Clean(group.EntryFile) {
				continue
			}
			legacyMatches, _, legacyConflict, legacyErr := inspectLink(target, file.SourcePath)
			if legacyErr != nil {
				return "", legacyErr
			}
			if legacyMatches && !legacyConflict {
				return StateStale, nil
			}
			break
		}
		return StateConflict, nil
	}
	if !exists {
		return StateDisabled, nil
	}
	if !matches {
		return StateConflict, nil
	}
	compiled, err := compile(group)
	if err != nil {
		return StateBroken, nil
	}
	actual, err := os.ReadFile(generated)
	if errors.Is(err, os.ErrNotExist) {
		return StateBroken, nil
	}
	if err != nil {
		return "", fmt.Errorf("read generated system prompt %s: %w", generated, err)
	}
	if !bytes.Equal(actual, compiled) {
		return StateStale, nil
	}
	return StateEnabled, nil
}

func compile(group Group) ([]byte, error) {
	if group.Mode != client.PromptConcat {
		return nil, fmt.Errorf("system prompt group %s is not concat", group.ID)
	}
	files := append([]File(nil), group.Files...)
	sort.Slice(files, func(i, j int) bool {
		iEntry := filepath.Clean(files[i].RelativePath) == filepath.Clean(group.EntryFile)
		jEntry := filepath.Clean(files[j].RelativePath) == filepath.Clean(group.EntryFile)
		if iEntry != jEntry {
			return iEntry
		}
		return filepath.ToSlash(files[i].RelativePath) < filepath.ToSlash(files[j].RelativePath)
	})
	var output bytes.Buffer
	fmt.Fprintf(&output, "<!-- GENERATED FILE: edit %s and its rules/ directory -->\n", group.ID)
	fmt.Fprintf(&output, "<!-- Run: skills-switch prompt build %s -->\n\n", group.ID)
	for _, file := range files {
		contents, err := os.ReadFile(file.SourcePath)
		if err != nil {
			return nil, fmt.Errorf("read system prompt source %s: %w", file.SourcePath, err)
		}
		relative := filepath.ToSlash(file.RelativePath)
		fmt.Fprintf(&output, "<!-- BEGIN: %s -->\n", relative)
		output.Write(contents)
		if len(contents) == 0 || contents[len(contents)-1] != '\n' {
			output.WriteByte('\n')
		}
		fmt.Fprintf(&output, "<!-- END: %s -->\n\n", relative)
	}
	return output.Bytes(), nil
}

func writeAtomicallyIfChanged(path string, contents []byte) (bool, error) {
	existing, err := os.ReadFile(path)
	if err == nil && bytes.Equal(existing, contents) {
		return false, nil
	}
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return false, err
	}
	directory := filepath.Dir(path)
	if err := os.MkdirAll(directory, 0o755); err != nil {
		return false, err
	}
	temporary, err := os.CreateTemp(directory, ".prompt-build-*")
	if err != nil {
		return false, err
	}
	temporaryPath := temporary.Name()
	defer os.Remove(temporaryPath)
	if err := temporary.Chmod(0o644); err != nil {
		temporary.Close()
		return false, err
	}
	if _, err := temporary.Write(contents); err != nil {
		temporary.Close()
		return false, err
	}
	if err := temporary.Sync(); err != nil {
		temporary.Close()
		return false, err
	}
	if err := temporary.Close(); err != nil {
		return false, err
	}
	if err := os.Rename(temporaryPath, path); err != nil {
		return false, err
	}
	return true, nil
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
