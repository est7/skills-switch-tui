package client

import (
	"fmt"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

type ID string

type MCPFormat string

const (
	Codex  ID = "codex"
	Claude ID = "claude"
	Gemini ID = "gemini"
)

const (
	MCPClaudeJSON MCPFormat = "claude-json"
	MCPCodexTOML  MCPFormat = "codex-toml"
	MCPGeminiJSON MCPFormat = "gemini-json"
)

type Definition struct {
	ID               ID
	ProjectSkillsDir string
	UserPromptDir    string
	ProjectMCPFile   string
	ProjectMCPFormat MCPFormat
}

type Registry struct {
	ordered []Definition
	byID    map[ID]Definition
}

var clientIDPattern = regexp.MustCompile(`^[a-z][a-z0-9_-]*$`)

var builtins = []Definition{
	{ID: Codex, ProjectSkillsDir: ".agents/skills", UserPromptDir: ".codex", ProjectMCPFile: ".codex/config.toml", ProjectMCPFormat: MCPCodexTOML},
	{ID: Claude, ProjectSkillsDir: ".claude/skills", UserPromptDir: ".claude", ProjectMCPFile: ".mcp.json", ProjectMCPFormat: MCPClaudeJSON},
	{ID: Gemini, ProjectSkillsDir: ".gemini/skills", UserPromptDir: ".gemini", ProjectMCPFile: ".gemini/settings.json", ProjectMCPFormat: MCPGeminiJSON},
}

func NewRegistry(configured map[ID]Definition) (Registry, error) {
	definitions := make(map[ID]Definition, len(builtins)+len(configured))
	for _, definition := range builtins {
		definitions[definition.ID] = definition
	}
	for id, configuredDefinition := range configured {
		definition := configuredDefinition
		definition.ID = id
		if builtin, ok := definitions[id]; ok {
			definition = mergeDefinition(builtin, definition)
		}
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

func mergeDefinition(base, override Definition) Definition {
	base.ID = override.ID
	if override.ProjectSkillsDir != "" {
		base.ProjectSkillsDir = override.ProjectSkillsDir
	}
	if override.UserPromptDir != "" {
		base.UserPromptDir = override.UserPromptDir
	}
	if override.ProjectMCPFile != "" {
		base.ProjectMCPFile = override.ProjectMCPFile
	}
	if override.ProjectMCPFormat != "" {
		base.ProjectMCPFormat = override.ProjectMCPFormat
	}
	return base
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

func (r Registry) Definition(id ID) (Definition, bool) {
	definition, ok := r.byID[id]
	return definition, ok
}

func (r Registry) TargetDir(projectRoot string, id ID) (string, error) {
	definition, ok := r.byID[id]
	if !ok {
		return "", fmt.Errorf("unknown client %q", id)
	}
	if definition.ProjectSkillsDir == "" {
		return "", fmt.Errorf("client %s does not support skills", id)
	}
	return filepath.Join(projectRoot, filepath.FromSlash(definition.ProjectSkillsDir)), nil
}

func (r Registry) UserPromptTargetDir(userHome string, id ID) (string, error) {
	definition, ok := r.byID[id]
	if !ok {
		return "", fmt.Errorf("unknown client %q", id)
	}
	if definition.UserPromptDir == "" {
		return "", fmt.Errorf("client %s does not support system prompts", id)
	}
	return filepath.Join(userHome, filepath.FromSlash(definition.UserPromptDir)), nil
}

func (r Registry) MCPProjectFile(projectRoot string, id ID) (string, MCPFormat, error) {
	definition, ok := r.byID[id]
	if !ok {
		return "", "", fmt.Errorf("unknown client %q", id)
	}
	if definition.ProjectMCPFile == "" || definition.ProjectMCPFormat == "" {
		return "", "", fmt.Errorf("client %s does not support MCP", id)
	}
	return filepath.Join(projectRoot, filepath.FromSlash(definition.ProjectMCPFile)), definition.ProjectMCPFormat, nil
}

func validateDefinition(definition Definition) error {
	if !clientIDPattern.MatchString(string(definition.ID)) {
		return fmt.Errorf("invalid client id %q", definition.ID)
	}
	if err := validateRelativePath(definition.ProjectSkillsDir, false); err != nil {
		return fmt.Errorf("client %s projectSkillsDir must stay inside the project: %q", definition.ID, definition.ProjectSkillsDir)
	}
	if err := validateRelativePath(definition.UserPromptDir, false); err != nil {
		return fmt.Errorf("client %s userPromptDir must stay inside the user home: %q", definition.ID, definition.UserPromptDir)
	}
	if definition.ProjectSkillsDir == "" && definition.UserPromptDir == "" && definition.ProjectMCPFile == "" {
		return fmt.Errorf("client %s must declare at least one resource adapter", definition.ID)
	}
	if definition.ProjectMCPFile != "" {
		if err := validateRelativePath(definition.ProjectMCPFile, false); err != nil {
			return fmt.Errorf("client %s projectMCPFile must stay inside the project: %q", definition.ID, definition.ProjectMCPFile)
		}
		if definition.ProjectMCPFormat == "" {
			return fmt.Errorf("client %s projectMCPFormat is required when projectMCPFile is set", definition.ID)
		}
	}
	if definition.ProjectMCPFile == "" && definition.ProjectMCPFormat != "" {
		return fmt.Errorf("client %s projectMCPFile is required when projectMCPFormat is set", definition.ID)
	}
	switch definition.ProjectMCPFormat {
	case "", MCPClaudeJSON, MCPCodexTOML, MCPGeminiJSON:
	default:
		return fmt.Errorf("client %s has unknown projectMCPFormat %q", definition.ID, definition.ProjectMCPFormat)
	}
	return nil
}

func validateRelativePath(raw string, allowRoot bool) error {
	if raw == "" {
		return nil
	}
	path := filepath.Clean(filepath.FromSlash(raw))
	if (!allowRoot && path == ".") || filepath.IsAbs(path) || path == ".." || strings.HasPrefix(path, ".."+string(filepath.Separator)) {
		return fmt.Errorf("path escapes root")
	}
	return nil
}
