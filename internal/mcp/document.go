package mcp

import (
	"errors"
	"fmt"
	"reflect"
	"strconv"

	"github.com/est7/skills-switch-tui/internal/client"
	"github.com/matthewmueller/jsonc"
	toml "github.com/neongreen/tomlsawyer"
)

type configDocument interface {
	server(name string) (map[string]any, bool, error)
	add(name string, definition map[string]any) error
	remove(name string) error
	bytes() ([]byte, error)
}

func parseDocument(format client.MCPFormat, data []byte) (configDocument, error) {
	switch format {
	case client.MCPClaudeJSON, client.MCPGeminiJSON:
		return parseJSONDocument(data)
	case client.MCPCodexTOML:
		return parseTOMLDocument(data)
	default:
		return nil, fmt.Errorf("unsupported MCP format %q", format)
	}
}

type jsonDocument struct {
	original []byte
	root     map[string]any
}

func parseJSONDocument(data []byte) (*jsonDocument, error) {
	if len(data) == 0 {
		data = []byte("{}\n")
	}
	var root map[string]any
	if err := jsonc.Unmarshal(data, &root); err != nil {
		return nil, fmt.Errorf("parse JSONC project config: %w", err)
	}
	if root == nil {
		return nil, fmt.Errorf("JSONC project config must be an object")
	}
	return &jsonDocument{original: append([]byte(nil), data...), root: root}, nil
}

func (d *jsonDocument) servers(create bool) (map[string]any, error) {
	raw, ok := d.root["mcpServers"]
	if !ok {
		if !create {
			return nil, nil
		}
		servers := make(map[string]any)
		d.root["mcpServers"] = servers
		return servers, nil
	}
	servers, ok := raw.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("mcpServers must be an object")
	}
	return servers, nil
}

func (d *jsonDocument) server(name string) (map[string]any, bool, error) {
	servers, err := d.servers(false)
	if err != nil || servers == nil {
		return nil, false, err
	}
	raw, ok := servers[name]
	if !ok {
		return nil, false, nil
	}
	definition, ok := raw.(map[string]any)
	if !ok {
		return nil, false, fmt.Errorf("MCP server %q must be an object", name)
	}
	return normalizeValue(definition).(map[string]any), true, nil
}

func (d *jsonDocument) add(name string, definition map[string]any) error {
	servers, err := d.servers(true)
	if err != nil {
		return err
	}
	servers[name] = definition
	return nil
}

func (d *jsonDocument) remove(name string) error {
	servers, err := d.servers(false)
	if err != nil || servers == nil {
		return err
	}
	delete(servers, name)
	return nil
}

func (d *jsonDocument) bytes() ([]byte, error) {
	return jsonc.Patch(d.original, d.root)
}

type tomlDocument struct {
	doc *toml.Document
}

func parseTOMLDocument(data []byte) (*tomlDocument, error) {
	doc, err := toml.Parse(data)
	if err != nil {
		return nil, fmt.Errorf("parse TOML project config: %w", err)
	}
	return &tomlDocument{doc: doc}, nil
}

func (d *tomlDocument) server(name string) (map[string]any, bool, error) {
	path := tomlPath("mcp_servers", name)
	value, exists, err := d.node(path)
	if errors.Is(err, toml.ErrNotValue) {
		err = nil
	}
	if err != nil || !exists {
		return nil, exists, err
	}
	definition, ok := value.(map[string]any)
	if !ok {
		return nil, false, fmt.Errorf("MCP server %q must be a table", name)
	}
	return normalizeValue(definition).(map[string]any), true, nil
}

func (d *tomlDocument) node(path string) (any, bool, error) {
	value, exists, err := d.doc.Get(path)
	if err == nil && exists {
		return normalizeValue(value), true, nil
	}
	if err != nil && !errors.Is(err, toml.ErrNotValue) {
		return nil, false, err
	}
	keys, keysErr := d.doc.Keys(path)
	if keysErr != nil {
		return nil, false, keysErr
	}
	if len(keys) == 0 {
		return nil, false, nil
	}
	result := make(map[string]any, len(keys))
	for _, key := range keys {
		child, childExists, childErr := d.node(path + "." + quoteTOMLKey(key))
		if childErr != nil {
			return nil, false, childErr
		}
		if childExists {
			result[key] = child
		}
	}
	return result, true, nil
}

func (d *tomlDocument) add(name string, definition map[string]any) error {
	return d.setNode(tomlPath("mcp_servers", name), definition)
}

func (d *tomlDocument) setNode(path string, value any) error {
	if object, ok := value.(map[string]any); ok {
		for _, key := range sortedKeys(object) {
			if err := d.setNode(path+"."+quoteTOMLKey(key), object[key]); err != nil {
				return err
			}
		}
		return nil
	}
	return d.doc.Set(path, value)
}

func (d *tomlDocument) remove(name string) error {
	path := tomlPath("mcp_servers", name)
	if err := d.removeNode(path); err != nil {
		return err
	}
	for {
		before := string(d.doc.Bytes())
		d.doc.Prune()
		if string(d.doc.Bytes()) == before {
			break
		}
	}
	return nil
}

func (d *tomlDocument) removeNode(path string) error {
	if _, exists, err := d.doc.Get(path); err == nil && exists {
		return d.doc.Delete(path)
	} else if err != nil && !errors.Is(err, toml.ErrNotValue) {
		return err
	}
	keys, err := d.doc.Keys(path)
	if err != nil {
		return err
	}
	for _, key := range keys {
		if err := d.removeNode(path + "." + quoteTOMLKey(key)); err != nil {
			return err
		}
	}
	return nil
}

func (d *tomlDocument) bytes() ([]byte, error) {
	return d.doc.Bytes(), nil
}

func tomlPath(parts ...string) string {
	result := ""
	for _, part := range parts {
		if result != "" {
			result += "."
		}
		result += quoteTOMLKey(part)
	}
	return result
}

func quoteTOMLKey(key string) string {
	return strconv.Quote(key)
}

func normalizeValue(value any) any {
	switch typed := value.(type) {
	case map[string]any:
		result := make(map[string]any, len(typed))
		for key, item := range typed {
			normalized := normalizeValue(item)
			if isEmptyCollection(normalized) {
				continue
			}
			result[key] = normalized
		}
		return result
	case map[string]string:
		result := make(map[string]any, len(typed))
		for key, item := range typed {
			result[key] = item
		}
		return result
	case []string:
		result := make([]any, len(typed))
		for index, item := range typed {
			result[index] = item
		}
		return result
	case []any:
		result := make([]any, len(typed))
		for index, item := range typed {
			result[index] = normalizeValue(item)
		}
		return result
	default:
		return value
	}
}

func isEmptyCollection(value any) bool {
	reflected := reflect.ValueOf(value)
	return reflected.IsValid() && (reflected.Kind() == reflect.Map || reflected.Kind() == reflect.Slice) && reflected.Len() == 0
}

func definitionsEqual(format client.MCPFormat, existing, expected map[string]any) bool {
	existing = normalizeValue(existing).(map[string]any)
	expected = normalizeValue(expected).(map[string]any)
	if format == client.MCPClaudeJSON {
		if _, ok := existing["type"]; !ok {
			if _, hasCommand := existing["command"]; hasCommand {
				existing["type"] = "stdio"
			}
		}
		if existing["type"] == "streamable-http" {
			existing["type"] = "http"
		}
	}
	return reflect.DeepEqual(existing, expected)
}
