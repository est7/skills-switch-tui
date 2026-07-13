package tui

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"charm.land/bubbles/v2/help"
	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"github.com/est7/skills-switch-tui/internal/catalog"
	"github.com/est7/skills-switch-tui/internal/i18n"
	"github.com/est7/skills-switch-tui/internal/mcp"
	"github.com/est7/skills-switch-tui/internal/projection"
	"github.com/est7/skills-switch-tui/internal/source"
	"github.com/est7/skills-switch-tui/internal/systemprompt"
)

type resourceTab int

const (
	tabSkills resourceTab = iota
	tabMCP
	tabSystemPrompts
)

type filterMode int

const (
	filterAll filterMode = iota
	filterEnabled
	filterIssues
	filterArchive
)

func (f filterMode) String() string {
	switch f {
	case filterAll:
		return "all"
	case filterEnabled:
		return "enabled"
	case filterIssues:
		return "issues"
	case filterArchive:
		return "archive"
	default:
		return "unknown"
	}
}

type rowKind int

const (
	sourceRow rowKind = iota
	skillRow
)

type row struct {
	kind        rowKind
	sourceIndex int
	skillIndex  int
}

type updateFinishedMsg struct {
	results []source.UpdateResult
	err     error
}

type Model struct {
	catalog     catalog.Catalog
	project     string
	userHome    string
	projection  projection.Manager
	mcpCatalog  mcp.Catalog
	mcpManager  mcp.Manager
	prompts     systemprompt.Catalog
	promptMgr   systemprompt.Manager
	updater     *source.Manager
	tab         resourceTab
	clientIndex int
	cursor      int
	offset      int
	width       int
	height      int
	expanded    map[string]bool
	filter      filterMode
	searching   bool
	search      textinput.Model
	help        help.Model
	keys        keyMap
	showHelp    bool
	updating    bool
	isDark      bool
	styles      styles
	translator  i18n.Translator
	status      string
	err         error
	context     context.Context
	cancel      context.CancelFunc
}

type Resources struct {
	MCPCatalog    mcp.Catalog
	MCPManager    mcp.Manager
	Prompts       systemprompt.Catalog
	PromptManager systemprompt.Manager
	UserHome      string
}

func NewModel(loaded catalog.Catalog, projectRoot string, manager projection.Manager, updater *source.Manager, translator i18n.Translator, resources ...Resources) Model {
	search := textinput.New()
	search.Prompt = "/ "
	search.Placeholder = translator.Text(i18n.SearchPlaceholder)
	search.CharLimit = 120
	search.SetWidth(36)
	search.SetVirtualCursor(true)
	search.SetStyles(textinput.DefaultDarkStyles())
	helpModel := help.New()
	helpModel.ShowAll = false
	helpModel.SetWidth(96)
	theme := newStyles(true)
	applyHelpStyles(&helpModel, theme)
	operationContext, cancel := context.WithCancel(context.Background())
	model := Model{
		catalog:    loaded,
		project:    projectRoot,
		projection: manager,
		updater:    updater,
		width:      100,
		height:     30,
		expanded:   make(map[string]bool),
		search:     search,
		help:       helpModel,
		keys:       defaultKeyMap(translator),
		isDark:     true,
		styles:     theme,
		translator: translator,
		status:     translator.Text(i18n.Ready),
		context:    operationContext,
		cancel:     cancel,
	}
	if len(resources) > 0 {
		model.mcpCatalog = resources[0].MCPCatalog
		model.mcpManager = resources[0].MCPManager
		model.prompts = resources[0].Prompts
		model.promptMgr = resources[0].PromptManager
		model.userHome = resources[0].UserHome
	}
	return model
}

func (m Model) Init() tea.Cmd {
	return tea.RequestBackgroundColor
}

func (m Model) Update(message tea.Msg) (tea.Model, tea.Cmd) {
	switch message := message.(type) {
	case tea.BackgroundColorMsg:
		m.isDark = message.IsDark()
		m.styles = newStyles(m.isDark)
		applyHelpStyles(&m.help, m.styles)
		m.search.SetStyles(textinput.DefaultStyles(m.isDark))
		return m, nil
	case tea.WindowSizeMsg:
		m.width = max(message.Width, 48)
		m.height = max(message.Height, 16)
		m.search.SetWidth(min(48, m.contentWidth()-8))
		m.help.SetWidth(m.contentWidth())
		m.clampCursor()
		return m, nil
	case updateFinishedMsg:
		m.updating = false
		reloaded, reloadErr := catalog.Load(m.catalog.Root, m.catalog.Clients)
		if reloadErr == nil {
			m.catalog = reloaded
			m.projection = projection.New(m.project, reloaded)
			if clients := m.catalog.Clients.IDs(); m.clientIndex >= len(clients) {
				m.clientIndex = max(0, len(clients)-1)
			}
			m.clampCursor()
		}
		if message.err != nil {
			m.err = message.err
			if reloadErr != nil {
				m.err = errors.Join(m.err, fmt.Errorf("reload catalog after failed update: %w", reloadErr))
			}
			m.status = m.translator.Text(i18n.UpdateFailed)
			return m, nil
		}
		if reloadErr != nil {
			m.err = fmt.Errorf("reload catalog after update: %w", reloadErr)
			m.status = m.translator.Text(i18n.UpdateReloadFailed)
			return m, nil
		}
		changed := 0
		for _, result := range message.results {
			if result.Changed {
				changed++
			}
		}
		m.err = nil
		m.status = m.translator.Text(i18n.UpdatedSources, len(message.results), changed)
		return m, nil
	}

	key, ok := message.(tea.KeyPressMsg)
	if !ok {
		return m, nil
	}
	if key.String() == "ctrl+c" {
		m.cancel()
		return m, tea.Quit
	}
	if m.searching {
		return m.updateSearch(key)
	}
	m.err = nil

	switch key.String() {
	case "q":
		m.cancel()
		return m, tea.Quit
	case "?":
		m.showHelp = !m.showHelp
		m.help.ShowAll = m.showHelp
	case "/":
		m.searching = true
		return m, m.search.Focus()
	case "tab":
		m.cycleResource(1)
	case "shift+tab":
		m.cycleResource(-1)
	case "f":
		m.cycleFilter(1)
	case "up", "k":
		m.moveCursor(-1)
	case "down", "j":
		m.moveCursor(1)
	case "left", "h":
		m.moveClient(-1)
	case "right", "l":
		m.moveClient(1)
	case "enter":
		m.toggleExpanded()
	case "space":
		m.toggleSelection()
	case "a":
		m.toggleAllClients()
	case "u":
		return m.startUpdate(false)
	case "U":
		return m.startUpdate(true)
	case "L":
		m.toggleLanguage()
	}
	return m, nil
}

func (m *Model) toggleLanguage() {
	language := i18n.Chinese
	if m.translator.Language() == i18n.Chinese {
		language = i18n.English
	}
	m.translator = i18n.New(language)
	m.keys = defaultKeyMap(m.translator)
	m.search.Placeholder = m.translator.Text(i18n.SearchPlaceholder)
	m.status = m.translator.Text(i18n.Ready)
	m.err = nil
}

func (m Model) updateSearch(key tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	switch key.String() {
	case "esc":
		m.searching = false
		m.search.Blur()
		m.search.Reset()
		m.cursor = 0
		m.offset = 0
		return m, nil
	case "enter":
		m.searching = false
		m.search.Blur()
		m.cursor = 0
		m.offset = 0
		return m, nil
	}
	var command tea.Cmd
	m.search, command = m.search.Update(key)
	m.cursor = 0
	m.offset = 0
	return m, command
}

func (m *Model) cycleFilter(delta int) {
	count := int(filterIssues) + 1
	if m.tab == tabSkills {
		count = int(filterArchive) + 1
	}
	next := (int(m.filter) + delta + count) % count
	m.filter = filterMode(next)
	m.cursor = 0
	m.offset = 0
	m.status = m.translator.Text(i18n.FilterSelected, m.filterLabel(m.filter))
}

func (m *Model) cycleResource(delta int) {
	count := int(tabSystemPrompts) + 1
	m.tab = resourceTab((int(m.tab) + delta + count) % count)
	if m.tab != tabSkills && m.filter == filterArchive {
		m.filter = filterAll
	}
	m.cursor = 0
	m.offset = 0
	m.status = m.translator.Text(i18n.Ready)
}

func (m *Model) moveCursor(delta int) {
	rowCount := m.activeRowCount()
	if rowCount == 0 {
		m.cursor = 0
		return
	}
	m.cursor = (m.cursor + delta + rowCount) % rowCount
	m.ensureCursorVisible(rowCount)
}

func (m *Model) moveClient(delta int) {
	clients := m.catalog.Clients.IDs()
	m.clientIndex = (m.clientIndex + delta + len(clients)) % len(clients)
	m.status = m.translator.Text(i18n.TargetSelected, m.currentClient())
}

func (m *Model) toggleExpanded() {
	if m.tab != tabSkills {
		return
	}
	selected, ok := m.selectedRow()
	if !ok || selected.kind != sourceRow {
		return
	}
	source := m.catalog.Sources[selected.sourceIndex]
	m.expanded[source.ID] = !m.expanded[source.ID]
	m.clampCursor()
}

func (m *Model) toggleSelection() {
	switch m.tab {
	case tabMCP:
		m.toggleMCPSelection()
		return
	case tabSystemPrompts:
		m.togglePromptSelection()
		return
	}
	m.toggleSkillSelection()
}

func (m *Model) toggleSkillSelection() {
	selected, ok := m.selectedRow()
	if !ok {
		m.status = m.translator.Text(i18n.NoMatchingSkills)
		return
	}
	source := m.catalog.Sources[selected.sourceIndex]
	client := m.currentClient()
	var skills []catalog.Skill
	enable := false
	if selected.kind == sourceRow {
		compatible := compatibleSkills(source.Skills, client)
		staleIncompatibleLink := false
		for _, skill := range source.Skills {
			state, err := m.projection.State(skill, client)
			if err != nil {
				m.err = err
				m.status = m.translator.Text(i18n.InspectProjectFailed)
				return
			}
			if state == projection.StateIncompatibleEnabled {
				staleIncompatibleLink = true
			}
		}
		if staleIncompatibleLink {
			skills = source.Skills
		} else {
			if len(compatible) == 0 {
				m.status = m.translator.Text(i18n.NoCompatibleSkills, client)
				return
			}
			skills = compatible
			for _, skill := range compatible {
				state, err := m.projection.State(skill, client)
				if err != nil {
					m.err = err
					m.status = m.translator.Text(i18n.InspectProjectFailed)
					return
				}
				if state != projection.StateEnabled {
					enable = true
					break
				}
			}
			if !enable {
				skills = source.Skills
			}
		}
	} else {
		skill := source.Skills[selected.skillIndex]
		state, err := m.projection.State(skill, client)
		if err != nil {
			m.err = err
			m.status = m.translator.Text(i18n.InspectProjectFailed)
			return
		}
		if !skill.Supports(client) && state != projection.StateIncompatibleEnabled {
			reason := skill.CompatibilityReason
			if reason == "" {
				reason = m.translator.Text(i18n.CatalogCompatibility)
			}
			m.err = errors.New(m.translator.Text(i18n.UnavailableForClient, skill.ID, client, reason))
			m.status = m.translator.Text(i18n.IncompatibleSkill)
			return
		}
		skills = []catalog.Skill{skill}
		enable = state != projection.StateEnabled && state != projection.StateIncompatibleEnabled
	}
	if source.IsArchived() && enable {
		m.err = errors.New(m.translator.Text(i18n.ArchiveReferenceError, source.ID))
		m.status = m.translator.Text(i18n.ArchiveCannotEnable)
		return
	}
	if err := m.projection.SetEnabled(skills, client, enable); err != nil {
		m.err = err
		m.status = m.translator.Text(i18n.NoChangesApplied)
		return
	}
	m.err = nil
	resultKey := i18n.DisabledSkills
	if enable {
		resultKey = i18n.EnabledSkills
	}
	m.status = m.translator.Text(resultKey, len(skills), client)
}

func (m *Model) toggleAllClients() {
	if m.tab != tabSkills {
		m.status = m.translator.Text(i18n.AllClientsSkillOnly)
		return
	}
	selected, ok := m.selectedRow()
	if !ok {
		m.status = m.translator.Text(i18n.SelectSkillForAllClients)
		return
	}

	source := m.catalog.Sources[selected.sourceIndex]
	skills := source.Skills
	sourceSelection := selected.kind == sourceRow
	if !sourceSelection {
		skills = []catalog.Skill{source.Skills[selected.skillIndex]}
	}
	clients := m.catalog.Clients.IDs()
	type stateKey struct {
		skill  int
		client catalog.Client
	}
	states := make(map[stateKey]projection.State, len(skills)*len(clients))
	enable := false
	compatibleCount := 0
	hasManagedProjection := false
	for skillIndex, skill := range skills {
		for _, clientID := range clients {
			state, err := m.projection.State(skill, clientID)
			if err != nil {
				m.err = err
				m.status = m.translator.Text(i18n.InspectProjectFailed)
				return
			}
			states[stateKey{skill: skillIndex, client: clientID}] = state
			if state == projection.StateEnabled || state == projection.StateIncompatibleEnabled {
				hasManagedProjection = true
			}
			if !skill.Supports(clientID) {
				continue
			}
			compatibleCount++
			if state != projection.StateEnabled {
				enable = true
			}
		}
	}
	if compatibleCount == 0 && !hasManagedProjection {
		m.status = m.translator.Text(i18n.NoCompatibleClients, source.ID)
		return
	}
	if source.IsArchived() && enable {
		m.err = errors.New(m.translator.Text(i18n.ArchiveReferenceError, source.ID))
		m.status = m.translator.Text(i18n.ArchiveCannotEnable)
		return
	}

	operations := make([]projection.Operation, 0, len(clients)*2)
	for _, clientID := range clients {
		toEnable := make([]catalog.Skill, 0, len(skills))
		toDisable := make([]catalog.Skill, 0, len(skills))
		for skillIndex, skill := range skills {
			state := states[stateKey{skill: skillIndex, client: clientID}]
			if enable {
				if skill.Supports(clientID) {
					toEnable = append(toEnable, skill)
				} else if state == projection.StateIncompatibleEnabled {
					toDisable = append(toDisable, skill)
				}
				continue
			}
			if state == projection.StateEnabled || state == projection.StateIncompatibleEnabled {
				toDisable = append(toDisable, skill)
			}
		}
		if len(toEnable) > 0 {
			operations = append(operations, projection.Operation{Skills: toEnable, Client: clientID, Enabled: true})
		}
		if len(toDisable) > 0 {
			operations = append(operations, projection.Operation{Skills: toDisable, Client: clientID, Enabled: false})
		}
	}
	if err := m.projection.Apply(operations); err != nil {
		m.err = err
		m.status = m.translator.Text(i18n.NoChangesApplied)
		return
	}
	m.err = nil
	if sourceSelection {
		resultKey := i18n.DisabledSourceAllClients
		if enable {
			resultKey = i18n.EnabledSourceAllClients
		}
		m.status = m.translator.Text(resultKey, source.ID, compatibleCount)
		return
	}
	skill := skills[0]
	resultKey := i18n.DisabledSkillAllClients
	if enable {
		resultKey = i18n.EnabledSkillAllClients
	}
	m.status = m.translator.Text(resultKey, skill.Name, compatibleCount)
}

func (m *Model) toggleMCPSelection() {
	names := m.mcpNames()
	if len(names) == 0 || m.cursor >= len(names) {
		m.status = m.translator.Text(i18n.NoSelection)
		return
	}
	name := names[m.cursor]
	clientID := m.currentClient()
	state, err := m.mcpManager.State(name, clientID)
	if err != nil {
		m.err = err
		m.status = m.translator.Text(i18n.InspectProjectFailed)
		return
	}
	if state == mcp.StateIncompatible {
		m.err = errors.New(m.translator.Text(i18n.ResourceIncompatible, name, clientID))
		m.status = m.translator.Text(i18n.NoChangesApplied)
		return
	}
	enable := state != mcp.StateEnabled
	if err := m.mcpManager.Apply([]mcp.Operation{{Server: name, Client: clientID, Enabled: enable}}); err != nil {
		m.err = err
		m.status = m.translator.Text(i18n.NoChangesApplied)
		return
	}
	m.err = nil
	key := i18n.DisabledResource
	if enable {
		key = i18n.EnabledResource
	}
	m.status = m.translator.Text(key, name, clientID)
}

func (m *Model) togglePromptSelection() {
	groups := m.promptGroups()
	if len(groups) == 0 || m.cursor >= len(groups) {
		m.status = m.translator.Text(i18n.NoSelection)
		return
	}
	group := groups[m.cursor]
	clientID := m.currentClient()
	if group.Client != clientID {
		m.err = errors.New(m.translator.Text(i18n.ResourceIncompatible, group.ID, clientID))
		m.status = m.translator.Text(i18n.NoChangesApplied)
		return
	}
	state, err := m.promptMgr.State(group)
	if err != nil {
		m.err = err
		m.status = m.translator.Text(i18n.InspectProjectFailed)
		return
	}
	enable := state != systemprompt.StateEnabled
	if err := m.promptMgr.SetEnabled([]systemprompt.Group{group}, enable); err != nil {
		m.err = err
		m.status = m.translator.Text(i18n.NoChangesApplied)
		return
	}
	m.err = nil
	key := i18n.DisabledResource
	if enable {
		key = i18n.EnabledResource
	}
	m.status = m.translator.Text(key, group.ID, clientID)
}

func (m Model) startUpdate(all bool) (tea.Model, tea.Cmd) {
	if m.tab != tabSkills {
		m.status = m.translator.Text(i18n.UpdatesUnavailable)
		return m, nil
	}
	if m.updating {
		return m, nil
	}
	if m.updater == nil {
		m.status = m.translator.Text(i18n.UpdatesUnavailable)
		return m, nil
	}
	selectedSources := make([]catalog.Source, 0, len(m.catalog.Sources))
	if all {
		for _, candidate := range m.catalog.Sources {
			if candidate.IsVendor() && !candidate.IsArchived() {
				selectedSources = append(selectedSources, candidate)
			}
		}
		if len(selectedSources) == 0 {
			m.status = m.translator.Text(i18n.NoVendorSources)
			return m, nil
		}
	} else {
		selected, ok := m.selectedRow()
		if !ok {
			m.status = m.translator.Text(i18n.NoSourceSelected)
			return m, nil
		}
		selectedSource := m.catalog.Sources[selected.sourceIndex]
		if selectedSource.IsArchived() || !selectedSource.IsVendor() {
			m.status = m.translator.Text(i18n.VendorOnlyUpdate)
			return m, nil
		}
		selectedSources = append(selectedSources, selectedSource)
	}
	m.updating = true
	m.err = nil
	if all {
		m.status = m.translator.Text(i18n.UpdatingAllSources, len(selectedSources))
	} else {
		m.status = m.translator.Text(i18n.UpdatingSource, selectedSources[0].ID)
	}
	updater := *m.updater
	return m, func() tea.Msg {
		results, err := updater.Update(m.context, selectedSources, false)
		return updateFinishedMsg{results: results, err: err}
	}
}

func (m Model) currentClient() catalog.Client {
	return m.catalog.Clients.IDs()[m.clientIndex]
}

func (m Model) selectedRow() (row, bool) {
	rows := m.rows()
	if len(rows) == 0 || m.cursor < 0 || m.cursor >= len(rows) {
		return row{}, false
	}
	return rows[m.cursor], true
}

func (m *Model) clampCursor() {
	rowCount := m.activeRowCount()
	if rowCount == 0 {
		m.cursor = 0
		m.offset = 0
		return
	}
	if m.cursor >= rowCount {
		m.cursor = rowCount - 1
	}
	m.ensureCursorVisible(rowCount)
}

func (m Model) activeRowCount() int {
	switch m.tab {
	case tabMCP:
		return len(m.mcpNames())
	case tabSystemPrompts:
		return len(m.promptGroups())
	default:
		return len(m.rows())
	}
}

func (m Model) mcpNames() []string {
	query := strings.ToLower(strings.TrimSpace(m.search.Value()))
	result := make([]string, 0, len(m.mcpCatalog.Servers))
	for _, name := range m.mcpCatalog.Names() {
		if query != "" && !containsFold(name, query) {
			continue
		}
		if !m.mcpMatchesFilter(name) {
			continue
		}
		result = append(result, name)
	}
	return result
}

func (m Model) mcpMatchesFilter(name string) bool {
	if m.filter == filterAll {
		return true
	}
	for _, clientID := range m.catalog.Clients.IDs() {
		state, err := m.mcpManager.State(name, clientID)
		if err != nil {
			return m.filter == filterIssues
		}
		if m.filter == filterEnabled && state == mcp.StateEnabled {
			return true
		}
		if m.filter == filterIssues && state == mcp.StateConflict {
			return true
		}
	}
	return false
}

func (m Model) promptGroups() []systemprompt.Group {
	query := strings.ToLower(strings.TrimSpace(m.search.Value()))
	result := make([]systemprompt.Group, 0, len(m.prompts.Groups))
	for _, group := range m.prompts.Groups {
		if query != "" && !containsFold(group.ID+" "+string(group.Client), query) {
			continue
		}
		state, err := m.promptMgr.State(group)
		if m.filter == filterEnabled && (err != nil || state != systemprompt.StateEnabled) {
			continue
		}
		if m.filter == filterIssues && err == nil && state != systemprompt.StateConflict && state != systemprompt.StateBroken && state != systemprompt.StatePartial {
			continue
		}
		result = append(result, group)
	}
	return result
}

func (m *Model) ensureCursorVisible(rowCount int) {
	visible := m.visibleRowCount()
	if m.cursor < m.offset {
		m.offset = m.cursor
	}
	if m.cursor >= m.offset+visible {
		m.offset = m.cursor - visible + 1
	}
	maximumOffset := max(0, rowCount-visible)
	if m.offset > maximumOffset {
		m.offset = maximumOffset
	}
}

func (m Model) visibleRowCount() int {
	reserved := 13
	if m.showHelp {
		reserved = 17
	}
	return max(3, m.height-reserved)
}

func (m Model) rows() []row {
	query := strings.ToLower(strings.TrimSpace(m.search.Value()))
	rows := make([]row, 0)
	for sourceIndex, source := range m.catalog.Sources {
		if m.filter == filterArchive {
			if !source.IsArchived() {
				continue
			}
		} else if source.IsArchived() {
			continue
		}

		matchingSkills := make([]int, 0, len(source.Skills))
		for skillIndex, skill := range source.Skills {
			if !m.skillMatchesFilter(skill) {
				continue
			}
			if query != "" && !containsFold(skill.ID+" "+skill.Name+" "+skill.Description, query) {
				continue
			}
			matchingSkills = append(matchingSkills, skillIndex)
		}
		sourceMatchesQuery := query == "" || containsFold(source.ID, query)
		if !sourceMatchesQuery && len(matchingSkills) == 0 {
			continue
		}
		if query == "" && !m.sourceMatchesFilter(source) {
			continue
		}

		rows = append(rows, row{kind: sourceRow, sourceIndex: sourceIndex, skillIndex: -1})
		if m.expanded[source.ID] || query != "" {
			if sourceMatchesQuery && query != "" && len(matchingSkills) == 0 {
				for skillIndex, skill := range source.Skills {
					if m.skillMatchesFilter(skill) {
						matchingSkills = append(matchingSkills, skillIndex)
					}
				}
			}
			for _, skillIndex := range matchingSkills {
				rows = append(rows, row{kind: skillRow, sourceIndex: sourceIndex, skillIndex: skillIndex})
			}
		}
	}
	return rows
}

func (m Model) sourceMatchesFilter(source catalog.Source) bool {
	if m.filter == filterAll || m.filter == filterArchive {
		return true
	}
	for _, skill := range source.Skills {
		if m.skillMatchesFilter(skill) {
			return true
		}
	}
	return false
}

func (m Model) skillMatchesFilter(skill catalog.Skill) bool {
	if m.filter == filterAll || m.filter == filterArchive {
		return true
	}
	for _, client := range m.catalog.Clients.IDs() {
		state, err := m.projection.State(skill, client)
		if err != nil {
			return m.filter == filterIssues
		}
		if m.filter == filterEnabled && state == projection.StateEnabled {
			return true
		}
		if m.filter == filterIssues && (state == projection.StateConflict || state == projection.StateBroken || state == projection.StateIncompatibleEnabled) {
			return true
		}
	}
	return false
}

func compatibleSkills(skills []catalog.Skill, client catalog.Client) []catalog.Skill {
	compatible := make([]catalog.Skill, 0, len(skills))
	for _, skill := range skills {
		if skill.Supports(client) {
			compatible = append(compatible, skill)
		}
	}
	return compatible
}

func containsFold(value, lowerQuery string) bool {
	return strings.Contains(strings.ToLower(value), lowerQuery)
}
