package mcp

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/est7/skills-switch-tui/internal/client"
)

func TestRemoveWithProjectionsRestoresEnabledClientsWhenCatalogRemovalFails(t *testing.T) {
	projectRoot := t.TempDir()
	server := Server{Name: "context7", Transport: TransportStdio, Command: "npx"}
	catalog := Catalog{Servers: map[string]Server{server.Name: server}}
	manager := NewManager(projectRoot, catalog, client.DefaultRegistry())
	if err := manager.Apply([]Operation{{Server: server.Name, Client: client.Codex, Enabled: true}}); err != nil {
		t.Fatal(err)
	}

	// A directory cannot be parsed/replaced as an MCP catalog file, forcing the
	// shared-provider deletion to fail after projection retirement.
	invalidCatalogPath := filepath.Join(t.TempDir(), "catalog-directory")
	if err := os.Mkdir(invalidCatalogPath, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := RemoveWithProjections(manager, invalidCatalogPath, server.Name, []client.ID{client.Codex}); err == nil {
		t.Fatal("RemoveWithProjections unexpectedly succeeded")
	}
	state, err := manager.State(server.Name, client.Codex)
	if err != nil {
		t.Fatal(err)
	}
	if state != StateEnabled {
		t.Fatalf("state after rollback = %s, want enabled", state)
	}
}
