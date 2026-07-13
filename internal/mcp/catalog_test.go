package mcp

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadCatalogAcceptsLegacyOneMCPMetadataWithoutOwningIt(t *testing.T) {
	path := filepath.Join(t.TempDir(), "mcp.json")
	data := `{
  "mcpServers": {
    "context7": {
      "command": "npx",
      "args": ["-y", "@upstash/context7-mcp@latest"],
      "disabled": true,
      "tags": ["all", "remote-search"]
    }
  }
}`
	if err := os.WriteFile(path, []byte(data), 0o644); err != nil {
		t.Fatal(err)
	}

	loaded, err := LoadCatalog(path)
	if err != nil {
		t.Fatal(err)
	}
	server, ok := loaded.Server("context7")
	if !ok {
		t.Fatal("context7 was not loaded")
	}
	if server.Transport != TransportStdio || server.Command != "npx" {
		t.Fatalf("unexpected server: %#v", server)
	}
}

func TestLoadCatalogRejectsAmbiguousTransport(t *testing.T) {
	path := filepath.Join(t.TempDir(), "mcp.json")
	data := `{"mcpServers":{"broken":{"command":"npx","url":"https://example.com/mcp"}}}`
	if err := os.WriteFile(path, []byte(data), 0o644); err != nil {
		t.Fatal(err)
	}

	if _, err := LoadCatalog(path); err == nil {
		t.Fatal("ambiguous server unexpectedly loaded")
	}
}
