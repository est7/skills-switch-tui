package client

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadRegistryExtendsBuiltins(t *testing.T) {
	path := filepath.Join(t.TempDir(), "registry.yaml")
	contents := `version: 1
clients:
  pi:
    projectSkillsDir: .pi/skills
    userPromptDir: .pi
`
	if err := os.WriteFile(path, []byte(contents), 0o644); err != nil {
		t.Fatal(err)
	}

	registry, err := LoadRegistry(path)
	if err != nil {
		t.Fatal(err)
	}
	if !registry.Has(Codex) || !registry.Has("pi") {
		t.Fatalf("registry IDs = %v, want builtins and pi", registry.IDs())
	}
}

func TestLoadRegistryMissingFileUsesBuiltins(t *testing.T) {
	registry, err := LoadRegistry(filepath.Join(t.TempDir(), "registry.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if got, want := registry.IDs(), DefaultRegistry().IDs(); len(got) != len(want) {
		t.Fatalf("registry IDs = %v, want %v", got, want)
	}
}

func TestLoadRegistryRejectsUnknownFieldsAndVersions(t *testing.T) {
	tests := []struct {
		name     string
		contents string
		want     string
	}{
		{name: "unknown field", contents: "version: 1\nunknown: true\n", want: "field unknown not found"},
		{name: "unknown version", contents: "version: 2\n", want: "unsupported client registry version"},
		{name: "multiple documents", contents: "version: 1\n---\nversion: 1\n", want: "multiple YAML documents"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "registry.yaml")
			if err := os.WriteFile(path, []byte(test.contents), 0o644); err != nil {
				t.Fatal(err)
			}
			_, err := LoadRegistry(path)
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("LoadRegistry() error = %v, want %q", err, test.want)
			}
		})
	}
}
