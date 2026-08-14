package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

// mutationFixture creates a resource root with one local skill and a git
// project, returning the shared CLI argument prefix.
func mutationFixture(t *testing.T) (resourceRoot, projectRoot string, base []string) {
	t.Helper()
	resourceRoot = t.TempDir()
	projectRoot = t.TempDir()
	if err := os.Mkdir(filepath.Join(projectRoot, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	writeCLISkill(t, filepath.Join(resourceRoot, "skills", "local", "shared", "core", "alpha"), "alpha")
	return resourceRoot, projectRoot, []string{"--resources", resourceRoot, "--project", projectRoot}
}

func decodeJSON[T any](t *testing.T, data []byte) T {
	t.Helper()
	var value T
	if err := json.Unmarshal(data, &value); err != nil {
		t.Fatalf("invalid JSON output %q: %v", data, err)
	}
	return value
}

func TestSkillsToggleJSONReportsAppliedOperations(t *testing.T) {
	_, projectRoot, base := mutationFixture(t)

	out, err := execute(t, append(base, "skills", "enable", "local-shared/core/alpha", "--client", "claude", "--json")...)
	if err != nil {
		t.Fatalf("skills enable --json: %v", err)
	}
	enabled := decodeJSON[skillToggleOutput](t, out)
	want := skillToggleOutput{
		Action:  "enable",
		Scope:   "project",
		Applied: []skillToggleClient{{Client: "claude", Skills: []string{"local-shared/core/alpha"}}},
	}
	if !reflect.DeepEqual(enabled, want) {
		t.Fatalf("enable output = %#v, want %#v", enabled, want)
	}
	if _, err := os.Lstat(filepath.Join(projectRoot, ".claude", "skills", "alpha")); err != nil {
		t.Fatalf("enable reported success without projecting: %v", err)
	}

	out, err = execute(t, append(base, "skills", "disable", "local-shared/core/alpha", "--client", "claude", "--json")...)
	if err != nil {
		t.Fatalf("skills disable --json: %v", err)
	}
	disabled := decodeJSON[skillToggleOutput](t, out)
	if disabled.Action != "disable" || len(disabled.Applied) != 1 || disabled.Applied[0].Skills[0] != "local-shared/core/alpha" {
		t.Fatalf("disable output = %#v", disabled)
	}
}

func TestSkillsDeleteJSONReportsRemovedTarget(t *testing.T) {
	resourceRoot, _, base := mutationFixture(t)

	out, err := execute(t, append(base, "skills", "delete", "local-shared/core/alpha", "--yes", "--json")...)
	if err != nil {
		t.Fatalf("skills delete --json: %v", err)
	}
	deleted := decodeJSON[skillDeleteOutput](t, out)
	wantPath := filepath.Join(resourceRoot, "skills", "local", "shared", "core", "alpha")
	if deleted.ID != "local-shared/core/alpha" || deleted.Group || deleted.Path != wantPath {
		t.Fatalf("delete output = %#v", deleted)
	}
	if _, err := os.Stat(wantPath); !os.IsNotExist(err) {
		t.Fatal("delete reported success without removing the skill")
	}
}

func TestMCPMutationJSONOutputs(t *testing.T) {
	_, projectRoot, base := mutationFixture(t)

	out, err := execute(t, append(base, "mcp", "add", "grafana", "--url", "https://mcp.example.com", "--json")...)
	if err != nil {
		t.Fatalf("mcp add --json: %v", err)
	}
	added := decodeJSON[mcpAddOutput](t, out)
	if !reflect.DeepEqual(added.Added, []string{"grafana"}) {
		t.Fatalf("add output = %#v", added)
	}

	wrapper := `{"mcpServers":{"context7":{"command":"npx"},"linear":{"url":"https://l"}}}`
	out, err = execute(t, append(base, "mcp", "import", wrapper, "--json")...)
	if err != nil {
		t.Fatalf("mcp import --json: %v", err)
	}
	imported := decodeJSON[mcpAddOutput](t, out)
	if len(imported.Added) != 2 {
		t.Fatalf("import output = %#v", imported)
	}

	out, err = execute(t, append(base, "mcp", "enable", "grafana", "--client", "claude", "--json")...)
	if err != nil {
		t.Fatalf("mcp enable --json: %v", err)
	}
	toggled := decodeJSON[resourceToggleOutput](t, out)
	want := resourceToggleOutput{Action: "enable", ID: "grafana", Clients: []string{"claude"}}
	if !reflect.DeepEqual(toggled, want) {
		t.Fatalf("enable output = %#v, want %#v", toggled, want)
	}
	if _, err := os.Stat(filepath.Join(projectRoot, ".mcp.json")); err != nil {
		t.Fatalf("enable reported success without writing the client config: %v", err)
	}

	out, err = execute(t, append(base, "mcp", "remove", "grafana", "--json")...)
	if err != nil {
		t.Fatalf("mcp remove --json: %v", err)
	}
	removed := decodeJSON[mcpRemoveOutput](t, out)
	if removed.Removed != "grafana" {
		t.Fatalf("remove output = %#v", removed)
	}
}

func TestPromptToggleJSONReportsGroupAndClient(t *testing.T) {
	resourceRoot := t.TempDir()
	userHome := t.TempDir()
	t.Setenv("HOME", userHome)
	if err := os.MkdirAll(filepath.Join(resourceRoot, "skills", "local", "shared"), 0o755); err != nil {
		t.Fatal(err)
	}
	promptFile := filepath.Join(resourceRoot, "system-prompts", "claude-prompt", "CLAUDE.md")
	if err := os.MkdirAll(filepath.Dir(promptFile), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(promptFile, []byte("# Prompt\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	out, err := execute(t, "--resources", resourceRoot, "--project", t.TempDir(), "prompt", "enable", "claude-prompt", "--json")
	if err != nil {
		t.Fatalf("prompt enable --json: %v", err)
	}
	toggled := decodeJSON[resourceToggleOutput](t, out)
	want := resourceToggleOutput{Action: "enable", ID: "claude-prompt", Clients: []string{"claude"}}
	if !reflect.DeepEqual(toggled, want) {
		t.Fatalf("prompt enable output = %#v, want %#v", toggled, want)
	}
	if _, err := os.Readlink(filepath.Join(userHome, ".claude", "CLAUDE.md")); err != nil {
		t.Fatalf("prompt enable reported success without projecting: %v", err)
	}
}

func TestUserResourceToggleJSONReportsKindAndClients(t *testing.T) {
	resourceRoot, projectRoot, base := mutationFixture(t)
	commandFile := filepath.Join(resourceRoot, "commands", "shared", "build.md")
	if err := os.MkdirAll(filepath.Dir(commandFile), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(commandFile, []byte("# build\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	out, err := execute(t, append(base, "commands", "enable", "shared/build.md", "--client", "claude", "--json")...)
	if err != nil {
		t.Fatalf("commands enable --json: %v", err)
	}
	toggled := decodeJSON[userResourceToggleOutput](t, out)
	want := userResourceToggleOutput{Action: "enable", Kind: "command", ID: "shared/build.md", Clients: []string{"claude"}}
	if !reflect.DeepEqual(toggled, want) {
		t.Fatalf("commands enable output = %#v, want %#v", toggled, want)
	}
	if _, err := os.Lstat(filepath.Join(projectRoot, ".claude", "commands", "build.md")); err != nil {
		t.Fatalf("commands enable reported success without projecting: %v", err)
	}
}
