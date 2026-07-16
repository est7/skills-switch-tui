package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSkillsDeleteRemovesLocalSkillOnlyWithConfirmation(t *testing.T) {
	resourceRoot := t.TempDir()
	projectRoot := t.TempDir()
	if err := os.Mkdir(filepath.Join(projectRoot, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	sourcesRoot := filepath.Join(resourceRoot, "skills")
	writeCLISkill(t, filepath.Join(sourcesRoot, "local", "shared", "core", "alpha"), "alpha")
	writeCLISkill(t, filepath.Join(sourcesRoot, "local", "shared", "core", "beta"), "beta")
	base := []string{"--resources", resourceRoot, "--project", projectRoot}

	if _, err := execute(t, append(base, "skills", "delete", "local-shared/core/alpha")...); err == nil {
		t.Fatal("delete without --yes must be refused")
	}
	if _, err := os.Stat(filepath.Join(sourcesRoot, "local", "shared", "core", "alpha", "SKILL.md")); err != nil {
		t.Fatalf("skill removed without confirmation: %v", err)
	}

	if _, err := execute(t, append(base, "skills", "delete", "local-shared/core/alpha", "--yes")...); err != nil {
		t.Fatalf("delete skill: %v", err)
	}
	if _, err := os.Stat(filepath.Join(sourcesRoot, "local", "shared", "core", "alpha")); !os.IsNotExist(err) {
		t.Fatal("skill directory not removed")
	}
	if _, err := os.Stat(filepath.Join(sourcesRoot, "local", "shared", "core", "beta", "SKILL.md")); err != nil {
		t.Fatalf("sibling skill removed: %v", err)
	}

	if _, err := execute(t, append(base, "skills", "delete", "local-shared/core", "--yes")...); err != nil {
		t.Fatalf("delete group: %v", err)
	}
	if _, err := os.Stat(filepath.Join(sourcesRoot, "local", "shared", "core")); !os.IsNotExist(err) {
		t.Fatal("group directory not removed")
	}
}

func TestSkillsDeleteRejectsVendorArchivedAndUnknown(t *testing.T) {
	resourceRoot := t.TempDir()
	projectRoot := t.TempDir()
	if err := os.Mkdir(filepath.Join(projectRoot, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	sourcesRoot := filepath.Join(resourceRoot, "skills")
	writeCLISkill(t, filepath.Join(sourcesRoot, "vendor", "shared", "repo", "skills", "tool"), "tool")
	base := []string{"--resources", resourceRoot, "--project", projectRoot}

	for _, id := range []string{"vendor-shared/repo/skills/tool", "vendor-shared/repo", "local-shared/nope"} {
		if _, err := execute(t, append(base, "skills", "delete", id, "--yes")...); err == nil {
			t.Fatalf("delete of %q must be rejected", id)
		}
	}
	if _, err := os.Stat(filepath.Join(sourcesRoot, "vendor", "shared", "repo")); err != nil {
		t.Fatalf("vendor source removed by skills delete: %v", err)
	}
}

func TestSkillsDeleteRejectsPartialClientCleanup(t *testing.T) {
	resourceRoot := t.TempDir()
	projectRoot := t.TempDir()
	if err := os.Mkdir(filepath.Join(projectRoot, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	skillDir := filepath.Join(resourceRoot, "skills", "local", "shared", "core", "alpha")
	writeCLISkill(t, skillDir, "alpha")
	base := []string{"--resources", resourceRoot, "--project", projectRoot}
	_, err := execute(t, append(base, "skills", "delete", "local-shared/core/alpha", "--yes", "--client", "codex")...)
	if err == nil || !strings.Contains(err.Error(), "cannot limit cleanup") {
		t.Fatalf("partial delete error = %v", err)
	}
	if _, err := os.Stat(filepath.Join(skillDir, "SKILL.md")); err != nil {
		t.Fatalf("partial delete removed the shared provider: %v", err)
	}
}

func TestMCPAddAndRemoveRoundTrip(t *testing.T) {
	resourceRoot := t.TempDir()
	projectRoot := t.TempDir()
	if err := os.Mkdir(filepath.Join(projectRoot, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	sourcesRoot := filepath.Join(resourceRoot, "skills")
	writeCLISkill(t, filepath.Join(sourcesRoot, "local", "shared", "core", "alpha"), "alpha")
	base := []string{"--resources", resourceRoot, "--project", projectRoot}

	if _, err := execute(t, append(base, "mcp", "add", "grafana", "--url", "https://mcp.example.com")...); err != nil {
		t.Fatalf("mcp add http: %v", err)
	}
	if _, err := execute(t, append(base, "mcp", "add", "context7", "--command", "npx", "--arg", "-y")...); err != nil {
		t.Fatalf("mcp add stdio: %v", err)
	}
	out, err := execute(t, append(base, "mcp", "list", "--json")...)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(out), "grafana") || !strings.Contains(string(out), "context7") {
		t.Fatalf("added servers not listed: %s", out)
	}
	if _, err := execute(t, append(base, "mcp", "add", "grafana", "--url", "https://x")...); err == nil {
		t.Fatal("duplicate add must fail")
	}
	if _, err := execute(t, append(base, "mcp", "add", "ambiguous")...); err == nil {
		t.Fatal("add without --command or --url must fail")
	}
	if _, err := execute(t, append(base, "mcp", "remove", "grafana")...); err != nil {
		t.Fatalf("mcp remove: %v", err)
	}
	out, err = execute(t, append(base, "mcp", "list", "--json")...)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(out), "grafana") {
		t.Fatalf("removed server still listed: %s", out)
	}
	if !strings.Contains(string(out), "context7") {
		t.Fatalf("unrelated server dropped: %s", out)
	}
}

func TestSkillsCreateScaffoldsADiscoverableLocalSkill(t *testing.T) {
	resourceRoot := t.TempDir()
	projectRoot := t.TempDir()
	if err := os.Mkdir(filepath.Join(projectRoot, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	base := []string{"--resources", resourceRoot, "--project", projectRoot}

	if _, err := execute(t, append(base, "skills", "create", "make-goal", "--description", "Draft a goal.")...); err != nil {
		t.Fatalf("skills create: %v", err)
	}
	skillFile := filepath.Join(resourceRoot, "skills", "local", "shared", "make-goal", "SKILL.md")
	if _, err := os.Stat(skillFile); err != nil {
		t.Fatalf("scaffolded SKILL.md missing: %v", err)
	}

	out, err := execute(t, append(base, "skills", "list", "--json")...)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(out), "local-shared/make-goal/make-goal") {
		t.Fatalf("created skill not listed: %s", out)
	}

	// Re-creating the same skill fails.
	if _, err := execute(t, append(base, "skills", "create", "make-goal")...); err == nil {
		t.Fatal("duplicate skills create must fail")
	}
}

func TestSkillCommandsIgnoreMCPOnlyClientsAndRejectTheirScope(t *testing.T) {
	resourceRoot := t.TempDir()
	projectRoot := t.TempDir()
	if err := os.Mkdir(filepath.Join(projectRoot, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	registry := `version: 1
clients:
  mcp-only:
    projectMCPFile: .mcp-only.json
    projectMCPFormat: claude-json
`
	if err := os.WriteFile(filepath.Join(resourceRoot, "registry.yaml"), []byte(registry), 0o644); err != nil {
		t.Fatal(err)
	}
	base := []string{"--resources", resourceRoot, "--project", projectRoot}
	if _, err := execute(t, append(base, "skills", "create", "valid")...); err != nil {
		t.Fatal(err)
	}
	for _, command := range [][]string{{"skills", "list", "--json"}, {"status", "--json"}, {"doctor", "--json"}} {
		if _, err := execute(t, append(base, command...)...); err != nil {
			t.Fatalf("%v failed because of an MCP-only client: %v", command, err)
		}
	}
	if _, err := execute(t, append(base, "skills", "create", "invalid", "--scope", "mcp-only")...); err == nil || !strings.Contains(err.Error(), "does not support skills") {
		t.Fatalf("MCP-only Skill scope error = %v", err)
	}
	if _, err := os.Stat(filepath.Join(resourceRoot, "skills", "local", "mcp-only")); !os.IsNotExist(err) {
		t.Fatalf("invalid scope wrote to disk: %v", err)
	}
}

func TestSourceAddRequiresDerivableNameOrExplicitFlag(t *testing.T) {
	resourceRoot := t.TempDir()
	_, err := execute(t, "--resources", resourceRoot, "source", "add", "https://example.com/")
	if err == nil {
		t.Fatal("source add with no derivable name must fail")
	}
	if !strings.Contains(err.Error(), "source name is required") {
		t.Fatalf("error = %v, want name-required message", err)
	}
}

func TestMCPImportAddsWrapperAndBareDefinitions(t *testing.T) {
	resourceRoot := t.TempDir()
	projectRoot := t.TempDir()
	if err := os.Mkdir(filepath.Join(projectRoot, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	sourcesRoot := filepath.Join(resourceRoot, "skills")
	writeCLISkill(t, filepath.Join(sourcesRoot, "local", "shared", "core", "alpha"), "alpha")
	base := []string{"--resources", resourceRoot, "--project", projectRoot}

	wrapper := `{"mcpServers":{"grafana":{"type":"http","url":"https://mcp.example.com"},"context7":{"command":"npx"}}}`
	if _, err := execute(t, append(base, "mcp", "import", wrapper)...); err != nil {
		t.Fatalf("import wrapper: %v", err)
	}
	out, err := execute(t, append(base, "mcp", "list", "--json")...)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(out), "grafana") || !strings.Contains(string(out), "context7") {
		t.Fatalf("imported wrapper servers not listed: %s", out)
	}

	if _, err := execute(t, append(base, "mcp", "import", `{"command":"deno"}`)...); err == nil {
		t.Fatal("bare object without --name must fail")
	}
	if _, err := execute(t, append(base, "mcp", "import", `{"command":"deno"}`, "--name", "denomcp")...); err != nil {
		t.Fatalf("import bare with --name: %v", err)
	}

	defFile := filepath.Join(t.TempDir(), "server.json")
	if err := os.WriteFile(defFile, []byte(`{"mcpServers":{"fromfile":{"url":"https://f"}}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := execute(t, append(base, "mcp", "import", "--file", defFile)...); err != nil {
		t.Fatalf("import from file: %v", err)
	}
	out, err = execute(t, append(base, "mcp", "list", "--json")...)
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"grafana", "context7", "denomcp", "fromfile"} {
		if !strings.Contains(string(out), name) {
			t.Fatalf("server %q missing after imports: %s", name, out)
		}
	}

	// A wrapper where one name already exists is rejected whole: the new sibling
	// must not be partially written.
	if _, err := execute(t, append(base, "mcp", "import", `{"mcpServers":{"grafana":{"url":"https://y"},"brandnew":{"url":"https://z"}}}`)...); err == nil {
		t.Fatal("import with a pre-existing name must fail")
	}
	out, err = execute(t, append(base, "mcp", "list", "--json")...)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(out), "brandnew") {
		t.Fatalf("partial import wrote a server despite a conflict: %s", out)
	}
}
