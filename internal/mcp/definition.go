package mcp

import (
	"fmt"
	"regexp"
	"sort"
	"strings"

	"github.com/est7/skills-switch-tui/internal/client"
)

var environmentReference = regexp.MustCompile(`^\$\{([A-Za-z_][A-Za-z0-9_]*)\}$`)
var bearerReference = regexp.MustCompile(`^Bearer \$\{([A-Za-z_][A-Za-z0-9_]*)\}$`)

func projectDefinition(format client.MCPFormat, server Server) (map[string]any, error) {
	if err := validateServer(server); err != nil {
		return nil, err
	}
	switch format {
	case client.MCPClaudeJSON:
		return claudeDefinition(server), nil
	case client.MCPGeminiJSON:
		return geminiDefinition(server), nil
	case client.MCPCodexTOML:
		return codexDefinition(server)
	default:
		return nil, fmt.Errorf("unsupported MCP format %q", format)
	}
}

func claudeDefinition(server Server) map[string]any {
	if server.Transport == TransportHTTP {
		result := map[string]any{"type": "http", "url": server.URL}
		putStringMap(result, "headers", server.Headers)
		return result
	}
	result := map[string]any{"type": "stdio", "command": server.Command}
	putStrings(result, "args", server.Args)
	putStringMap(result, "env", server.Env)
	putString(result, "cwd", server.CWD)
	return result
}

func geminiDefinition(server Server) map[string]any {
	if server.Transport == TransportHTTP {
		result := map[string]any{"httpUrl": server.URL}
		putStringMap(result, "headers", server.Headers)
		return result
	}
	result := map[string]any{"command": server.Command}
	putStrings(result, "args", server.Args)
	putStringMap(result, "env", server.Env)
	putString(result, "cwd", server.CWD)
	return result
}

func codexDefinition(server Server) (map[string]any, error) {
	if server.Transport == TransportHTTP {
		result := map[string]any{"url": server.URL}
		staticHeaders := make(map[string]string)
		environmentHeaders := make(map[string]string)
		for _, header := range sortedKeys(server.Headers) {
			value := server.Headers[header]
			if match := bearerReference.FindStringSubmatch(value); len(match) == 2 && strings.EqualFold(header, "authorization") {
				result["bearer_token_env_var"] = match[1]
				continue
			}
			if match := environmentReference.FindStringSubmatch(value); len(match) == 2 {
				environmentHeaders[header] = match[1]
				continue
			}
			if strings.Contains(value, "${") {
				return nil, fmt.Errorf("Codex cannot losslessly represent interpolated header %q", header)
			}
			staticHeaders[header] = value
		}
		putStringMap(result, "http_headers", staticHeaders)
		putStringMap(result, "env_http_headers", environmentHeaders)
		return result, nil
	}

	if strings.Contains(server.Command, "${") || strings.Contains(server.CWD, "${") {
		return nil, fmt.Errorf("Codex cannot losslessly expand variables in command or cwd")
	}
	for _, argument := range server.Args {
		if strings.Contains(argument, "${") {
			return nil, fmt.Errorf("Codex cannot losslessly expand variables in arguments")
		}
	}
	result := map[string]any{"command": server.Command}
	putStrings(result, "args", server.Args)
	putString(result, "cwd", server.CWD)
	staticEnv := make(map[string]string)
	forwarded := make([]string, 0)
	for _, name := range sortedKeys(server.Env) {
		value := server.Env[name]
		if match := environmentReference.FindStringSubmatch(value); len(match) == 2 {
			if match[1] != name {
				return nil, fmt.Errorf("Codex cannot forward environment variable %s as %s", match[1], name)
			}
			forwarded = append(forwarded, name)
			continue
		}
		if strings.Contains(value, "${") {
			return nil, fmt.Errorf("Codex cannot losslessly represent interpolated environment variable %q", name)
		}
		staticEnv[name] = value
	}
	putStringMap(result, "env", staticEnv)
	putStrings(result, "env_vars", forwarded)
	return result, nil
}

func putString(target map[string]any, key, value string) {
	if value != "" {
		target[key] = value
	}
}

func putStrings(target map[string]any, key string, values []string) {
	if len(values) == 0 {
		return
	}
	items := make([]any, len(values))
	for index, value := range values {
		items[index] = value
	}
	target[key] = items
}

func putStringMap(target map[string]any, key string, values map[string]string) {
	if len(values) == 0 {
		return
	}
	items := make(map[string]any, len(values))
	for name, value := range values {
		items[name] = value
	}
	target[key] = items
}

func sortedKeys[V any](values map[string]V) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}
