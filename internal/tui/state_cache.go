package tui

import (
	"fmt"

	"github.com/est7/skills-switch-tui/internal/catalog"
	"github.com/est7/skills-switch-tui/internal/client"
	"github.com/est7/skills-switch-tui/internal/mcp"
	"github.com/est7/skills-switch-tui/internal/projection"
	"github.com/est7/skills-switch-tui/internal/systemprompt"
	"github.com/est7/skills-switch-tui/internal/userresource"
)

type cachedState[T ~string] struct {
	value T
	err   error
}

type skillStateKey struct {
	scope  projection.Scope
	skill  string
	client client.ID
}

type skillClientKey struct {
	skill  string
	client client.ID
}

type mcpStateKey struct {
	server string
	client client.ID
}

type userResourceStateKey struct {
	kind     userresource.Kind
	resource string
	client   client.ID
}

type stateCache struct {
	skills       map[skillStateKey]cachedState[projection.State]
	skillsByName map[string][]catalog.Skill
	mcp          map[mcpStateKey]cachedState[mcp.State]
	userResource map[userResourceStateKey]cachedState[userresource.State]
	prompts      map[string]cachedState[systemprompt.State]
}

var cachedSkillScopes = [...]struct {
	scope      projection.Scope
	capability client.Capability
}{
	{scope: projection.ScopeProject, capability: client.CapabilityProjectSkills},
	{scope: projection.ScopeGlobal, capability: client.CapabilityGlobalSkills},
}

func (m *Model) rebuildStateCache() {
	m.stateCache = stateCache{
		skills:       make(map[skillStateKey]cachedState[projection.State]),
		skillsByName: make(map[string][]catalog.Skill),
		mcp:          make(map[mcpStateKey]cachedState[mcp.State]),
		userResource: make(map[userResourceStateKey]cachedState[userresource.State]),
		prompts:      make(map[string]cachedState[systemprompt.State]),
	}

	skills := make([]catalog.Skill, 0)
	for _, source := range m.catalog.Sources {
		skills = append(skills, source.Skills...)
		for _, skill := range source.Skills {
			m.stateCache.skillsByName[skill.Name] = append(m.stateCache.skillsByName[skill.Name], skill)
		}
	}
	clients := m.catalog.Clients.IDs()
	m.refreshSkillStates(skills, clients)
	m.refreshMCPStates(m.mcpCatalog.Names(), clients)
	for _, set := range m.userResourceSets {
		m.refreshUserResourceStates(set.Catalog.Resources, clients)
	}
	m.refreshSystemPromptStates(m.prompts.Groups)
}

func (m *Model) refreshSkillStates(skills []catalog.Skill, clients []client.ID) {
	touched := make(map[skillClientKey]catalog.Skill, len(skills)*len(clients))
	for _, skill := range skills {
		for _, clientID := range clients {
			touched[skillClientKey{skill: skill.ID, client: clientID}] = skill
		}
	}
	m.refreshSkillClientStates(touched)
}

func (m *Model) refreshSkillOperationStates(operations []projection.Operation) {
	touched := make(map[skillClientKey]catalog.Skill)
	for _, operation := range operations {
		for _, skill := range operation.Skills {
			touched[skillClientKey{skill: skill.ID, client: operation.Client}] = skill
		}
	}
	m.refreshSkillClientStates(touched)
}

func (m *Model) refreshSkillClientStates(touched map[skillClientKey]catalog.Skill) {
	affected := make(map[skillClientKey]catalog.Skill, len(touched))
	for key, skill := range touched {
		for _, provider := range m.stateCache.skillsByName[skill.Name] {
			affected[skillClientKey{skill: provider.ID, client: key.client}] = provider
		}
	}
	for key, skill := range affected {
		for _, cachedScope := range cachedSkillScopes {
			if !m.catalog.Clients.Supports(key.client, cachedScope.capability) {
				continue
			}
			state, err := m.projection.StateAt(skill, key.client, cachedScope.scope)
			m.stateCache.skills[skillStateKey{scope: cachedScope.scope, skill: skill.ID, client: key.client}] = cachedState[projection.State]{value: state, err: err}
		}
	}
}

func (m *Model) refreshMCPStates(names []string, clients []client.ID) {
	touched := make(map[mcpStateKey]struct{}, len(names)*len(clients))
	for _, name := range names {
		for _, clientID := range clients {
			touched[mcpStateKey{server: name, client: clientID}] = struct{}{}
		}
	}
	m.refreshMCPKeys(touched)
}

func (m *Model) refreshMCPOperationStates(operations []mcp.Operation) {
	touched := make(map[mcpStateKey]struct{}, len(operations))
	for _, operation := range operations {
		touched[mcpStateKey{server: operation.Server, client: operation.Client}] = struct{}{}
	}
	m.refreshMCPKeys(touched)
}

func (m *Model) refreshMCPKeys(touched map[mcpStateKey]struct{}) {
	for key := range touched {
		state, err := m.mcpManager.State(key.server, key.client)
		m.stateCache.mcp[key] = cachedState[mcp.State]{value: state, err: err}
	}
}

func (m *Model) refreshUserResourceStates(resources []userresource.Resource, clients []client.ID) {
	touched := make(map[userResourceStateKey]userresource.Resource, len(resources)*len(clients))
	for _, resource := range resources {
		for _, clientID := range clients {
			key := userResourceStateKey{kind: resource.Kind, resource: resource.ID, client: clientID}
			touched[key] = resource
		}
	}
	m.refreshUserResourceKeys(touched)
}

func (m *Model) refreshUserResourceOperationStates(operations []userresource.Operation) {
	touched := make(map[userResourceStateKey]userresource.Resource, len(operations))
	for _, operation := range operations {
		key := userResourceStateKey{kind: operation.Resource.Kind, resource: operation.Resource.ID, client: operation.Client}
		touched[key] = operation.Resource
	}
	m.refreshUserResourceKeys(touched)
}

func (m *Model) refreshUserResourceKeys(touched map[userResourceStateKey]userresource.Resource) {
	for key, resource := range touched {
		manager := m.userResourceSets[resource.Kind].Manager
		state, err := manager.State(resource, key.client)
		m.stateCache.userResource[key] = cachedState[userresource.State]{value: state, err: err}
	}
}

func (m *Model) refreshSystemPromptStates(groups []systemprompt.Group) {
	for _, group := range groups {
		state, err := m.promptMgr.State(group)
		m.stateCache.prompts[group.ID] = cachedState[systemprompt.State]{value: state, err: err}
	}
}

func (m Model) skillState(skill catalog.Skill, clientID catalog.Client) (projection.State, error) {
	state, ok := m.stateCache.skills[skillStateKey{scope: m.skillScope, skill: skill.ID, client: clientID}]
	if !ok {
		return "", fmt.Errorf("projection state cache missing skill %s for client %s at %s scope", skill.ID, clientID, m.skillScope)
	}
	return state.value, state.err
}

func (m Model) mcpState(name string, clientID catalog.Client) (mcp.State, error) {
	state, ok := m.stateCache.mcp[mcpStateKey{server: name, client: clientID}]
	if !ok {
		return "", fmt.Errorf("MCP state cache missing server %s for client %s", name, clientID)
	}
	return state.value, state.err
}

func (m Model) userResourceState(resource userresource.Resource, clientID catalog.Client) (userresource.State, error) {
	state, ok := m.stateCache.userResource[userResourceStateKey{kind: resource.Kind, resource: resource.ID, client: clientID}]
	if !ok {
		return "", fmt.Errorf("user resource state cache missing %s %s for client %s", resource.Kind, resource.ID, clientID)
	}
	return state.value, state.err
}

func (m Model) systemPromptState(group systemprompt.Group) (systemprompt.State, error) {
	state, ok := m.stateCache.prompts[group.ID]
	if !ok {
		return "", fmt.Errorf("system prompt state cache missing group %s", group.ID)
	}
	return state.value, state.err
}
