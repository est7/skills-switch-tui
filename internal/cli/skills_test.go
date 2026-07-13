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
