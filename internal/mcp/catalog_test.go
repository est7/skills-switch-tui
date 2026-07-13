package mcp

import (
	"os"
	"path/filepath"
	"strings"
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

func TestAddAndRemoveServerPreserveOtherEntriesAndUnknownFields(t *testing.T) {
	path := filepath.Join(t.TempDir(), "mcp.json")
	data := `{
  "version": 1,
  "mcpServers": {
    "context7": {
      "command": "npx",
      "args": ["-y", "@upstash/context7-mcp@latest"],
      "tags": ["all"]
    }
  }
}`
	if err := os.WriteFile(path, []byte(data), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := AddServer(path, Server{Name: "grafana", Transport: TransportHTTP, URL: "https://mcp.example.com"}); err != nil {
		t.Fatalf("add server: %v", err)
	}
	loaded, err := LoadCatalog(path)
	if err != nil {
		t.Fatal(err)
	}
	grafana, ok := loaded.Server("grafana")
	if !ok || grafana.Transport != TransportHTTP || grafana.URL != "https://mcp.example.com" {
		t.Fatalf("added server not persisted: %#v", grafana)
	}
	if _, ok := loaded.Server("context7"); !ok {
		t.Fatal("existing server was dropped by add")
	}

	// Adding a duplicate name is rejected.
	if err := AddServer(path, Server{Name: "grafana", Transport: TransportHTTP, URL: "https://other"}); err == nil {
		t.Fatal("duplicate add must be rejected")
	}

	if err := RemoveServer(path, "grafana"); err != nil {
		t.Fatalf("remove server: %v", err)
	}
	if err := RemoveServer(path, "grafana"); err == nil {
		t.Fatal("removing a missing server must be rejected")
	}

	// context7 and its unknown "tags" field survive both mutations.
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := string(raw); !strings.Contains(got, "\"tags\"") || !strings.Contains(got, "context7") {
		t.Fatalf("unknown fields not preserved: %s", got)
	}
	loaded, err = LoadCatalog(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := loaded.Server("grafana"); ok {
		t.Fatal("removed server still present")
	}
	if _, ok := loaded.Server("context7"); !ok {
		t.Fatal("unrelated server removed")
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
