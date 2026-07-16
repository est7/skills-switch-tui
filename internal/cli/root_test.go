package cli

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/est7/skills-switch-tui/internal/systemprompt"
)

func TestDefaultResourcesRootUsesResourceFirstHierarchy(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("SKILLS_SWITCH_RESOURCES", "")

	got, err := resolveResourcesRoot("")
	if err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(home, ".agents", "resources")
	if got != want {
		t.Fatalf("resolveResourcesRoot() = %q, want %q", got, want)
	}
}

func TestMCPCommandsAppendAndRemoveOnlyManagedServer(t *testing.T) {
	resourceRoot := t.TempDir()
	sourcesRoot := filepath.Join(resourceRoot, "skills")
	projectRoot := filepath.Join(t.TempDir(), "project")
	if err := os.MkdirAll(filepath.Join(sourcesRoot, "local", "shared"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(projectRoot, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	mcpDir := filepath.Join(resourceRoot, "mcp")
	if err := os.MkdirAll(mcpDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(mcpDir, "mcp.json"), []byte(`{"mcpServers":{"context7":{"command":"npx"}}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	projectConfig := filepath.Join(projectRoot, ".mcp.json")
	if err := os.WriteFile(projectConfig, []byte(`{"mcpServers":{"project-owned":{"command":"keep"}}}`), 0o644); err != nil {
		t.Fatal(err)
	}

	if _, err := execute(t, "--resources", resourceRoot, "--project", projectRoot, "mcp", "enable", "context7", "--client", "claude"); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(projectConfig)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(data, []byte(`"project-owned"`)) || !bytes.Contains(data, []byte(`"context7"`)) {
		t.Fatalf("MCP enable did not merge entries: %s", data)
	}

	if _, err := execute(t, "--resources", resourceRoot, "--project", projectRoot, "mcp", "disable", "context7", "--client", "claude"); err != nil {
		t.Fatal(err)
	}
	data, err = os.ReadFile(projectConfig)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(data, []byte(`"project-owned"`)) || bytes.Contains(data, []byte(`"context7"`)) {
		t.Fatalf("MCP disable removed the wrong entry: %s", data)
	}
}

func TestPromptCommandsUseUserGlobalRecursiveClientGroupWithoutGitProject(t *testing.T) {
	resourceRoot := t.TempDir()
	sourcesRoot := filepath.Join(resourceRoot, "skills")
	userHome := t.TempDir()
	t.Setenv("HOME", userHome)
	if err := os.MkdirAll(filepath.Join(sourcesRoot, "local", "shared"), 0o755); err != nil {
		t.Fatal(err)
	}
	for _, relative := range []string{"CLAUDE.md", filepath.Join("rules", "core.md")} {
		path := filepath.Join(resourceRoot, "system-prompts", "claude-prompt", relative)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte("# Prompt\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	if _, err := execute(t, "--resources", resourceRoot, "--project", filepath.Join(t.TempDir(), "not-a-git-project"), "prompt", "enable", "claude-prompt"); err != nil {
		t.Fatal(err)
	}
	for _, relative := range []string{"CLAUDE.md", filepath.Join("rules", "core.md")} {
		if _, err := os.Readlink(filepath.Join(userHome, ".claude", relative)); err != nil {
			t.Fatalf("prompt file %s was not projected: %v", relative, err)
		}
	}
}

func TestPromptBuildCompilesCodexSourcesAndReportsStaleState(t *testing.T) {
	resourceRoot := t.TempDir()
	userHome := t.TempDir()
	projectRoot := t.TempDir()
	t.Setenv("HOME", userHome)
	for _, directory := range []string{
		filepath.Join(resourceRoot, "skills", "local", "shared"),
		filepath.Join(projectRoot, ".git"),
	} {
		if err := os.MkdirAll(directory, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	base := filepath.Join(resourceRoot, "system-prompts", "codex-prompt", "AGENTS.md")
	rule := filepath.Join(resourceRoot, "system-prompts", "codex-prompt", "rules", "10-core.md")
	for path, contents := range map[string]string{base: "# Base\n", rule: "## Core\n"} {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(contents), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	output, err := execute(t, "--resources", resourceRoot, "prompt", "build", "codex-prompt", "--json")
	if err != nil {
		t.Fatal(err)
	}
	var built systemprompt.BuildResult
	if err := json.Unmarshal(output, &built); err != nil {
		t.Fatal(err)
	}
	if !built.Changed || built.Bytes == 0 || !strings.HasSuffix(built.Path, filepath.Join("codex-prompt", "AGENTS.md")) {
		t.Fatalf("unexpected build output: %#v", built)
	}
	if _, err := os.Lstat(filepath.Join(userHome, ".codex", "AGENTS.md")); !os.IsNotExist(err) {
		t.Fatalf("build unexpectedly enabled the prompt: %v", err)
	}
	if _, err := execute(t, "--resources", resourceRoot, "prompt", "enable", "codex-prompt"); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(rule, []byte("## Core changed\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	listed, err := execute(t, "--resources", resourceRoot, "prompt", "list", "--json")
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(listed, []byte(`"state": "stale"`)) || !bytes.Contains(listed, []byte(`"mode": "concat"`)) {
		t.Fatalf("prompt list did not expose concat stale state:\n%s", listed)
	}
	diagnosis, err := execute(t, "--resources", resourceRoot, "--project", projectRoot, "doctor", "--json")
	if err == nil || !bytes.Contains(diagnosis, []byte(`"state": "stale"`)) {
		t.Fatalf("doctor did not report stale prompt as an issue: %v\n%s", err, diagnosis)
	}
	if _, err := execute(t, "--resources", resourceRoot, "prompt", "build", "codex-prompt"); err != nil {
		t.Fatal(err)
	}
	listed, err = execute(t, "--resources", resourceRoot, "prompt", "list", "--json")
	if err != nil || !bytes.Contains(listed, []byte(`"state": "enabled"`)) {
		t.Fatalf("prompt rebuild did not repair state: %v\n%s", err, listed)
	}
}

func TestCommandAndHookCommandsUseScopedProjectProjections(t *testing.T) {
	resourceRoot := t.TempDir()
	userHome := t.TempDir()
	projectRoot := t.TempDir()
	t.Setenv("HOME", userHome)
	for _, directory := range []string{filepath.Join(resourceRoot, "skills", "local", "shared"), filepath.Join(projectRoot, ".git")} {
		if err := os.MkdirAll(directory, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	commandSource := filepath.Join(resourceRoot, "commands", "shared", "remember.md")
	hookSource := filepath.Join(resourceRoot, "hooks", "claude-only", "audit.sh")
	for _, path := range []string{commandSource, hookSource} {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte("test\n"), 0o755); err != nil {
			t.Fatal(err)
		}
	}

	if _, err := execute(t, "--resources", resourceRoot, "--project", projectRoot, "commands", "enable", "shared/remember.md", "--client", "claude", "--client", "codex"); err != nil {
		t.Fatal(err)
	}
	if _, err := execute(t, "--resources", resourceRoot, "--project", projectRoot, "hooks", "enable", "claude-only/audit.sh", "--client", "claude"); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{
		filepath.Join(projectRoot, ".claude", "commands", "remember.md"),
		filepath.Join(projectRoot, ".codex", "prompts", "remember.md"),
		filepath.Join(projectRoot, ".claude", "hooks", "audit.sh"),
	} {
		if _, err := os.Readlink(path); err != nil {
			t.Fatalf("missing user resource projection %s: %v", path, err)
		}
	}
	if _, err := execute(t, "--resources", resourceRoot, "--project", projectRoot, "hooks", "enable", "claude-only/audit.sh", "--client", "gemini"); err == nil {
		t.Fatal("client-only hook was enabled for an incompatible client")
	}
}

func TestAgentAndOutputStyleCommandsUseUserGlobalAdapters(t *testing.T) {
	resourceRoot := t.TempDir()
	userHome := t.TempDir()
	t.Setenv("HOME", userHome)
	if err := os.MkdirAll(filepath.Join(resourceRoot, "skills", "local", "shared"), 0o755); err != nil {
		t.Fatal(err)
	}
	for path, contents := range map[string]string{
		filepath.Join(resourceRoot, "agents", "claude-only", "reviewer.md"):      "reviewer\n",
		filepath.Join(resourceRoot, "agents", "codex-only", "reviewer.toml"):     "name = \"reviewer\"\n",
		filepath.Join(resourceRoot, "output-styles", "claude-only", "mentor.md"): "mentor\n",
	} {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(contents), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	commands := [][]string{
		{"agents", "enable", "claude-only/reviewer.md", "--client", "claude"},
		{"agents", "enable", "codex-only/reviewer.toml", "--client", "codex"},
		{"output-styles", "enable", "claude-only/mentor.md", "--client", "claude"},
	}
	for _, arguments := range commands {
		if _, err := execute(t, append([]string{"--resources", resourceRoot}, arguments...)...); err != nil {
			t.Fatal(err)
		}
	}
	for _, path := range []string{
		filepath.Join(userHome, ".claude", "agents", "reviewer.md"),
		filepath.Join(userHome, ".codex", "agents", "reviewer.toml"),
		filepath.Join(userHome, ".claude", "output-styles", "mentor.md"),
	} {
		if _, err := os.Readlink(path); err != nil {
			t.Fatalf("missing user-global projection %s: %v", path, err)
		}
	}
	if _, err := execute(t, "--resources", resourceRoot, "output-styles", "enable", "claude-only/mentor.md", "--client", "codex"); err == nil {
		t.Fatal("Claude-only output style was enabled for Codex")
	}
}

func TestDoctorReportsProjectCommandConflict(t *testing.T) {
	resourceRoot := t.TempDir()
	userHome := t.TempDir()
	projectRoot := t.TempDir()
	t.Setenv("HOME", userHome)
	for _, directory := range []string{
		filepath.Join(resourceRoot, "skills", "local", "shared"),
		filepath.Join(resourceRoot, "commands", "shared"),
		filepath.Join(projectRoot, ".git"),
		filepath.Join(projectRoot, ".claude", "commands"),
	} {
		if err := os.MkdirAll(directory, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(resourceRoot, "commands", "shared", "remember.md"), []byte("catalog\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	conflict := filepath.Join(projectRoot, ".claude", "commands", "remember.md")
	if err := os.WriteFile(conflict, []byte("user owned\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	output, err := execute(t, "--resources", resourceRoot, "--project", projectRoot, "doctor", "--json")
	if err == nil {
		t.Fatal("doctor accepted an unmanaged command conflict")
	}
	for _, fragment := range []string{`"kind": "command"`, `"resource": "shared/remember.md"`, `"client": "claude"`, `"state": "conflict"`} {
		if !bytes.Contains(output, []byte(fragment)) {
			t.Fatalf("doctor output does not contain %s:\n%s", fragment, output)
		}
	}
}

func TestEnableThenListReportsProjectState(t *testing.T) {
	resourceRoot := t.TempDir()
	sourcesRoot := filepath.Join(resourceRoot, "skills")
	projectRoot := t.TempDir()
	if err := os.Mkdir(filepath.Join(projectRoot, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	skillDir := filepath.Join(sourcesRoot, "local", "shared", "demo", "worktrunk")
	if err := os.MkdirAll(skillDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte("---\nname: worktrunk\ndescription: Manage worktrees.\n---\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	if _, err := execute(t,
		"--resources", resourceRoot,
		"--project", projectRoot,
		"skills", "enable", "local-shared/demo/worktrunk",
		"--client", "codex",
	); err != nil {
		t.Fatalf("enable command: %v", err)
	}

	output, err := execute(t,
		"--resources", resourceRoot,
		"--project", projectRoot,
		"skills", "list", "--json",
	)
	if err != nil {
		t.Fatalf("list command: %v", err)
	}
	var result struct {
		Skills []struct {
			ID      string            `json:"id"`
			Clients map[string]string `json:"clients"`
		} `json:"skills"`
	}
	if err := json.Unmarshal(output, &result); err != nil {
		t.Fatalf("decode list output %q: %v", output, err)
	}
	if got, want := len(result.Skills), 1; got != want {
		t.Fatalf("skill count = %d, want %d", got, want)
	}
	if got, want := result.Skills[0].ID, "local-shared/demo/worktrunk"; got != want {
		t.Fatalf("skill id = %q, want %q", got, want)
	}
	if got, want := result.Skills[0].Clients["codex"], "enabled"; got != want {
		t.Fatalf("codex state = %q, want %q", got, want)
	}
}

func TestConfiguredClientCanBeEnabledWithoutCodeChanges(t *testing.T) {
	resourceRoot := t.TempDir()
	sourcesRoot := filepath.Join(resourceRoot, "skills")
	projectRoot := t.TempDir()
	if err := os.Mkdir(filepath.Join(projectRoot, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	writeCLISkill(t, filepath.Join(sourcesRoot, "local", "shared", "base", "portable"), "portable")
	config := "version: 1\nclients:\n  pi:\n    projectSkillsDir: .pi/skills\n"
	if err := os.WriteFile(filepath.Join(resourceRoot, "registry.yaml"), []byte(config), 0o644); err != nil {
		t.Fatal(err)
	}

	if _, err := execute(t,
		"--resources", resourceRoot,
		"--project", projectRoot,
		"skills", "enable", "local-shared/base/portable",
		"--client", "pi",
	); err != nil {
		t.Fatalf("enable pi: %v", err)
	}
	if _, err := os.Readlink(filepath.Join(projectRoot, ".pi", "skills", "portable")); err != nil {
		t.Fatalf("pi projection was not created: %v", err)
	}
}

func TestResourcesFlagReplacesPreReleaseSourcesFlag(t *testing.T) {
	command := NewRootCommand("test")
	if command.PersistentFlags().Lookup("resources") == nil {
		t.Fatal("--resources flag is missing")
	}
	if command.PersistentFlags().Lookup("sources") != nil {
		t.Fatal("pre-release --sources compatibility unexpectedly remains")
	}
}

func TestInitCommandIsProjectIndependentAndLocalized(t *testing.T) {
	command := NewRootCommand("test")
	initCommand, _, err := command.Find([]string{"init"})
	if err != nil {
		t.Fatal(err)
	}
	if initCommand == nil || initCommand.Name() != "init" {
		t.Fatal("init command is not registered")
	}
	if initCommand.Flags().Lookup("json") == nil {
		t.Fatal("init command is missing --json")
	}

	help, err := execute(t, "--lang", "zh", "init", "--help")
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(help, []byte("初始化 Agent 资源和内置操作 Skill")) {
		t.Fatalf("Chinese init help was not localized:\n%s", help)
	}
}

func TestChineseLanguageLocalizesHelpAndHumanListHeaders(t *testing.T) {
	resourceRoot := t.TempDir()
	sourcesRoot := filepath.Join(resourceRoot, "skills")
	projectRoot := t.TempDir()
	if err := os.Mkdir(filepath.Join(projectRoot, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	writeCLISkill(t, filepath.Join(sourcesRoot, "local", "shared", "base", "portable"), "portable")

	help, err := execute(t, "--lang", "zh", "--help")
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(help, []byte("管理项目资源和用户级 Agent 文件")) {
		t.Fatalf("Chinese help was not localized:\n%s", help)
	}

	output, err := execute(t,
		"--lang", "zh",
		"--resources", resourceRoot,
		"--project", projectRoot,
		"skills", "list",
	)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(output, []byte("来源")) {
		t.Fatalf("Chinese list header was not localized:\n%s", output)
	}
	sourceHelp, err := execute(t, "--lang", "zh", "source", "add", "--help")
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(sourceHelp, []byte("来源发现策略优先级")) {
		t.Fatalf("Chinese discovery priority flag was not localized:\n%s", sourceHelp)
	}
	for _, want := range []string{"将整个来源限制为一个已注册客户端", "权威 Skill 目录路径"} {
		if !bytes.Contains(sourceHelp, []byte(want)) {
			t.Fatalf("Chinese source add help omitted %q:\n%s", want, sourceHelp)
		}
	}
}

func TestChineseLanguageLocalizesResourceSelectionErrors(t *testing.T) {
	resourceRoot := t.TempDir()
	projectRoot := t.TempDir()
	if err := os.Mkdir(filepath.Join(projectRoot, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(resourceRoot, "mcp"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(resourceRoot, "skills"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(resourceRoot, "mcp", "mcp.json"), []byte(`{"mcpServers":{}}`), 0o644); err != nil {
		t.Fatal(err)
	}

	_, err := execute(t, "--lang", "zh", "--resources", resourceRoot, "--project", projectRoot, "mcp", "enable", "missing", "--client", "claude")
	if err == nil || !strings.Contains(err.Error(), "未知 MCP server") {
		t.Fatalf("MCP error = %v, want Chinese unknown-server error", err)
	}
	_, err = execute(t, "--lang", "zh", "--resources", resourceRoot, "prompt", "enable", "missing")
	if err == nil || !strings.Contains(err.Error(), "未知系统提示词组") {
		t.Fatalf("prompt error = %v, want Chinese unknown-group error", err)
	}
	_, err = execute(t, "--lang", "zh", "--resources", resourceRoot, "source", "add", "https://example.invalid/repo.git", "--name", "repo", "--client", "pi")
	if err == nil || !strings.Contains(err.Error(), "未知客户端") {
		t.Fatalf("source error = %v, want Chinese unknown-client error", err)
	}
}

func TestSourceListReportsResolvedDiscoveryStrategy(t *testing.T) {
	resourceRoot := t.TempDir()
	sourcesRoot := filepath.Join(resourceRoot, "skills")
	vendorRoot := filepath.Join(sourcesRoot, "vendor", "shared", "curated")
	writeCLISkill(t, filepath.Join(vendorRoot, "skills", "registered"), "registered")
	manifestPath := filepath.Join(vendorRoot, ".claude-plugin", "plugin.json")
	if err := os.MkdirAll(filepath.Dir(manifestPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(manifestPath, []byte(`{
  "name": "curated",
  "skills": ["./skills/registered"]
}`), 0o644); err != nil {
		t.Fatal(err)
	}
	config := `version: 1
sources:
  vendor-shared/curated:
    discoveryPriority: [claude-plugin, skills-dir]
`
	if err := os.WriteFile(filepath.Join(sourcesRoot, "catalog.yaml"), []byte(config), 0o644); err != nil {
		t.Fatal(err)
	}

	output, err := execute(t, "--resources", resourceRoot, "source", "list", "--json")
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(output, []byte(`"discoveryStrategy": "claude-plugin"`)) {
		t.Fatalf("source JSON omitted resolved discovery strategy: %s", output)
	}
	if !bytes.Contains(output, []byte(`"discoveryPriority"`)) {
		t.Fatalf("source JSON omitted discovery priority: %s", output)
	}
	if !bytes.Contains(output, []byte(`"scope": "shared"`)) {
		t.Fatalf("source JSON omitted physical scope: %s", output)
	}
}

func TestArchivedSkillsRequireExplicitListingAndCannotBeEnabled(t *testing.T) {
	resourceRoot := t.TempDir()
	sourcesRoot := filepath.Join(resourceRoot, "skills")
	projectRoot := t.TempDir()
	if err := os.Mkdir(filepath.Join(projectRoot, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	writeCLISkill(t, filepath.Join(sourcesRoot, "archived", "shared", "waza", "read"), "waza-read")

	withoutArchive, err := execute(t, "--resources", resourceRoot, "--project", projectRoot, "skills", "list", "--json")
	if err != nil {
		t.Fatal(err)
	}
	withArchive, err := execute(t, "--resources", resourceRoot, "--project", projectRoot, "skills", "list", "--archive", "--json")
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(withoutArchive, []byte("archived-shared/waza/read")) {
		t.Fatalf("archive leaked into default list: %s", withoutArchive)
	}
	if !bytes.Contains(withArchive, []byte("archived-shared/waza/read")) {
		t.Fatalf("explicit archive list omitted skill: %s", withArchive)
	}

	_, err = execute(t,
		"--resources", resourceRoot,
		"--project", projectRoot,
		"skills", "enable", "archived-shared/waza/read",
		"--client", "codex",
	)
	if err == nil {
		t.Fatal("enable accepted an archived skill")
	}
	if _, statErr := os.Lstat(filepath.Join(projectRoot, ".agents", "skills", "waza-read")); !os.IsNotExist(statErr) {
		t.Fatalf("archived skill projection unexpectedly exists: %v", statErr)
	}
}

func TestShowStatusAndDoctorExposeStableJSON(t *testing.T) {
	resourceRoot := t.TempDir()
	sourcesRoot := filepath.Join(resourceRoot, "skills")
	projectRoot := t.TempDir()
	if err := os.Mkdir(filepath.Join(projectRoot, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	writeCLISkill(t, filepath.Join(sourcesRoot, "local", "shared", "base", "portable"), "portable")
	base := []string{"--resources", resourceRoot, "--project", projectRoot}
	if _, err := execute(t, append(base, "skills", "enable", "local-shared/base/portable", "--client", "codex")...); err != nil {
		t.Fatal(err)
	}

	show, err := execute(t, append(base, "skills", "show", "local-shared/base/portable", "--json")...)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(show, []byte(`"codex"`)) || !bytes.Contains(show, []byte(`"enabled"`)) {
		t.Fatalf("show JSON missing projection state: %s", show)
	}

	status, err := execute(t, append(base, "status", "--json")...)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(status, []byte(`"enabled": 1`)) {
		t.Fatalf("status JSON missing enabled count: %s", status)
	}

	doctor, err := execute(t, append(base, "doctor", "--json")...)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(doctor, []byte(`"healthy": true`)) {
		t.Fatalf("doctor JSON did not report healthy state: %s", doctor)
	}
}

func TestMultiClientEnableIsAtomicAcrossClientDirectories(t *testing.T) {
	resourceRoot := t.TempDir()
	sourcesRoot := filepath.Join(resourceRoot, "skills")
	projectRoot := t.TempDir()
	if err := os.Mkdir(filepath.Join(projectRoot, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	writeCLISkill(t, filepath.Join(sourcesRoot, "local", "shared", "base", "portable"), "portable")
	conflict := filepath.Join(projectRoot, ".claude", "skills", "portable")
	if err := os.MkdirAll(conflict, 0o755); err != nil {
		t.Fatal(err)
	}

	_, err := execute(t,
		"--resources", resourceRoot,
		"--project", projectRoot,
		"skills", "enable", "local-shared/base/portable",
		"--client", "codex",
		"--client", "claude",
	)
	if err == nil {
		t.Fatal("enable succeeded despite a Claude conflict")
	}
	if _, statErr := os.Lstat(filepath.Join(projectRoot, ".agents", "skills", "portable")); !os.IsNotExist(statErr) {
		t.Fatalf("Codex projection changed before full preflight: %v", statErr)
	}
}

func writeCLISkill(t *testing.T, directory, name string) {
	t.Helper()
	if err := os.MkdirAll(directory, 0o755); err != nil {
		t.Fatal(err)
	}
	contents := "---\nname: " + name + "\ndescription: test\n---\n"
	if err := os.WriteFile(filepath.Join(directory, "SKILL.md"), []byte(contents), 0o644); err != nil {
		t.Fatal(err)
	}
}

func execute(t *testing.T, args ...string) ([]byte, error) {
	t.Helper()
	var stdout bytes.Buffer
	command := NewRootCommand("test")
	command.SetOut(&stdout)
	command.SetErr(&stdout)
	command.SetArgs(args)
	err := command.Execute()
	return stdout.Bytes(), err
}
