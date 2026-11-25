package mcp

import (
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	"github.com/est7/skills-switch-tui/internal/client"
)

func TestAgyEnableDisableFormatAndRuntimeDisabledTolerance(t *testing.T) {
	project := t.TempDir()
	path := filepath.Join(project, ".agents", "mcp_config.json")
	original := `{
  // Keep user comment.
  "mcpServers": {
    "existing": {
      "command": "keep-me"
    }
  }
}`
	writeTestFile(t, path, original)
	manager := testManager(project, map[string]Server{
		"context7": {Name: "context7", Transport: TransportStdio, Command: "npx", Args: []string{"-y", "@upstash/context7-mcp@latest"}, Env: map[string]string{"KEY": "${API_KEY}"}},
		"remote":   {Name: "remote", Transport: TransportHTTP, URL: "https://mcp.example.com/mcp", Headers: map[string]string{"Authorization": "Bearer ${TOKEN}"}},
	})

	if err := manager.Apply([]Operation{
		{Server: "context7", Client: client.Agy, Enabled: true},
		{Server: "remote", Client: client.Agy, Enabled: true},
	}); err != nil {
		t.Fatalf("enable agy MCP servers: %v", err)
	}
	rendered := readTestFile(t, path)
	if !strings.Contains(rendered, "Keep user comment.") || !strings.Contains(rendered, `"existing"`) {
		t.Fatalf("agy MCP write lost comment or sibling:\n%s", rendered)
	}
	var parsed struct {
		MCPServers map[string]map[string]any `json:"mcpServers"`
	}
	// Strip the single comment line before decoding JSON structure.
	clean := strings.ReplaceAll(rendered, "  // Keep user comment.\n", "")
	if err := json.Unmarshal([]byte(clean), &parsed); err != nil {
		t.Fatalf("parse agy mcp_config.json: %v\n%s", err, rendered)
	}
	stdioEntry := parsed.MCPServers["context7"]
	if stdioEntry["command"] != "npx" || stdioEntry["type"] != nil {
		t.Fatalf("agy stdio entry = %#v, want command without type", stdioEntry)
	}
	httpEntry := parsed.MCPServers["remote"]
	if httpEntry["serverUrl"] != "https://mcp.example.com/mcp" || httpEntry["url"] != nil || httpEntry["type"] != nil {
		t.Fatalf("agy http entry = %#v, want serverUrl without type/url", httpEntry)
	}

	// Simulate `agy mcp disable` adding a runtime "disabled": true flag and using legacy "url".
	writeTestFile(t, path, `{
  "mcpServers": {
    "existing": {"command": "keep-me"},
    "context7": {"command": "npx", "args": ["-y", "@upstash/context7-mcp@latest"], "env": {"KEY": "${API_KEY}"}, "disabled": true},
    "remote": {"url": "https://mcp.example.com/mcp", "headers": {"Authorization": "Bearer ${TOKEN}"}, "disabled": true}
  }
}`)
	for _, name := range []string{"context7", "remote"} {
		state, err := manager.State(name, client.Agy)
		if err != nil || state != StateEnabled {
			t.Fatalf("State(%s, agy) with runtime disabled flag = %q, %v; want enabled", name, state, err)
		}
	}

	if err := manager.Apply([]Operation{
		{Server: "context7", Client: client.Agy, Enabled: false},
		{Server: "remote", Client: client.Agy, Enabled: false},
	}); err != nil {
		t.Fatalf("disable agy MCP servers: %v", err)
	}
	after := readTestFile(t, path)
	if strings.Contains(after, `"context7"`) || strings.Contains(after, `"remote"`) || !strings.Contains(after, `"existing"`) {
		t.Fatalf("disable agy MCP left unexpected state:\n%s", after)
	}
}

func TestParseServersAcceptsAgyServerURL(t *testing.T) {
	servers, err := ParseServers([]byte(`{"mcpServers":{"agy-http":{"serverUrl":"https://api.example.com/mcp","headers":{"Authorization":"Bearer ${API_TOKEN}"}}}}`))
	if err != nil {
		t.Fatalf("ParseServers(serverUrl): %v", err)
	}
	if len(servers) != 1 || servers[0].Transport != TransportHTTP || servers[0].URL != "https://api.example.com/mcp" {
		t.Fatalf("parsed = %#v", servers)
	}
	if _, err := ParseServers([]byte(`{"url":"https://a.example","serverUrl":"https://b.example"}`)); err == nil {
		t.Fatal("ParseServers accepted conflicting url and serverUrl")
	}
}

func TestAgyNonBooleanDisabledReportsConflict(t *testing.T) {
	project := t.TempDir()
	path := filepath.Join(project, ".agents", "mcp_config.json")
	manager := testManager(project, map[string]Server{
		"context7": {Name: "context7", Transport: TransportStdio, Command: "npx"},
	})
	writeTestFile(t, path, `{"mcpServers":{"context7":{"command":"npx","disabled":"yes"}}}`)
	state, err := manager.State("context7", client.Agy)
	if err != nil || state != StateConflict {
		t.Fatalf("State with non-boolean disabled = %q, %v; want conflict", state, err)
	}
	writeTestFile(t, path, `{"mcpServers":{"context7":{"command":"npx","disabled":false}}}`)
	state, err = manager.State("context7", client.Agy)
	if err != nil || state != StateEnabled {
		t.Fatalf("State with boolean disabled=false = %q, %v; want enabled", state, err)
	}
}
