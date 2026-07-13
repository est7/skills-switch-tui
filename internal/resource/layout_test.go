package resource

import (
	"path/filepath"
	"testing"
)

func TestLayoutOwnsResourceTopology(t *testing.T) {
	root := t.TempDir()
	layout, err := NewLayout(root)
	if err != nil {
		t.Fatal(err)
	}

	tests := map[string]string{
		layout.RegistryFile():      filepath.Join(root, "registry.yaml"),
		layout.SkillsRoot():        filepath.Join(root, "skills"),
		layout.MCPCatalogFile():    filepath.Join(root, "mcp", "mcp.json"),
		layout.SystemPromptsRoot(): filepath.Join(root, "system-prompts"),
	}
	for got, want := range tests {
		if got != want {
			t.Fatalf("layout path = %q, want %q", got, want)
		}
	}
}
