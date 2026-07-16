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
type PromptMode string

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

const (
	PromptTree   PromptMode = "tree"
	PromptConcat PromptMode = "concat"
)

type Definition struct {
	ID                  ID
	ProjectSkillsDir    string
	UserPromptDir       string
	UserPromptMode      PromptMode
	UserPromptEntry     string
	UserCommandsDir     string
	UserHooksDir        string
	UserAgentsDir       string
	UserOutputStylesDir string
	ProjectMCPFile      string
	ProjectMCPFormat    MCPFormat
}

type Registry struct {
	ordered []Definition
	byID    map[ID]Definition
}

var clientIDPattern = regexp.MustCompile(`^[a-z][a-z0-9_-]*$`)

var builtins = []Definition{
	{ID: Codex, ProjectSkillsDir: ".agents/skills", UserPromptDir: ".codex", UserPromptMode: PromptConcat, UserPromptEntry: "AGENTS.md", UserCommandsDir: ".codex/prompts", UserHooksDir: ".codex/hooks", UserAgentsDir: ".codex/agents", ProjectMCPFile: ".codex/config.toml", ProjectMCPFormat: MCPCodexTOML},
	{ID: Claude, ProjectSkillsDir: ".claude/skills", UserPromptDir: ".claude", UserPromptMode: PromptTree, UserCommandsDir: ".claude/commands", UserHooksDir: ".claude/hooks", UserAgentsDir: ".claude/agents", UserOutputStylesDir: ".claude/output-styles", ProjectMCPFile: ".mcp.json", ProjectMCPFormat: MCPClaudeJSON},
	{ID: Gemini, ProjectSkillsDir: ".gemini/skills", UserPromptDir: ".gemini", UserPromptMode: PromptTree, UserCommandsDir: ".gemini/commands", UserHooksDir: ".gemini/hooks", UserAgentsDir: ".gemini/agents", ProjectMCPFile: ".gemini/settings.json", ProjectMCPFormat: MCPGeminiJSON},
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
		definition = normalizeDefinition(definition)
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
		definition = normalizeDefinition(definition)
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
	if override.UserPromptMode != "" {
		base.UserPromptMode = override.UserPromptMode
		if override.UserPromptMode == PromptTree {
			base.UserPromptEntry = ""
		}
	}
	if override.UserPromptEntry != "" {
		base.UserPromptEntry = override.UserPromptEntry
	}
	if override.UserCommandsDir != "" {
		base.UserCommandsDir = override.UserCommandsDir
	}
	if override.UserHooksDir != "" {
		base.UserHooksDir = override.UserHooksDir
	}
	if override.UserAgentsDir != "" {
		base.UserAgentsDir = override.UserAgentsDir
	}
	if override.UserOutputStylesDir != "" {
		base.UserOutputStylesDir = override.UserOutputStylesDir
	}
	if override.ProjectMCPFile != "" {
		base.ProjectMCPFile = override.ProjectMCPFile
	}
	if override.ProjectMCPFormat != "" {
		base.ProjectMCPFormat = override.ProjectMCPFormat
	}
	return base
}

func normalizeDefinition(definition Definition) Definition {
	if definition.UserPromptDir != "" && definition.UserPromptMode == "" {
		definition.UserPromptMode = PromptTree
	}
	return definition
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

func (r Registry) UserPromptAdapter(id ID) (PromptMode, string, error) {
	definition, ok := r.byID[id]
	if !ok {
		return "", "", fmt.Errorf("unknown client %q", id)
	}
	if definition.UserPromptDir == "" {
		return "", "", fmt.Errorf("client %s does not support system prompts", id)
	}
	return definition.UserPromptMode, definition.UserPromptEntry, nil
}

func (r Registry) UserCommandsTargetDir(userHome string, id ID) (string, error) {
	definition, ok := r.byID[id]
	if !ok {
		return "", fmt.Errorf("unknown client %q", id)
	}
	if definition.UserCommandsDir == "" {
		return "", fmt.Errorf("client %s does not support commands", id)
	}
	return filepath.Join(userHome, filepath.FromSlash(definition.UserCommandsDir)), nil
}

func (r Registry) UserHooksTargetDir(userHome string, id ID) (string, error) {
	definition, ok := r.byID[id]
	if !ok {
		return "", fmt.Errorf("unknown client %q", id)
	}
	if definition.UserHooksDir == "" {
		return "", fmt.Errorf("client %s does not support hooks", id)
	}
	return filepath.Join(userHome, filepath.FromSlash(definition.UserHooksDir)), nil
}

func (r Registry) UserAgentsTargetDir(userHome string, id ID) (string, error) {
	definition, ok := r.byID[id]
	if !ok {
		return "", fmt.Errorf("unknown client %q", id)
	}
	if definition.UserAgentsDir == "" {
		return "", fmt.Errorf("client %s does not support agents", id)
	}
	return filepath.Join(userHome, filepath.FromSlash(definition.UserAgentsDir)), nil
}

func (r Registry) UserOutputStylesTargetDir(userHome string, id ID) (string, error) {
	definition, ok := r.byID[id]
	if !ok {
		return "", fmt.Errorf("unknown client %q", id)
	}
	if definition.UserOutputStylesDir == "" {
		return "", fmt.Errorf("client %s does not support output styles", id)
	}
	return filepath.Join(userHome, filepath.FromSlash(definition.UserOutputStylesDir)), nil
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
	switch definition.UserPromptMode {
	case "":
		if definition.UserPromptDir != "" {
			return fmt.Errorf("client %s userPromptMode is required when userPromptDir is set", definition.ID)
		}
	case PromptTree:
		if definition.UserPromptEntry != "" {
			return fmt.Errorf("client %s userPromptEntry is only valid for concat prompts", definition.ID)
		}
	case PromptConcat:
		if definition.UserPromptDir == "" {
			return fmt.Errorf("client %s userPromptDir is required for concat prompts", definition.ID)
		}
		if definition.UserPromptEntry == "" {
			return fmt.Errorf("client %s userPromptEntry is required for concat prompts", definition.ID)
		}
		if err := validateRelativePath(definition.UserPromptEntry, true); err != nil {
			return fmt.Errorf("client %s userPromptEntry must stay inside the prompt directory: %q", definition.ID, definition.UserPromptEntry)
		}
	default:
		return fmt.Errorf("client %s has unknown userPromptMode %q", definition.ID, definition.UserPromptMode)
	}
	if err := validateRelativePath(definition.UserCommandsDir, false); err != nil {
		return fmt.Errorf("client %s userCommandsDir must stay inside the user home: %q", definition.ID, definition.UserCommandsDir)
	}
	if err := validateRelativePath(definition.UserHooksDir, false); err != nil {
		return fmt.Errorf("client %s userHooksDir must stay inside the user home: %q", definition.ID, definition.UserHooksDir)
	}
	if err := validateRelativePath(definition.UserAgentsDir, false); err != nil {
		return fmt.Errorf("client %s userAgentsDir must stay inside the user home: %q", definition.ID, definition.UserAgentsDir)
	}
	if err := validateRelativePath(definition.UserOutputStylesDir, false); err != nil {
		return fmt.Errorf("client %s userOutputStylesDir must stay inside the user home: %q", definition.ID, definition.UserOutputStylesDir)
	}
	if definition.ProjectSkillsDir == "" && definition.UserPromptDir == "" && definition.UserCommandsDir == "" && definition.UserHooksDir == "" && definition.UserAgentsDir == "" && definition.UserOutputStylesDir == "" && definition.ProjectMCPFile == "" {
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
