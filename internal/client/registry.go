package client

import (
	"fmt"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

type ID string

const (
	Codex  ID = "codex"
	Claude ID = "claude"
	Gemini ID = "gemini"
)

type Definition struct {
	ID               ID
	ProjectSkillsDir string
}

type Registry struct {
	ordered []Definition
	byID    map[ID]Definition
}

var clientIDPattern = regexp.MustCompile(`^[a-z][a-z0-9_-]*$`)

var builtins = []Definition{
	{ID: Codex, ProjectSkillsDir: ".agents/skills"},
	{ID: Claude, ProjectSkillsDir: ".claude/skills"},
	{ID: Gemini, ProjectSkillsDir: ".gemini/skills"},
}

func NewRegistry(configured map[ID]string) (Registry, error) {
	definitions := make(map[ID]Definition, len(builtins)+len(configured))
	for _, definition := range builtins {
		definitions[definition.ID] = definition
	}
	for id, projectSkillsDir := range configured {
		definition := Definition{ID: id, ProjectSkillsDir: projectSkillsDir}
		if err := validateDefinition(definition); err != nil {
			return Registry{}, err
		}
		definitions[id] = definition
	}

	ordered := make([]Definition, 0, len(definitions))
	for _, builtin := range builtins {
		ordered = append(ordered, definitions[builtin.ID])
		delete(definitions, builtin.ID)
	}
	customIDs := make([]string, 0, len(definitions))
	for id := range definitions {
		customIDs = append(customIDs, string(id))
	}
	sort.Strings(customIDs)
	for _, rawID := range customIDs {
		ordered = append(ordered, definitions[ID(rawID)])
	}

	byID := make(map[ID]Definition, len(ordered))
	for _, definition := range ordered {
		if err := validateDefinition(definition); err != nil {
			return Registry{}, err
		}
		byID[definition.ID] = definition
	}
	return Registry{ordered: ordered, byID: byID}, nil
}

func DefaultRegistry() Registry {
	registry, err := NewRegistry(nil)
	if err != nil {
		panic(err)
	}
	return registry
}

func (r Registry) IDs() []ID {
	ids := make([]ID, 0, len(r.ordered))
	for _, definition := range r.ordered {
		ids = append(ids, definition.ID)
	}
	return ids
}

func (r Registry) Definitions() []Definition {
	return append([]Definition(nil), r.ordered...)
}

func (r Registry) Has(id ID) bool {
	_, ok := r.byID[id]
	return ok
}

func (r Registry) TargetDir(projectRoot string, id ID) (string, error) {
	definition, ok := r.byID[id]
	if !ok {
		return "", fmt.Errorf("unknown client %q", id)
	}
	return filepath.Join(projectRoot, filepath.FromSlash(definition.ProjectSkillsDir)), nil
}

func validateDefinition(definition Definition) error {
	if !clientIDPattern.MatchString(string(definition.ID)) {
		return fmt.Errorf("invalid client id %q", definition.ID)
	}
	path := filepath.Clean(filepath.FromSlash(definition.ProjectSkillsDir))
	if definition.ProjectSkillsDir == "" || path == "." || filepath.IsAbs(path) || path == ".." || strings.HasPrefix(path, ".."+string(filepath.Separator)) {
		return fmt.Errorf("client %s projectSkillsDir must stay inside the project: %q", definition.ID, definition.ProjectSkillsDir)
	}
	return nil
}
