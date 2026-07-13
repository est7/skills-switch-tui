package mcp

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

type Transport string

const (
	TransportStdio Transport = "stdio"
	TransportHTTP  Transport = "http"
)

type Server struct {
	Name      string
	Transport Transport
	Command   string
	Args      []string
	Env       map[string]string
	CWD       string
	URL       string
	Headers   map[string]string
}

type Catalog struct {
	Path    string
	Servers map[string]Server
}

func (c Catalog) Server(name string) (Server, bool) {
	server, ok := c.Servers[name]
	return server, ok
}

func (c Catalog) Names() []string {
	names := make([]string, 0, len(c.Servers))
	for name := range c.Servers {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

type catalogFile struct {
	Version    int                  `json:"version,omitempty"`
	MCPServers map[string]rawServer `json:"mcpServers"`
}

type rawServer struct {
	Type    string            `json:"type,omitempty"`
	Command string            `json:"command,omitempty"`
	Args    []string          `json:"args,omitempty"`
	Env     map[string]string `json:"env,omitempty"`
	CWD     string            `json:"cwd,omitempty"`
	URL     string            `json:"url,omitempty"`
	Headers map[string]string `json:"headers,omitempty"`
}

func LoadCatalog(path string) (Catalog, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Catalog{}, fmt.Errorf("read MCP catalog %s: %w", path, err)
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	var file catalogFile
	if err := decoder.Decode(&file); err != nil {
		return Catalog{}, fmt.Errorf("parse MCP catalog %s: %w", path, err)
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		if err == nil {
			err = fmt.Errorf("multiple JSON values")
		}
		return Catalog{}, fmt.Errorf("parse MCP catalog %s: %w", path, err)
	}
	if file.MCPServers == nil {
		return Catalog{}, fmt.Errorf("MCP catalog %s is missing mcpServers", path)
	}
	loaded := Catalog{Path: path, Servers: make(map[string]Server, len(file.MCPServers))}
	for name, raw := range file.MCPServers {
		server, err := normalizeServer(name, raw)
		if err != nil {
			return Catalog{}, fmt.Errorf("MCP catalog server %q: %w", name, err)
		}
		loaded.Servers[name] = server
	}
	return loaded, nil
}

// AddServer registers a new server in the catalog file at path, preserving all
// other entries and any unknown fields. It fails if the server already exists.
func AddServer(path string, server Server) error {
	if err := validateServer(server); err != nil {
		return err
	}
	return mutateCatalogServers(path, func(servers map[string]any) error {
		if _, exists := servers[server.Name]; exists {
			return fmt.Errorf("MCP server already exists: %s", server.Name)
		}
		entry, err := serverEntry(server)
		if err != nil {
			return err
		}
		servers[server.Name] = entry
		return nil
	})
}

// RemoveServer deletes a server from the catalog file at path, preserving all
// other entries. It fails if the server does not exist.
func RemoveServer(path, name string) error {
	return mutateCatalogServers(path, func(servers map[string]any) error {
		if _, exists := servers[name]; !exists {
			return fmt.Errorf("MCP server does not exist: %s", name)
		}
		delete(servers, name)
		return nil
	})
}

func serverEntry(server Server) (map[string]any, error) {
	raw := rawServer{
		Type:    string(server.Transport),
		Command: server.Command,
		Args:    server.Args,
		Env:     server.Env,
		CWD:     server.CWD,
		URL:     server.URL,
		Headers: server.Headers,
	}
	data, err := json.Marshal(raw)
	if err != nil {
		return nil, fmt.Errorf("encode MCP server %q: %w", server.Name, err)
	}
	entry := map[string]any{}
	if err := json.Unmarshal(data, &entry); err != nil {
		return nil, fmt.Errorf("encode MCP server %q: %w", server.Name, err)
	}
	return entry, nil
}

func mutateCatalogServers(path string, mutate func(map[string]any) error) error {
	data, exists, mode, err := readConfig(path)
	if err != nil {
		return fmt.Errorf("read MCP catalog %s: %w", path, err)
	}
	document := map[string]any{}
	if exists {
		if err := json.Unmarshal(data, &document); err != nil {
			return fmt.Errorf("parse MCP catalog %s: %w", path, err)
		}
	} else {
		document["version"] = 1
	}
	servers := map[string]any{}
	if existing, ok := document["mcpServers"]; ok && existing != nil {
		typed, ok := existing.(map[string]any)
		if !ok {
			return fmt.Errorf("MCP catalog %s mcpServers is not an object", path)
		}
		servers = typed
	}
	if err := mutate(servers); err != nil {
		return err
	}
	document["mcpServers"] = servers
	encoded, err := json.MarshalIndent(document, "", "  ")
	if err != nil {
		return fmt.Errorf("encode MCP catalog %s: %w", path, err)
	}
	encoded = append(encoded, '\n')
	if mode == 0 {
		mode = 0o644
	}
	if _, err := ensureParentDirectory(filepath.Dir(path)); err != nil {
		return fmt.Errorf("prepare MCP catalog directory: %w", err)
	}
	return atomicWrite(path, encoded, mode)
}

func normalizeServer(name string, raw rawServer) (Server, error) {
	if strings.TrimSpace(name) == "" {
		return Server{}, fmt.Errorf("name must not be empty")
	}
	transport := Transport(raw.Type)
	if transport == "" {
		switch {
		case raw.Command != "" && raw.URL == "":
			transport = TransportStdio
		case raw.URL != "" && raw.Command == "":
			transport = TransportHTTP
		default:
			return Server{}, fmt.Errorf("cannot infer one unambiguous transport")
		}
	}
	server := Server{
		Name:      name,
		Transport: transport,
		Command:   raw.Command,
		Args:      append([]string(nil), raw.Args...),
		Env:       cloneStrings(raw.Env),
		CWD:       raw.CWD,
		URL:       raw.URL,
		Headers:   cloneStrings(raw.Headers),
	}
	if err := validateServer(server); err != nil {
		return Server{}, err
	}
	return server, nil
}

func validateServer(server Server) error {
	if strings.TrimSpace(server.Name) == "" {
		return fmt.Errorf("name must not be empty")
	}
	switch server.Transport {
	case TransportStdio:
		if server.Command == "" {
			return fmt.Errorf("stdio transport requires command")
		}
		if server.URL != "" || len(server.Headers) != 0 {
			return fmt.Errorf("stdio transport cannot declare URL or headers")
		}
	case TransportHTTP:
		if server.URL == "" {
			return fmt.Errorf("HTTP transport requires URL")
		}
		if server.Command != "" || len(server.Args) != 0 || len(server.Env) != 0 || server.CWD != "" {
			return fmt.Errorf("HTTP transport cannot declare stdio fields")
		}
	default:
		return fmt.Errorf("unsupported transport %q", server.Transport)
	}
	return nil
}

func cloneStrings(input map[string]string) map[string]string {
	if len(input) == 0 {
		return nil
	}
	result := make(map[string]string, len(input))
	for key, value := range input {
		result[key] = value
	}
	return result
}
