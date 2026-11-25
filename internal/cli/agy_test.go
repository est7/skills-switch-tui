package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/est7/skills-switch-tui/internal/projection"
)

func TestCLIAgySkillsMCPAndDoctorEndToEnd(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	resourceRoot := t.TempDir()
	projectRoot := t.TempDir()
	if err := os.Mkdir(filepath.Join(projectRoot, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	skillDir := filepath.Join(resourceRoot, "skills", "local", "shared", "wiki")
	writeCLISkill(t, skillDir, "wiki")
	base := []string{"--resources", resourceRoot, "--project", projectRoot}

	// Enable wiki for agy in project scope: creates managed copy under .agents/skills/wiki.
	if _, err := execute(t, append(base, "skills", "enable", "local-shared/wiki/wiki", "--client", "agy")...); err != nil {
		t.Fatalf("skills enable --client agy: %v", err)
	}
	copyPath := filepath.Join(projectRoot, ".agents", "skills", "wiki")
	if info, err := os.Lstat(copyPath); err != nil || info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
		t.Fatalf("expected managed copy at %s, got info=%v err=%v", copyPath, info, err)
	}

	// Both agy and codex show enabled in skills list --json.
	listOut, err := execute(t, append(base, "skills", "list", "--json")...)
	if err != nil {
		t.Fatalf("skills list: %v", err)
	}
	var listed struct {
		Skills []struct {
			ID      string            `json:"id"`
			Clients map[string]string `json:"clients"`
		} `json:"skills"`
	}
	if err := json.Unmarshal(listOut, &listed); err != nil {
		t.Fatalf("unmarshal skills list: %v\n%s", err, listOut)
	}
	if len(listed.Skills) != 1 || listed.Skills[0].Clients["agy"] != "enabled" || listed.Skills[0].Clients["codex"] != "enabled" {
		t.Fatalf("unexpected skills list: %+v", listed)
	}

	// Enable an HTTP MCP server for agy: writes .agents/mcp_config.json with serverUrl.
	if _, err := execute(t, append(base, "mcp", "import", `{"mcpServers":{"remote":{"serverUrl":"https://mcp.example.com/mcp"}}}`)...); err != nil {
		t.Fatalf("mcp import: %v", err)
	}
	if _, err := execute(t, append(base, "mcp", "enable", "remote", "--client", "agy")...); err != nil {
		t.Fatalf("mcp enable --client agy: %v", err)
	}
	mcpBytes, err := os.ReadFile(filepath.Join(projectRoot, ".agents", "mcp_config.json"))
	if err != nil || !strings.Contains(string(mcpBytes), `"serverUrl"`) || strings.Contains(string(mcpBytes), `"type"`) {
		t.Fatalf("unexpected .agents/mcp_config.json: %s, %v", mcpBytes, err)
	}

	// Doctor is healthy when copy matches source.
	if _, err := execute(t, append(base, "doctor", "--json")...); err != nil {
		t.Fatalf("doctor expected healthy: %v", err)
	}

	// Modifying SSOT makes doctor report stale.
	if err := os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte("---\nname: wiki\ndescription: v2\n---\n# v2\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	doctorOut, doctorErr := execute(t, append(base, "doctor", "--json")...)
	if doctorErr == nil || !strings.Contains(string(doctorOut), `"stale"`) {
		t.Fatalf("doctor after SSOT edit = (%s, %v), want stale issue", doctorOut, doctorErr)
	}

	// Re-enabling refreshes the copy and doctor returns healthy.
	if _, err := execute(t, append(base, "skills", "enable", "local-shared/wiki/wiki", "--client", "agy")...); err != nil {
		t.Fatalf("re-enable to refresh stale copy: %v", err)
	}
	if _, err := execute(t, append(base, "doctor", "--json")...); err != nil {
		t.Fatalf("doctor after refresh: %v", err)
	}
}

func TestCLIDoctorReportsGlobalStaleAndOrphanedModifiedCopiesAccurately(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	resourceRoot := t.TempDir()
	projectRoot := t.TempDir()
	if err := os.Mkdir(filepath.Join(projectRoot, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	skillDir := filepath.Join(resourceRoot, "skills", "local", "shared", "wiki")
	writeCLISkill(t, skillDir, "wiki")
	base := []string{"--resources", resourceRoot, "--project", projectRoot}

	// Enable wiki for agy in project scope, then move that managed copy into agy's
	// global directory to simulate a global managed copy, and edit SSOT so it's stale.
	if _, err := execute(t, append(base, "skills", "enable", "local-shared/wiki/wiki", "--client", "agy")...); err != nil {
		t.Fatal(err)
	}
	projectCopy := filepath.Join(projectRoot, ".agents", "skills", "wiki")
	globalCopy := filepath.Join(home, ".gemini", "config", "skills", "wiki")
	if err := os.MkdirAll(filepath.Dir(globalCopy), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(projectCopy, globalCopy); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte("---\nname: wiki\ndescription: v2\n---\n# v2\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	doctorOut, doctorErr := execute(t, append(base, "doctor", "--json")...)
	if doctorErr == nil {
		t.Fatalf("doctor expected unhealthy for global stale copy:\n%s", doctorOut)
	}
	var report doctorOutput
	if err := json.Unmarshal(doctorOut, &report); err != nil {
		t.Fatalf("decode doctor JSON: %v\n%s", err, doctorOut)
	}
	if len(report.Issues) != 1 {
		t.Fatalf("doctor issues = %#v, want single global stale issue", report.Issues)
	}
	issue := report.Issues[0]
	if issue.Scope != "global" || issue.State != "stale" || issue.Path != globalCopy || issue.Client != "agy" {
		t.Fatalf("global stale issue = %+v, want scope=global state=stale client=agy path=%s", issue, globalCopy)
	}
	if err := os.RemoveAll(globalCopy); err != nil {
		t.Fatal(err)
	}

	// Orphaned locally modified copy: protected from `skills prune --yes` AND surfaced by `doctor`.
	if _, err := execute(t, append(base, "skills", "enable", "local-shared/wiki/wiki", "--client", "agy")...); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(projectCopy, "local-note.md"), []byte("keep\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.RemoveAll(skillDir); err != nil {
		t.Fatal(err)
	}
	pruneOut, pruneErr := execute(t, append(base, "skills", "prune", "--yes", "--json")...)
	if pruneErr != nil {
		t.Fatalf("skills prune --yes: %v\n%s", pruneErr, pruneOut)
	}
	if _, err := os.Stat(filepath.Join(projectCopy, "local-note.md")); err != nil {
		t.Fatalf("skills prune deleted locally modified orphan: %v", err)
	}
	doctorOut, doctorErr = execute(t, append(base, "doctor", "--json")...)
	if doctorErr == nil {
		t.Fatalf("doctor reported healthy despite orphaned modified copy:\n%s", doctorOut)
	}
	report = doctorOutput{}
	if err := json.Unmarshal(doctorOut, &report); err != nil {
		t.Fatalf("decode doctor JSON: %v\n%s", err, doctorOut)
	}
	if len(report.Issues) != 1 || report.Issues[0].State != "conflict" || report.Issues[0].Path != projectCopy {
		t.Fatalf("orphaned modified copy doctor issues = %#v, want conflict at %s", report.Issues, projectCopy)
	}
}

func TestCLISharedAgentsDirPeerGuardsEndToEnd(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	resourceRoot := t.TempDir()
	projectRoot := t.TempDir()
	if err := os.Mkdir(filepath.Join(projectRoot, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	writeCLISkill(t, filepath.Join(resourceRoot, "skills", "local", "shared", "wiki"), "wiki")
	base := []string{"--resources", resourceRoot, "--project", projectRoot}

	// Codex global blocks agy project enable.
	if _, err := execute(t, append(base, "skills", "enable", "local-shared/wiki/wiki", "--scope", "global", "--client", "codex")...); err != nil {
		t.Fatal(err)
	}
	if _, err := execute(t, append(base, "skills", "enable", "local-shared/wiki/wiki", "--client", "agy")...); err == nil || !strings.Contains(err.Error(), "codex") {
		t.Fatalf("expected agy project enable to fail naming codex, got %v", err)
	}
	if _, err := execute(t, append(base, "skills", "disable", "local-shared/wiki/wiki", "--scope", "global", "--client", "codex")...); err != nil {
		t.Fatal(err)
	}

	// Agy project blocks solo codex disable; disabling both succeeds.
	if _, err := execute(t, append(base, "skills", "enable", "local-shared/wiki/wiki", "--client", "agy")...); err != nil {
		t.Fatal(err)
	}
	if _, err := execute(t, append(base, "skills", "disable", "local-shared/wiki/wiki", "--client", "codex")...); err == nil || !strings.Contains(err.Error(), "agy") {
		t.Fatalf("expected solo codex disable to fail naming agy, got %v", err)
	}
	if _, err := execute(t, append(base, "skills", "disable", "local-shared/wiki/wiki", "--client", "codex", "--client", "agy")...); err != nil {
		t.Fatalf("disable codex+agy: %v", err)
	}
}

func TestUpdateOutputJSONOmitsEmptyRefreshedAndEmitsPopulatedRefreshed(t *testing.T) {
	emptyBytes, err := json.Marshal(updateOutput{Updates: nil, Pruned: []prunedLink{}, Failures: []updateFailure{}})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(emptyBytes), `"refreshed"`) {
		t.Fatalf("empty updateOutput unexpectedly included refreshed: %s", emptyBytes)
	}
	populatedBytes, err := json.Marshal(updateOutput{
		Pruned: []prunedLink{},
		Refreshed: []projection.RefreshedCopy{{
			Client: "agy", Scope: projection.ScopeProject, Name: "wiki", Path: "/p/.agents/skills/wiki", Source: "vendor-shared/repo",
		}},
		Failures: []updateFailure{},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(populatedBytes), `"refreshed"`) || !strings.Contains(string(populatedBytes), `"wiki"`) {
		t.Fatalf("populated updateOutput missing refreshed payload: %s", populatedBytes)
	}
}
