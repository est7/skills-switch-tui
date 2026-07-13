package tui

import (
	"fmt"
	"path/filepath"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/est7/skills-switch-tui/internal/catalog"
	"github.com/est7/skills-switch-tui/internal/i18n"
	"github.com/est7/skills-switch-tui/internal/projection"
	"github.com/est7/skills-switch-tui/internal/systemprompt"
)

const clientColumnWidth = 10

func (m Model) View() tea.View {
	sections := []string{m.renderHeader(), m.renderTable(), m.renderDetail(), m.renderFooter()}
	view := tea.NewView(lipgloss.NewStyle().Padding(1, 2).Render(strings.Join(sections, "\n")))
	view.AltScreen = true
	view.WindowTitle = "skills-switch · " + filepath.Base(m.project)
	return view
}

func (m Model) renderHeader() string {
	title := m.styles.title.Render("skills-switch")
	scopeLabel := i18n.ProjectLabel
	scopePath := m.project
	if m.tab == tabSystemPrompts {
		scopeLabel = i18n.UserLabel
		scopePath = m.userHome
	}
	scope := m.styles.subtle.Render(m.translator.Text(scopeLabel) + "  " + scopePath)
	tabs := make([]string, 0, int(tabSystemPrompts)+1)
	for candidate := tabSkills; candidate <= tabSystemPrompts; candidate++ {
		style := m.styles.filter
		if candidate == m.tab {
			style = m.styles.activeFilter
		}
		tabs = append(tabs, style.Render(m.tabLabel(candidate)))
	}
	filters := make([]string, 0, int(filterArchive)+1)
	lastFilter := filterIssues
	if m.tab == tabSkills {
		lastFilter = filterArchive
	}
	for candidate := filterAll; candidate <= lastFilter; candidate++ {
		style := m.styles.filter
		if candidate == m.filter {
			style = m.styles.activeFilter
		}
		filters = append(filters, style.Render(m.filterLabel(candidate)))
	}
	filterLine := strings.Join(filters, " ")
	if m.searching || m.search.Value() != "" {
		filterLine += "  " + m.search.View()
	}
	return strings.Join(tabs, " ") + "\n\n" + title + "  " + m.styles.subtle.Render(m.translator.Text(i18n.ProductSubtitle)) + "\n" + scope + "\n\n" + filterLine
}

func (m Model) renderTable() string {
	switch m.tab {
	case tabMCP:
		return m.renderMCPTable()
	case tabSystemPrompts:
		return m.renderPromptTable()
	default:
		return m.renderSkillsTable()
	}
}

func (m Model) renderSkillsTable() string {
	clientStart, clientEnd := m.visibleClientRange()
	visibleClientCount := clientEnd - clientStart
	tableWidth := max(44, m.width-4)
	labelWidth := max(18, tableWidth-2-clientColumnWidth*visibleClientCount)
	header := m.styles.tableHeader.Width(tableWidth).Render(
		"  " + lipgloss.NewStyle().Width(labelWidth).Render(m.translator.Text(i18n.SourceSkillHeader)) + m.renderClientHeaders(clientStart, clientEnd),
	)
	rows := m.rows()
	if len(rows) == 0 {
		return header + "\n" + m.styles.subtle.Padding(1, 2).Render(m.translator.Text(i18n.NoSkillsMatch))
	}
	start := min(m.offset, len(rows)-1)
	end := min(len(rows), start+m.visibleRowCount())
	lines := []string{header}
	for index := start; index < end; index++ {
		lines = append(lines, m.renderRow(rows[index], index == m.cursor, labelWidth, clientStart, clientEnd, tableWidth))
	}
	if end < len(rows) {
		lines = append(lines, m.styles.subtle.Render("  "+m.translator.Text(i18n.MoreRows, len(rows)-end)))
	}
	return strings.Join(lines, "\n")
}

func (m Model) renderMCPTable() string {
	names := m.mcpNames()
	return m.renderResourceTable(names, func(name string, clientID catalog.Client) (string, lipgloss.Style) {
		state, err := m.mcpManager.State(name, clientID)
		if err != nil {
			return "!", m.styles.issue
		}
		switch state {
		case "enabled":
			return "[x]", m.styles.enabled
		case "disabled":
			return "[ ]", m.styles.disabled
		case "incompatible":
			return "—", m.styles.incompatible
		default:
			return "!", m.styles.issue
		}
	})
}

func (m Model) renderPromptTable() string {
	groups := m.promptGroups()
	labels := make([]string, len(groups))
	byID := make(map[string]systemPromptRow, len(groups))
	for index, group := range groups {
		labels[index] = group.ID
		byID[group.ID] = systemPromptRow{client: group.Client, state: m.promptState(group)}
	}
	return m.renderResourceTable(labels, func(name string, clientID catalog.Client) (string, lipgloss.Style) {
		row := byID[name]
		if row.client != clientID {
			return "—", m.styles.incompatible
		}
		switch row.state {
		case "enabled":
			return "[x]", m.styles.enabled
		case "disabled":
			return "[ ]", m.styles.disabled
		case "partial":
			return "~", m.styles.issue
		default:
			return "!", m.styles.issue
		}
	})
}

type systemPromptRow struct {
	client catalog.Client
	state  string
}

func (m Model) promptState(group systemprompt.Group) string {
	state, err := m.promptMgr.State(group)
	if err != nil {
		return "error"
	}
	return string(state)
}

func (m Model) renderResourceTable(labels []string, cell func(string, catalog.Client) (string, lipgloss.Style)) string {
	clientStart, clientEnd := m.visibleClientRange()
	visibleClientCount := clientEnd - clientStart
	tableWidth := max(44, m.width-4)
	labelWidth := max(18, tableWidth-2-clientColumnWidth*visibleClientCount)
	header := m.styles.tableHeader.Width(tableWidth).Render(
		"  " + lipgloss.NewStyle().Width(labelWidth).Render(m.translator.Text(i18n.ResourceHeader)) + m.renderClientHeaders(clientStart, clientEnd),
	)
	if len(labels) == 0 {
		return header + "\n" + m.styles.subtle.Padding(1, 2).Render(m.translator.Text(i18n.NoResourcesMatch))
	}
	start := min(m.offset, len(labels)-1)
	end := min(len(labels), start+m.visibleRowCount())
	lines := []string{header}
	clients := m.catalog.Clients.IDs()
	for index := start; index < end; index++ {
		selected := index == m.cursor
		cursor := "  "
		if selected {
			cursor = m.styles.accent.Render("› ")
		}
		label := lipgloss.NewStyle().Width(labelWidth).MaxWidth(labelWidth).Render(m.styles.child.Render(labels[index]))
		var cells strings.Builder
		for clientIndex := clientStart; clientIndex < clientEnd; clientIndex++ {
			value, style := cell(labels[index], clients[clientIndex])
			if selected && clientIndex == m.clientIndex {
				style = m.styles.selectedCell
			}
			cells.WriteString(style.Width(clientColumnWidth).Align(lipgloss.Center).Render(value))
		}
		line := cursor + label + cells.String()
		if selected {
			line = m.styles.selected.Width(tableWidth).Render(line)
		}
		lines = append(lines, line)
	}
	return strings.Join(lines, "\n")
}

func (m Model) renderClientHeaders(start, end int) string {
	var builder strings.Builder
	clients := m.catalog.Clients.IDs()
	for index := start; index < end; index++ {
		client := clients[index]
		style := m.styles.subtle
		if index == m.clientIndex {
			style = m.styles.accent
		}
		label := truncate(strings.ToUpper(string(client)), clientColumnWidth-2)
		if index == start && start > 0 {
			label = "‹" + label
		}
		if index == end-1 && end < len(clients) {
			label += "›"
		}
		builder.WriteString(style.Width(clientColumnWidth).Align(lipgloss.Center).Render(label))
	}
	return builder.String()
}

func (m Model) renderRow(item row, selected bool, labelWidth, clientStart, clientEnd, tableWidth int) string {
	source := m.catalog.Sources[item.sourceIndex]
	cursor := "  "
	if selected {
		cursor = m.styles.accent.Render("› ")
	}
	var label string
	if item.kind == sourceRow {
		disclosure := "▸"
		if m.expanded[source.ID] || strings.TrimSpace(m.search.Value()) != "" {
			disclosure = "▾"
		}
		archive := ""
		if source.IsArchived() {
			archive = "  " + m.translator.Text(i18n.ArchivedLabel)
		}
		label = m.styles.group.Render(disclosure+" "+source.ID) + m.styles.subtle.Render(archive)
	} else {
		skill := source.Skills[item.skillIndex]
		descriptionWidth := max(0, labelWidth-len([]rune(skill.Name))-6)
		description := truncate(skill.Description, descriptionWidth)
		label = m.styles.child.Render("  "+skill.Name) + m.styles.subtle.Render("  "+description)
	}
	label = lipgloss.NewStyle().Width(labelWidth).MaxWidth(labelWidth).Render(label)

	var cells strings.Builder
	clients := m.catalog.Clients.IDs()
	for clientIndex := clientStart; clientIndex < clientEnd; clientIndex++ {
		client := clients[clientIndex]
		value, style := m.cell(item, client)
		if selected && clientIndex == m.clientIndex {
			style = m.styles.selectedCell
		}
		cells.WriteString(style.Width(clientColumnWidth).Align(lipgloss.Center).Render(value))
	}
	line := cursor + label + cells.String()
	if selected {
		return m.styles.selected.Width(tableWidth).Render(line)
	}
	return line
}

func (m Model) visibleClientRange() (int, int) {
	clients := m.catalog.Clients.IDs()
	if len(clients) == 0 {
		return 0, 0
	}
	innerWidth := max(44, m.width-4)
	capacity := max(1, (innerWidth-2-18)/clientColumnWidth)
	capacity = min(capacity, len(clients))
	start := m.clientIndex - capacity/2
	start = max(0, min(start, len(clients)-capacity))
	return start, start + capacity
}

func (m Model) cell(item row, client catalog.Client) (string, lipgloss.Style) {
	source := m.catalog.Sources[item.sourceIndex]
	if item.kind == skillRow {
		return m.stateCell(source.Skills[item.skillIndex], client)
	}
	compatible := 0
	enabled := 0
	for _, skill := range source.Skills {
		state, err := m.projection.State(skill, client)
		if err != nil || state == projection.StateConflict || state == projection.StateBroken || state == projection.StateIncompatibleEnabled {
			return "!", m.styles.issue
		}
		if !skill.Supports(client) {
			continue
		}
		compatible++
		if state == projection.StateEnabled {
			enabled++
		}
	}
	if compatible == 0 {
		return "—", m.styles.incompatible
	}
	value := fmt.Sprintf("%d/%d", enabled, compatible)
	if enabled == compatible {
		return value, m.styles.enabled
	}
	return value, m.styles.disabled
}

func (m Model) stateCell(skill catalog.Skill, client catalog.Client) (string, lipgloss.Style) {
	state, err := m.projection.State(skill, client)
	if err != nil {
		return "!", m.styles.issue
	}
	switch state {
	case projection.StateEnabled:
		return "[x]", m.styles.enabled
	case projection.StateDisabled:
		return "[ ]", m.styles.disabled
	case projection.StateIncompatible:
		return "—", m.styles.incompatible
	case projection.StateIncompatibleEnabled, projection.StateConflict:
		return "!", m.styles.issue
	case projection.StateBroken:
		return "×", m.styles.issue
	default:
		return "?", m.styles.issue
	}
}

func (m Model) renderDetail() string {
	switch m.tab {
	case tabMCP:
		return m.renderMCPDetail()
	case tabSystemPrompts:
		return m.renderPromptDetail()
	}
	selected, ok := m.selectedRow()
	if !ok {
		return m.styles.detail.Width(max(20, m.width-4)).Render(m.translator.Text(i18n.NoSelection))
	}
	source := m.catalog.Sources[selected.sourceIndex]
	lines := make([]string, 0, 3)
	if selected.kind == sourceRow {
		kind := m.translator.Text(i18n.LocalSource)
		if source.IsVendor() {
			kind = m.translator.Text(i18n.VendorBranch, source.Branch)
			if source.DiscoveryStrategy != "" {
				kind += " · " + m.translator.Text(i18n.DiscoveryLabel, source.DiscoveryStrategy)
			}
		}
		if source.IsArchived() {
			kind = m.translator.Text(i18n.ArchiveReference)
		}
		lines = append(lines, m.styles.accent.Render(source.ID)+"  "+m.styles.subtle.Render(kind))
		lines = append(lines, m.styles.subtle.Render(source.Path))
	} else {
		skill := source.Skills[selected.skillIndex]
		lines = append(lines, m.styles.accent.Render(skill.Name)+"  "+m.styles.subtle.Render(skill.ID))
		lines = append(lines, truncate(skill.Description, max(20, m.width-8)))
		targets := make([]string, 0, len(m.catalog.Clients.IDs()))
		for _, client := range m.catalog.Clients.IDs() {
			if skill.Supports(client) {
				targets = append(targets, string(client))
			}
		}
		compatibility := m.translator.Text(i18n.TargetsLabel, strings.Join(targets, ", "))
		if skill.CompatibilityReason != "" {
			compatibility += " · " + skill.CompatibilityReason
		}
		if skill.MetadataIssue != "" {
			compatibility += " · " + m.translator.Text(i18n.MetadataIssueLabel) + ": " + skill.MetadataIssue
		}
		lines = append(lines, m.styles.subtle.Render(compatibility))
	}
	return m.styles.detail.Width(max(20, m.width-4)).Render(strings.Join(lines, "\n"))
}

func (m Model) renderMCPDetail() string {
	names := m.mcpNames()
	if len(names) == 0 || m.cursor >= len(names) {
		return m.styles.detail.Width(max(20, m.width-4)).Render(m.translator.Text(i18n.NoSelection))
	}
	server, _ := m.mcpCatalog.Server(names[m.cursor])
	endpoint := server.Command
	if server.URL != "" {
		endpoint = server.URL
	}
	lines := []string{
		m.styles.accent.Render(server.Name) + "  " + m.styles.subtle.Render(string(server.Transport)),
		m.styles.subtle.Render(endpoint),
	}
	return m.styles.detail.Width(max(20, m.width-4)).Render(strings.Join(lines, "\n"))
}

func (m Model) renderPromptDetail() string {
	groups := m.promptGroups()
	if len(groups) == 0 || m.cursor >= len(groups) {
		return m.styles.detail.Width(max(20, m.width-4)).Render(m.translator.Text(i18n.NoSelection))
	}
	group := groups[m.cursor]
	lines := []string{
		m.styles.accent.Render(group.ID) + "  " + m.styles.subtle.Render(string(group.Client)),
		m.styles.subtle.Render(m.translator.Text(i18n.PromptFileSummary, len(group.Files), group.Path)),
	}
	return m.styles.detail.Width(max(20, m.width-4)).Render(strings.Join(lines, "\n"))
}

func (m Model) tabLabel(tab resourceTab) string {
	switch tab {
	case tabMCP:
		return m.translator.Text(i18n.TabMCP)
	case tabSystemPrompts:
		return m.translator.Text(i18n.TabSystemPrompts)
	default:
		return m.translator.Text(i18n.TabSkills)
	}
}

func (m Model) filterLabel(filter filterMode) string {
	switch filter {
	case filterAll:
		return m.translator.Text(i18n.FilterAll)
	case filterEnabled:
		return m.translator.Text(i18n.FilterEnabled)
	case filterIssues:
		return m.translator.Text(i18n.FilterIssues)
	case filterArchive:
		return m.translator.Text(i18n.FilterArchive)
	default:
		return filter.String()
	}
}

func (m Model) renderFooter() string {
	statusStyle := m.styles.status
	status := m.status
	if m.err != nil {
		statusStyle = m.styles.error
		status += ": " + m.err.Error()
	}
	if m.updating {
		status = "◌ " + status
	}
	return statusStyle.Render(status) + "\n" + m.styles.help.Render(m.help.View(m.keys))
}

func truncate(value string, width int) string {
	if width <= 0 {
		return ""
	}
	runes := []rune(value)
	if len(runes) <= width {
		return value
	}
	if width == 1 {
		return "…"
	}
	return string(runes[:width-1]) + "…"
}
