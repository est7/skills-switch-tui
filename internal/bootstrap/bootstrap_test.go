package bootstrap

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/est7/skills-switch-tui/internal/catalog"
	"github.com/est7/skills-switch-tui/internal/client"
)

func TestInitializeCreatesResourceSkeletonAndRegistersBundledSkillIdempotently(t *testing.T) {
	agentsRoot := filepath.Join(t.TempDir(), ".agents")
	resourcesRoot := filepath.Join(agentsRoot, "resources")
	git := &bootstrapGit{repositoryRoot: agentsRoot}
	manager := Manager{
		ResourcesRoot: resourcesRoot,
		RepositoryURL: "https://github.com/est7/skills-switch-tui.git",
		Git:           git,
	}

	first, err := manager.Initialize(context.Background())
	if err != nil {
		t.Fatalf("first initialize: %v", err)
	}
	if !first.SourceAdded {
		t.Fatal("first initialize did not register the bundled source")
	}
	for _, path := range []string{
		filepath.Join(resourcesRoot, "skills", "local", "shared"),
		filepath.Join(resourcesRoot, "skills", "archived", "shared"),
		filepath.Join(resourcesRoot, "skills", "vendor", "shared"),
		filepath.Join(resourcesRoot, "mcp", "mcp.json"),
		filepath.Join(resourcesRoot, "system-prompts"),
		filepath.Join(resourcesRoot, "commands", "shared"),
		filepath.Join(resourcesRoot, "hooks", "shared"),
	} {
		if _, err := os.Stat(path); err != nil {
			t.Fatalf("bootstrap path %s: %v", path, err)
		}
	}
	ignore, err := os.ReadFile(filepath.Join(agentsRoot, ".gitignore"))
	if err != nil || string(ignore) != "/skills/\n" {
		t.Fatalf("bootstrap .gitignore = %q, %v", ignore, err)
	}
	loaded, err := catalog.Load(filepath.Join(resourcesRoot, "skills"), client.DefaultRegistry())
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := loaded.Skill(BundledSkillID); !ok {
		t.Fatalf("bundled Skill %q was not discovered", BundledSkillID)
	}

	customMCP := []byte("{\"mcpServers\":{\"keep\":{\"command\":\"keep\"}}}\n")
	if err := os.WriteFile(filepath.Join(resourcesRoot, "mcp", "mcp.json"), customMCP, 0o644); err != nil {
		t.Fatal(err)
	}
	second, err := manager.Initialize(context.Background())
	if err != nil {
		t.Fatalf("second initialize: %v", err)
	}
	if second.SourceAdded {
		t.Fatal("second initialize registered the bundled source again")
	}
	if git.submoduleAdds != 1 {
		t.Fatalf("submodule adds = %d, want 1", git.submoduleAdds)
	}
	data, err := os.ReadFile(filepath.Join(resourcesRoot, "mcp", "mcp.json"))
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != string(customMCP) {
		t.Fatalf("second initialize overwrote MCP catalog: %s", data)
	}
}

func TestEnsureGitIgnoreEntryPreservesExistingContentAndIsIdempotent(t *testing.T) {
	path := filepath.Join(t.TempDir(), ".gitignore")
	if err := os.WriteFile(path, []byte("generated/"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := ensureGitIgnoreEntry(path, "/skills/"); err != nil {
		t.Fatal(err)
	}
	if err := ensureGitIgnoreEntry(path, "/skills/"); err != nil {
		t.Fatal(err)
	}
	contents, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := string(contents), "generated/\n/skills/\n"; got != want {
		t.Fatalf(".gitignore = %q, want %q", got, want)
	}
}

func TestEnsureGitIgnoreEntryMigratesLegacySpellingsInPlace(t *testing.T) {
	path := filepath.Join(t.TempDir(), ".gitignore")
	// The unanchored skills/ that earlier releases wrote also ignored
	// resources/skills/, blocking vendor submodule adds; it must be rewritten
	// even when the anchored entry is already present.
	if err := os.WriteFile(path, []byte("generated/\n/skills/\nskills/\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := ensureGitIgnoreEntry(path, "/skills/", "skills/"); err != nil {
		t.Fatal(err)
	}
	contents, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := string(contents), "generated/\n/skills/\n"; got != want {
		t.Fatalf(".gitignore = %q, want %q", got, want)
	}

	if err := os.WriteFile(path, []byte("generated/\nskills/\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := ensureGitIgnoreEntry(path, "/skills/", "skills/"); err != nil {
		t.Fatal(err)
	}
	contents, err = os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := string(contents), "generated/\n/skills/\n"; got != want {
		t.Fatalf(".gitignore = %q, want %q", got, want)
	}
	if err := ensureGitIgnoreEntry(path, "/skills/", "skills/"); err != nil {
		t.Fatal(err)
	}
	contents, err = os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := string(contents), "generated/\n/skills/\n"; got != want {
		t.Fatalf(".gitignore = %q, want repeat run to stay idempotent with %q", got, want)
	}
}

type bootstrapGit struct {
	repositoryRoot string
	initialized    bool
	submoduleAdds  int
}

func (g *bootstrapGit) Output(_ context.Context, directory string, arguments ...string) ([]byte, error) {
	command := strings.Join(arguments, " ")
	switch {
	case command == "rev-parse --show-toplevel":
		if !g.initialized {
			return nil, errors.New("not a git repository")
		}
		return []byte(g.repositoryRoot + "\n"), nil
	case command == "init":
		g.initialized = true
		return nil, nil
	case strings.HasPrefix(command, "submodule add "):
		g.submoduleAdds++
		target := filepath.Join(directory, filepath.FromSlash(arguments[len(arguments)-1]))
		if err := writeBootstrapFile(filepath.Join(target, ".agents", "plugins", "marketplace.json"), `{
  "name": "skills-switch",
  "plugins": [{"name": "skills-switch", "source": {"source": "local", "path": "."}}]
}`); err != nil {
			return nil, err
		}
		if err := writeBootstrapFile(filepath.Join(target, "skills", "skills-switch", "SKILL.md"), `---
name: skills-switch
description: Manage skills-switch resources.
---
`); err != nil {
			return nil, err
		}
		return nil, nil
	case strings.HasPrefix(command, "sparse-checkout "):
		return nil, nil
	case strings.HasPrefix(command, "rm -- "):
		return nil, nil
	default:
		return nil, errors.New("unexpected git command: " + command)
	}
}

func writeBootstrapFile(path, content string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, []byte(content), 0o644)
}
