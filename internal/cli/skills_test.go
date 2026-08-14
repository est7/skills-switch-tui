package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestSkillsDiscoverReportsUnmanagedProjectAndGlobalSkillsInStableOrder(t *testing.T) {
	resourceRoot := t.TempDir()
	projectRoot := t.TempDir()
	userHome := t.TempDir()
	t.Setenv("HOME", userHome)
	if err := os.Mkdir(filepath.Join(resourceRoot, "skills"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(projectRoot, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}

	claudeProjectSkill := filepath.Join(projectRoot, ".claude", "skills", "beta")
	codexProjectSkill := filepath.Join(projectRoot, ".agents", "skills", "zeta")
	codexGlobalSkill := filepath.Join(userHome, ".agents", "skills", "global-tool")
	writeCLISkill(t, claudeProjectSkill, "beta")
	writeCLISkill(t, codexProjectSkill, "zeta")
	writeCLISkill(t, codexGlobalSkill, "global-tool")

	codexProjectTarget := filepath.Join(projectRoot, ".agents", "skills")
	if err := os.MkdirAll(filepath.Join(codexProjectTarget, "not-a-skill"), 0o755); err != nil {
		t.Fatal(err)
	}
	managedTarget := filepath.Join(t.TempDir(), "managed")
	writeCLISkill(t, managedTarget, "managed")
	if err := os.Symlink(managedTarget, filepath.Join(codexProjectTarget, "managed")); err != nil {
		t.Fatal(err)
	}

	base := []string{"--resources", resourceRoot, "--project", projectRoot, "skills", "discover"}
	output, err := execute(t, append(base, "--json")...)
	if err != nil {
		t.Fatal(err)
	}
	type row struct {
		Client string `json:"client"`
		Scope  string `json:"scope"`
		Name   string `json:"name"`
		Path   string `json:"path"`
	}
	var got []row
	if err := json.Unmarshal(output, &got); err != nil {
		t.Fatalf("decode discover JSON: %v\n%s", err, output)
	}
	want := []row{
		{Client: "claude", Scope: "project", Name: "beta", Path: claudeProjectSkill},
		{Client: "codex", Scope: "global", Name: "global-tool", Path: codexGlobalSkill},
		{Client: "codex", Scope: "project", Name: "zeta", Path: codexProjectSkill},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("skills discover = %#v, want %#v", got, want)
	}

	human, err := execute(t, append(base, "--scope", "project")...)
	if err != nil {
		t.Fatal(err)
	}
	wantFields := []string{
		"CLIENT", "SCOPE", "NAME", "PATH",
		"claude", "project", "beta", claudeProjectSkill,
		"codex", "project", "zeta", codexProjectSkill,
	}
	if fields := strings.Fields(string(human)); !reflect.DeepEqual(fields, wantFields) {
		t.Fatalf("skills discover table fields = %#v, want %#v\n%s", fields, wantFields, human)
	}
}

func TestSkillsDiscoverMissingTargetsReturnsEmptyJSON(t *testing.T) {
	resourceRoot := t.TempDir()
	projectRoot := t.TempDir()
	t.Setenv("HOME", t.TempDir())
	if err := os.Mkdir(filepath.Join(resourceRoot, "skills"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(projectRoot, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}

	output, err := execute(t, "--resources", resourceRoot, "--project", projectRoot, "skills", "discover", "--json")
	if err != nil {
		t.Fatalf("skills discover with missing targets: %v", err)
	}
	if got := strings.TrimSpace(string(output)); got != "[]" {
		t.Fatalf("empty discovery JSON = %q, want []", got)
	}
}

func TestSkillsDiscoverHelpIsLocalized(t *testing.T) {
	help, err := execute(t, "--lang", "zh", "skills", "discover", "--help")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"发现客户端目标目录中未受管理的 Skills", "发现作用域：project、global 或 all", "输出 JSON"} {
		if !strings.Contains(string(help), want) {
			t.Fatalf("Chinese skills discover help omitted %q:\n%s", want, help)
		}
	}
}

func TestSkillsAdoptJSONMovesUnmanagedSkillAndReportsStableResult(t *testing.T) {
	resourceRoot := t.TempDir()
	projectRoot := t.TempDir()
	t.Setenv("HOME", t.TempDir())
	if err := os.MkdirAll(filepath.Join(resourceRoot, "skills"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(projectRoot, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	original := filepath.Join(projectRoot, ".agents", "skills", "portable")
	writeCLISkill(t, original, "portable")

	output, err := execute(t,
		"--resources", resourceRoot,
		"--project", projectRoot,
		"skills", "adopt", original,
		"--group", "tools",
		"--json",
	)
	if err != nil {
		t.Fatal(err)
	}
	type row struct {
		Path     string `json:"path"`
		SkillID  string `json:"skillId"`
		SSOTPath string `json:"ssotPath"`
		Status   string `json:"status"`
		Reason   string `json:"reason"`
	}
	var got []row
	if err := json.Unmarshal(output, &got); err != nil {
		t.Fatalf("decode adopt JSON: %v\n%s", err, output)
	}
	ssot := filepath.Join(resourceRoot, "skills", "local", "shared", "tools", "portable")
	want := []row{{Path: original, SkillID: "local-shared/tools/portable", SSOTPath: ssot, Status: "adopted"}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("skills adopt = %#v, want %#v", got, want)
	}
	if target, err := os.Readlink(original); err != nil || target != ssot {
		t.Fatalf("adopted projection = %q, %v", target, err)
	}
}

func TestSkillsAdoptRendersEveryOutcomeBeforeReturningAggregateError(t *testing.T) {
	resourceRoot := t.TempDir()
	projectRoot := t.TempDir()
	t.Setenv("HOME", t.TempDir())
	if err := os.MkdirAll(filepath.Join(resourceRoot, "skills"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(projectRoot, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	outside := filepath.Join(t.TempDir(), "outside")
	writeCLISkill(t, outside, "outside")
	valid := filepath.Join(projectRoot, ".agents", "skills", "valid")
	writeCLISkill(t, valid, "valid")

	output, err := execute(t,
		"--resources", resourceRoot,
		"--project", projectRoot,
		"skills", "adopt", outside, valid,
		"--json",
	)
	if err == nil {
		t.Fatal("mixed adopt outcomes must return an error")
	}
	var got []map[string]any
	if decodeErr := json.Unmarshal(output, &got); decodeErr != nil {
		t.Fatalf("decode mixed adopt JSON: %v\n%s", decodeErr, output)
	}
	if len(got) != 2 || got[0]["status"] != "refused" || got[1]["status"] != "adopted" {
		t.Fatalf("mixed adopt results = %#v", got)
	}
}

func TestSkillsAdoptHelpIsLocalized(t *testing.T) {
	help, err := execute(t, "--lang", "zh", "skills", "adopt", "--help")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"将未受管理的 Skills 纳入本地目录", "本地作用域", "组目录", "输出 JSON"} {
		if !strings.Contains(string(help), want) {
			t.Fatalf("Chinese skills adopt help omitted %q:\n%s", want, help)
		}
	}
}

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
