package resource

import (
	"fmt"
	"path/filepath"
)

type Layout struct {
	Root string
}

func NewLayout(root string) (Layout, error) {
	absRoot, err := filepath.Abs(root)
	if err != nil {
		return Layout{}, fmt.Errorf("resolve resources root: %w", err)
	}
	return Layout{Root: absRoot}, nil
}

func (l Layout) RegistryFile() string {
	return filepath.Join(l.Root, "registry.yaml")
}

func (l Layout) SkillsRoot() string {
	return filepath.Join(l.Root, "skills")
}

func (l Layout) MCPCatalogFile() string {
	return filepath.Join(l.Root, "mcp", "mcp.json")
}

func (l Layout) SystemPromptsRoot() string {
	return filepath.Join(l.Root, "system-prompts")
}
