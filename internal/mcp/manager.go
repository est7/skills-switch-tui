package mcp

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/est7/skills-switch-tui/internal/client"
)

type State string

const (
	StateDisabled     State = "disabled"
	StateEnabled      State = "enabled"
	StateConflict     State = "conflict"
	StateIncompatible State = "incompatible"
)

type Operation struct {
	Server  string
	Client  client.ID
	Enabled bool
}

type Conflict struct {
	Path   string
	Reason string
}

type ConflictError struct {
	Conflicts []Conflict
}

func (e *ConflictError) Error() string {
	parts := make([]string, 0, len(e.Conflicts))
	for _, conflict := range e.Conflicts {
		parts = append(parts, conflict.Path+": "+conflict.Reason)
	}
	return "MCP conflicts: " + strings.Join(parts, "; ")
}

type Manager struct {
	projectRoot string
	catalog     Catalog
	clients     client.Registry
	beforeWrite func(string)
	writeFile   func(string, []byte, os.FileMode) error
}

func NewManager(projectRoot string, catalog Catalog, clients client.Registry) Manager {
	return Manager{projectRoot: projectRoot, catalog: catalog, clients: clients, writeFile: atomicWrite}
}

func (m Manager) State(serverName string, clientID client.ID) (State, error) {
	server, ok := m.catalog.Server(serverName)
	if !ok {
		return "", fmt.Errorf("unknown MCP server %q", serverName)
	}
	path, format, err := m.clients.MCPProjectFile(m.projectRoot, clientID)
	if err != nil {
		return StateIncompatible, nil
	}
	expected, err := projectDefinition(format, server)
	if err != nil {
		return StateIncompatible, nil
	}
	resolved, err := resolveConfigPath(path)
	if err != nil {
		return "", err
	}
	data, exists, _, err := readConfig(resolved)
	if err != nil {
		return "", err
	}
	if !exists {
		return StateDisabled, nil
	}
	document, err := parseDocument(format, data)
	if err != nil {
		return "", fmt.Errorf("inspect %s: %w", path, err)
	}
	existing, exists, err := document.server(serverName)
	if err != nil {
		return "", fmt.Errorf("inspect %s server %s: %w", path, serverName, err)
	}
	if !exists {
		return StateDisabled, nil
	}
	if definitionsEqual(format, existing, expected) {
		return StateEnabled, nil
	}
	return StateConflict, nil
}

type filePlan struct {
	requestedPath string
	resolvedPath  string
	format        client.MCPFormat
	original      []byte
	originalMode  os.FileMode
	existed       bool
	document      configDocument
	result        []byte
	changed       bool
	createdDirs   []string
}

func (m Manager) Apply(operations []Operation) error {
	if len(operations) == 0 {
		return nil
	}
	unique := make(map[string]Operation, len(operations))
	conflicts := make([]Conflict, 0)
	for _, operation := range operations {
		key := string(operation.Client) + "\x00" + operation.Server
		if previous, exists := unique[key]; exists && previous.Enabled != operation.Enabled {
			conflicts = append(conflicts, Conflict{Path: operation.Server, Reason: "operation requests both enabled and disabled"})
			continue
		}
		unique[key] = operation
	}
	if len(conflicts) > 0 {
		return &ConflictError{Conflicts: conflicts}
	}

	ordered := make([]Operation, 0, len(unique))
	for _, operation := range unique {
		ordered = append(ordered, operation)
	}
	sort.Slice(ordered, func(i, j int) bool {
		if ordered[i].Client == ordered[j].Client {
			return ordered[i].Server < ordered[j].Server
		}
		return ordered[i].Client < ordered[j].Client
	})

	plans := make(map[string]*filePlan)
	planOrder := make([]string, 0)
	for _, operation := range ordered {
		server, ok := m.catalog.Server(operation.Server)
		if !ok {
			return fmt.Errorf("unknown MCP server %q", operation.Server)
		}
		path, format, err := m.clients.MCPProjectFile(m.projectRoot, operation.Client)
		if err != nil {
			return err
		}
		expected, err := projectDefinition(format, server)
		if err != nil {
			return fmt.Errorf("MCP server %s is incompatible with %s: %w", operation.Server, operation.Client, err)
		}
		plan, err := loadPlan(path, format, plans)
		if err != nil {
			return err
		}
		if _, seen := plans[plan.resolvedPath]; !seen {
			plans[plan.resolvedPath] = plan
			planOrder = append(planOrder, plan.resolvedPath)
		}
		existing, exists, err := plan.document.server(operation.Server)
		if err != nil {
			return fmt.Errorf("inspect %s server %s: %w", path, operation.Server, err)
		}
		if exists && !definitionsEqual(format, existing, expected) {
			conflicts = append(conflicts, Conflict{Path: path, Reason: fmt.Sprintf("server %q has a different project-owned definition", operation.Server)})
			continue
		}
		switch {
		case operation.Enabled && !exists:
			if err := plan.document.add(operation.Server, expected); err != nil {
				return fmt.Errorf("plan MCP enable in %s: %w", path, err)
			}
			plan.changed = true
		case !operation.Enabled && exists:
			if err := plan.document.remove(operation.Server); err != nil {
				return fmt.Errorf("plan MCP disable in %s: %w", path, err)
			}
			plan.changed = true
		}
	}
	if len(conflicts) > 0 {
		return &ConflictError{Conflicts: conflicts}
	}

	for _, key := range planOrder {
		plan := plans[key]
		if !plan.changed {
			continue
		}
		result, err := plan.document.bytes()
		if err != nil {
			return fmt.Errorf("render MCP config %s: %w", plan.requestedPath, err)
		}
		plan.result = result
	}
	for _, key := range planOrder {
		if err := verifyUnchanged(plans[key]); err != nil {
			return err
		}
	}

	committed := make([]*filePlan, 0, len(planOrder))
	for _, key := range planOrder {
		plan := plans[key]
		if !plan.changed || bytes.Equal(plan.original, plan.result) {
			continue
		}
		if m.beforeWrite != nil {
			m.beforeWrite(plan.requestedPath)
		}
		if err := verifyUnchanged(plan); err != nil {
			rollbackErr := rollbackPlans(committed)
			if rollbackErr != nil {
				return errors.Join(err, rollbackErr)
			}
			return err
		}
		created, err := ensureParentDirectory(filepath.Dir(plan.resolvedPath))
		if err != nil {
			rollbackErr := rollbackPlans(committed)
			if rollbackErr != nil {
				return errors.Join(err, rollbackErr)
			}
			return err
		}
		plan.createdDirs = created
		mode := plan.originalMode
		if !plan.existed {
			mode = 0o644
		}
		writeFile := m.writeFile
		if writeFile == nil {
			writeFile = atomicWrite
		}
		if err := writeFile(plan.resolvedPath, plan.result, mode); err != nil {
			cleanupDirectories(created)
			rollbackErr := rollbackPlans(committed)
			operationErr := fmt.Errorf("write MCP config %s: %w", plan.requestedPath, err)
			if rollbackErr != nil {
				return errors.Join(operationErr, rollbackErr)
			}
			return operationErr
		}
		committed = append(committed, plan)
	}
	return nil
}

func loadPlan(path string, format client.MCPFormat, existing map[string]*filePlan) (*filePlan, error) {
	resolved, err := resolveConfigPath(path)
	if err != nil {
		return nil, fmt.Errorf("resolve MCP config %s: %w", path, err)
	}
	if plan, ok := existing[resolved]; ok {
		if plan.format != format {
			return nil, fmt.Errorf("MCP config %s is registered with conflicting formats", resolved)
		}
		return plan, nil
	}
	data, exists, mode, err := readConfig(resolved)
	if err != nil {
		return nil, fmt.Errorf("read MCP config %s: %w", path, err)
	}
	documentData := data
	if !exists && (format == client.MCPClaudeJSON || format == client.MCPGeminiJSON) {
		documentData = []byte("{}\n")
	}
	document, err := parseDocument(format, documentData)
	if err != nil {
		return nil, fmt.Errorf("parse MCP config %s: %w", path, err)
	}
	return &filePlan{
		requestedPath: path,
		resolvedPath:  resolved,
		format:        format,
		original:      data,
		originalMode:  mode,
		existed:       exists,
		document:      document,
	}, nil
}
