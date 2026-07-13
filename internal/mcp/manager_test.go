package mcp

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/est7/skills-switch-tui/internal/client"
)

func TestClaudeEnableAndDisablePreserveCommentsAndUnknownServers(t *testing.T) {
	project := t.TempDir()
	path := filepath.Join(project, ".mcp.json")
	original := `{
  // Project-owned server. Keep this comment.
  "mcpServers": {
    "project-owned": {
      "command": "project-command"
    }
  },
  "unrelated": true
}`
	writeTestFile(t, path, original)
	manager := testManager(project, map[string]Server{
		"context7": {Name: "context7", Transport: TransportStdio, Command: "npx", Args: []string{"-y", "@upstash/context7-mcp@latest"}},
	})

	if err := manager.Apply([]Operation{{Server: "context7", Client: client.Claude, Enabled: true}}); err != nil {
		t.Fatal(err)
	}
	enabled := readTestFile(t, path)
	for _, fragment := range []string{"Keep this comment.", `"project-owned"`, `"unrelated": true`, `"context7"`} {
		if !strings.Contains(enabled, fragment) {
			t.Fatalf("enabled config lost %q:\n%s", fragment, enabled)
		}
	}
	state, err := manager.State("context7", client.Claude)
	if err != nil || state != StateEnabled {
		t.Fatalf("state = %q, %v", state, err)
	}

	if err := manager.Apply([]Operation{{Server: "context7", Client: client.Claude, Enabled: false}}); err != nil {
		t.Fatal(err)
	}
	disabled := readTestFile(t, path)
	for _, fragment := range []string{"Keep this comment.", `"project-owned"`, `"unrelated": true`} {
		if !strings.Contains(disabled, fragment) {
			t.Fatalf("disabled config lost %q:\n%s", fragment, disabled)
		}
	}
	if strings.Contains(disabled, `"context7"`) {
		t.Fatalf("disabled config still contains context7:\n%s", disabled)
	}
}

func TestGeminiHTTPUsesHTTPURLAndPreservesJSONC(t *testing.T) {
	project := t.TempDir()
	path := filepath.Join(project, ".gemini", "settings.json")
	writeTestFile(t, path, "{\n  // Keep the theme.\n  \"theme\": \"Dracula\"\n}\n")
	manager := testManager(project, map[string]Server{
		"deepwiki": {Name: "deepwiki", Transport: TransportHTTP, URL: "https://mcp.deepwiki.com/mcp"},
	})

	if err := manager.Apply([]Operation{{Server: "deepwiki", Client: client.Gemini, Enabled: true}}); err != nil {
		t.Fatal(err)
	}
	got := readTestFile(t, path)
	for _, fragment := range []string{"Keep the theme.", `"theme": "Dracula"`, `"httpUrl": "https://mcp.deepwiki.com/mcp"`} {
		if !strings.Contains(got, fragment) {
			t.Fatalf("Gemini config lost %q:\n%s", fragment, got)
		}
	}
	if strings.Contains(got, `"url":`) {
		t.Fatalf("Gemini streamable HTTP must use httpUrl:\n%s", got)
	}
}

func TestCodexEnableAndDisablePreserveTOMLCommentsAndSiblings(t *testing.T) {
	project := t.TempDir()
	path := filepath.Join(project, ".codex", "config.toml")
	original := `# Keep this project policy.
model = "gpt-5"

[mcp_servers.project-owned]
command = "project-command" # Keep this inline comment.

[features]
web_search = true
`
	writeTestFile(t, path, original)
	manager := testManager(project, map[string]Server{
		"context7": {Name: "context7", Transport: TransportStdio, Command: "npx", Args: []string{"-y", "@upstash/context7-mcp@latest"}, Env: map[string]string{"TOKEN": "static"}},
	})

	if err := manager.Apply([]Operation{{Server: "context7", Client: client.Codex, Enabled: true}}); err != nil {
		t.Fatal(err)
	}
	enabled := readTestFile(t, path)
	for _, fragment := range []string{"Keep this project policy.", "Keep this inline comment.", "[mcp_servers.project-owned]", "[mcp_servers.context7]", "[features]"} {
		if !strings.Contains(enabled, fragment) {
			t.Fatalf("enabled TOML lost %q:\n%s", fragment, enabled)
		}
	}

	if err := manager.Apply([]Operation{{Server: "context7", Client: client.Codex, Enabled: false}}); err != nil {
		t.Fatal(err)
	}
	disabled := readTestFile(t, path)
	for _, fragment := range []string{"Keep this project policy.", "Keep this inline comment.", "[mcp_servers.project-owned]", "[features]"} {
		if !strings.Contains(disabled, fragment) {
			t.Fatalf("disabled TOML lost %q:\n%s", fragment, disabled)
		}
	}
	if strings.Contains(disabled, "mcp_servers.context7") {
		t.Fatalf("disabled TOML still contains context7:\n%s", disabled)
	}
}

func TestSameNameDifferentDefinitionConflictsWithoutMutation(t *testing.T) {
	project := t.TempDir()
	path := filepath.Join(project, ".mcp.json")
	original := `{"mcpServers":{"context7":{"command":"custom-wrapper"}}}`
	writeTestFile(t, path, original)
	manager := testManager(project, map[string]Server{
		"context7": {Name: "context7", Transport: TransportStdio, Command: "npx"},
	})

	state, err := manager.State("context7", client.Claude)
	if err != nil || state != StateConflict {
		t.Fatalf("state = %q, %v", state, err)
	}
	for _, enabled := range []bool{true, false} {
		err := manager.Apply([]Operation{{Server: "context7", Client: client.Claude, Enabled: enabled}})
		var conflict *ConflictError
		if !errors.As(err, &conflict) {
			t.Fatalf("enabled=%v error = %v, want ConflictError", enabled, err)
		}
		if got := readTestFile(t, path); got != original {
			t.Fatalf("conflict mutated config:\n%s", got)
		}
	}
}

func TestGroupPreflightIsAtomicAcrossClients(t *testing.T) {
	project := t.TempDir()
	claudePath := filepath.Join(project, ".mcp.json")
	writeTestFile(t, claudePath, `{"mcpServers":{"context7":{"command":"custom-wrapper"}}}`)
	manager := testManager(project, map[string]Server{
		"context7": {Name: "context7", Transport: TransportStdio, Command: "npx"},
	})

	err := manager.Apply([]Operation{
		{Server: "context7", Client: client.Codex, Enabled: true},
		{Server: "context7", Client: client.Claude, Enabled: true},
	})
	if err == nil {
		t.Fatal("group conflict unexpectedly succeeded")
	}
	if _, err := os.Lstat(filepath.Join(project, ".codex", "config.toml")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("Codex config was partially created: %v", err)
	}
}

func TestConfigSymlinkIsWrittenThrough(t *testing.T) {
	project := t.TempDir()
	realDir := t.TempDir()
	realPath := filepath.Join(realDir, "claude-mcp.json")
	writeTestFile(t, realPath, `{"mcpServers":{}}`)
	linkPath := filepath.Join(project, ".mcp.json")
	if err := os.Symlink(realPath, linkPath); err != nil {
		t.Fatal(err)
	}
	manager := testManager(project, map[string]Server{
		"context7": {Name: "context7", Transport: TransportStdio, Command: "npx"},
	})

	if err := manager.Apply([]Operation{{Server: "context7", Client: client.Claude, Enabled: true}}); err != nil {
		t.Fatal(err)
	}
	if info, err := os.Lstat(linkPath); err != nil || info.Mode()&os.ModeSymlink == 0 {
		t.Fatalf("config symlink was replaced: %v, %v", info, err)
	}
	if got := readTestFile(t, realPath); !strings.Contains(got, `"context7"`) {
		t.Fatalf("symlink target was not updated:\n%s", got)
	}
}

func TestInvalidProjectConfigFailsClosed(t *testing.T) {
	project := t.TempDir()
	path := filepath.Join(project, ".codex", "config.toml")
	original := "[mcp_servers.broken\ncommand = 42\n"
	writeTestFile(t, path, original)
	manager := testManager(project, map[string]Server{
		"context7": {Name: "context7", Transport: TransportStdio, Command: "npx"},
	})

	if err := manager.Apply([]Operation{{Server: "context7", Client: client.Codex, Enabled: true}}); err == nil {
		t.Fatal("invalid TOML unexpectedly overwritten")
	}
	if got := readTestFile(t, path); got != original {
		t.Fatalf("invalid TOML was mutated:\n%s", got)
	}
}

func TestCodexHTTPMapsSecretHeadersToEnvironmentReferences(t *testing.T) {
	project := t.TempDir()
	manager := testManager(project, map[string]Server{
		"context7": {
			Name:      "context7",
			Transport: TransportHTTP,
			URL:       "https://mcp.context7.com/mcp",
			Headers:   map[string]string{"CONTEXT7_API_KEY": "${CONTEXT7_API_KEY}"},
		},
	})

	if err := manager.Apply([]Operation{{Server: "context7", Client: client.Codex, Enabled: true}}); err != nil {
		t.Fatal(err)
	}
	got := readTestFile(t, filepath.Join(project, ".codex", "config.toml"))
	for _, fragment := range []string{
		`url = "https://mcp.context7.com/mcp"`,
		`[mcp_servers.context7.env_http_headers]`,
		`CONTEXT7_API_KEY = "CONTEXT7_API_KEY"`,
	} {
		if !strings.Contains(got, fragment) {
			t.Fatalf("Codex HTTP config lost %q:\n%s", fragment, got)
		}
	}
}

func TestCodexRejectsArgumentInterpolationWithoutWriting(t *testing.T) {
	project := t.TempDir()
	manager := testManager(project, map[string]Server{
		"legacy": {Name: "legacy", Transport: TransportStdio, Command: "npx", Args: []string{"--token", "${TOKEN}"}},
	})

	if err := manager.Apply([]Operation{{Server: "legacy", Client: client.Codex, Enabled: true}}); err == nil {
		t.Fatal("Codex argument interpolation unexpectedly succeeded")
	}
	if _, err := os.Lstat(filepath.Join(project, ".codex", "config.toml")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("incompatible server created a config: %v", err)
	}
}

func TestConcurrentChangeBeforeWriteAbortsAndRollsBackEarlierClient(t *testing.T) {
	project := t.TempDir()
	claudePath := filepath.Join(project, ".mcp.json")
	codexPath := filepath.Join(project, ".codex", "config.toml")
	writeTestFile(t, codexPath, "# original\n")
	manager := testManager(project, map[string]Server{
		"context7": {Name: "context7", Transport: TransportStdio, Command: "npx"},
	})
	manager.beforeWrite = func(path string) {
		if path == codexPath {
			writeTestFile(t, codexPath, "# changed concurrently\n")
		}
	}

	err := manager.Apply([]Operation{
		{Server: "context7", Client: client.Claude, Enabled: true},
		{Server: "context7", Client: client.Codex, Enabled: true},
	})
	if err == nil || !strings.Contains(err.Error(), "changed during operation") {
		t.Fatalf("error = %v, want concurrent-change failure", err)
	}
	if _, err := os.Lstat(claudePath); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("earlier Claude write was not rolled back: %v", err)
	}
	if got := readTestFile(t, codexPath); got != "# changed concurrently\n" {
		t.Fatalf("concurrent writer was overwritten: %q", got)
	}
}

func TestWriteFailureRollsBackEarlierClient(t *testing.T) {
	project := t.TempDir()
	claudePath := filepath.Join(project, ".mcp.json")
	manager := testManager(project, map[string]Server{
		"context7": {Name: "context7", Transport: TransportStdio, Command: "npx"},
	})
	manager.writeFile = func(path string, data []byte, mode os.FileMode) error {
		if strings.Contains(path, filepath.Join(".codex", "config.toml")) {
			return errors.New("injected write failure")
		}
		return atomicWrite(path, data, mode)
	}

	err := manager.Apply([]Operation{
		{Server: "context7", Client: client.Claude, Enabled: true},
		{Server: "context7", Client: client.Codex, Enabled: true},
	})
	if err == nil || !strings.Contains(err.Error(), "injected write failure") {
		t.Fatalf("error = %v, want injected write failure", err)
	}
	if _, err := os.Lstat(claudePath); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("earlier Claude write was not rolled back: %v", err)
	}
}

func testManager(project string, servers map[string]Server) Manager {
	return NewManager(project, Catalog{Servers: servers}, client.DefaultRegistry())
}

func writeTestFile(t *testing.T, path, data string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(data), 0o644); err != nil {
		t.Fatal(err)
	}
}

func readTestFile(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}
