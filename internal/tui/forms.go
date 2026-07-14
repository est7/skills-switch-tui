package tui

import (
	"errors"
	"regexp"
	"strings"

	tea "charm.land/bubbletea/v2"
	huh "charm.land/huh/v2"
	"charm.land/lipgloss/v2"
	"github.com/est7/skills-switch-tui/internal/catalog"
	"github.com/est7/skills-switch-tui/internal/i18n"
	"github.com/est7/skills-switch-tui/internal/mcp"
	"github.com/est7/skills-switch-tui/internal/source"
)

var localSkillNamePattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]*$`)

type formKind int

const (
	formAddMenu formKind = iota
	formAddRepo
	formCreateSkill
	formAddMCP
	formAddMCPName
)

// activeForm is the huh dialog currently overlaying the table. Its field
// pointers are bound into the huh form so completed values land here.
type activeForm struct {
	kind    formKind
	form    *huh.Form
	choice  string
	url     string
	name    string
	desc    string
	json    string
	servers []mcp.Server
}

// addFinishedMsg reports the result of an asynchronous vendor source add.
type addFinishedMsg struct {
	label string
	err   error
}

// startAdd opens the context-appropriate "new" dialog for the active tab.
func (m Model) startAdd() (tea.Model, tea.Cmd) {
	if m.updating || m.deleting {
		return m, nil
	}
	switch m.tab {
	case tabMCP:
		return m.openMCPForm()
	case tabSkills:
		return m.openAddMenu()
	default:
		m.status = m.translator.Text(i18n.AddUnavailable)
		return m, nil
	}
}

func (m Model) startForm(active *activeForm, form *huh.Form) (tea.Model, tea.Cmd) {
	form = form.
		WithWidth(max(20, m.modalInnerWidth())).
		WithShowHelp(true).
		WithShowErrors(true).
		WithTheme(m.huhTheme())
	active.form = form
	m.active = active
	m.err = nil
	return m, form.Init()
}

// huhTheme maps the app's Lip Gloss palette onto huh's form styles so dialogs
// share the same accent, foreground, and muted colors as the rest of the TUI.
func (m Model) huhTheme() huh.Theme {
	accent := m.styles.accent.GetForeground()
	foreground := m.styles.title.GetForeground()
	muted := m.styles.subtle.GetForeground()
	danger := m.styles.issue.GetForeground()
	buttonForeground := m.styles.selectedCell.GetForeground()
	buttonBackground := m.styles.selectedCell.GetBackground()
	isDark := m.isDark
	return huh.ThemeFunc(func(bool) *huh.Styles {
		styles := huh.ThemeBase(isDark)
		focused := &styles.Focused
		focused.Base = focused.Base.BorderForeground(accent)
		focused.Title = focused.Title.Foreground(accent).Bold(true)
		focused.Description = focused.Description.Foreground(muted)
		focused.SelectSelector = focused.SelectSelector.Foreground(accent)
		focused.Option = focused.Option.Foreground(foreground)
		focused.SelectedOption = focused.SelectedOption.Foreground(accent).Bold(true)
		focused.NextIndicator = focused.NextIndicator.Foreground(accent)
		focused.PrevIndicator = focused.PrevIndicator.Foreground(accent)
		focused.ErrorIndicator = focused.ErrorIndicator.Foreground(danger)
		focused.ErrorMessage = focused.ErrorMessage.Foreground(danger)
		focused.FocusedButton = focused.FocusedButton.Foreground(buttonForeground).Background(buttonBackground)
		focused.BlurredButton = focused.BlurredButton.Foreground(muted)
		focused.TextInput.Cursor = focused.TextInput.Cursor.Foreground(accent)
		focused.TextInput.Prompt = focused.TextInput.Prompt.Foreground(accent)
		focused.TextInput.Placeholder = focused.TextInput.Placeholder.Foreground(muted)
		focused.TextInput.Text = focused.TextInput.Text.Foreground(foreground)
		styles.Group.Title = styles.Group.Title.Foreground(accent).Bold(true)
		styles.Group.Description = styles.Group.Description.Foreground(muted)
		blurred := *focused
		blurred.Base = blurred.Base.BorderStyle(lipgloss.HiddenBorder())
		blurred.Title = blurred.Title.Foreground(muted).Bold(false)
		styles.Blurred = blurred
		return styles
	})
}

func (m Model) openAddMenu() (tea.Model, tea.Cmd) {
	active := &activeForm{kind: formAddMenu}
	form := huh.NewForm(huh.NewGroup(
		huh.NewSelect[string]().
			Title(m.translator.Text(i18n.AddMenuTitle)).
			Options(
				huh.NewOption(m.translator.Text(i18n.AddMenuRepo), "repo"),
				huh.NewOption(m.translator.Text(i18n.AddMenuLocal), "create"),
			).
			Value(&active.choice),
	))
	return m.startForm(active, form)
}

func (m Model) openRepoForm() (tea.Model, tea.Cmd) {
	if m.updater == nil {
		m.status = m.translator.Text(i18n.AddRepoUnavailable)
		return m, nil
	}
	active := &activeForm{kind: formAddRepo}
	form := huh.NewForm(huh.NewGroup(
		huh.NewInput().
			Title(m.translator.Text(i18n.AddRepoTitle)).
			Placeholder("https://github.com/owner/repo").
			Value(&active.url).
			Validate(func(value string) error {
				if strings.TrimSpace(value) == "" {
					return errors.New(m.translator.Text(i18n.AddRepoRequired))
				}
				_, err := source.ParseSourceRef(value)
				return err
			}),
	))
	return m.startForm(active, form)
}

func (m Model) openCreateForm() (tea.Model, tea.Cmd) {
	active := &activeForm{kind: formCreateSkill}
	form := huh.NewForm(huh.NewGroup(
		huh.NewInput().
			Title(m.translator.Text(i18n.CreateSkillNameTitle)).
			Value(&active.name).
			Validate(func(value string) error {
				if !localSkillNamePattern.MatchString(strings.TrimSpace(value)) {
					return errors.New(m.translator.Text(i18n.CreateSkillNameInvalid))
				}
				return nil
			}),
		huh.NewInput().
			Title(m.translator.Text(i18n.CreateSkillDescTitle)).
			Value(&active.desc),
	))
	return m.startForm(active, form)
}

func (m Model) openMCPForm() (tea.Model, tea.Cmd) {
	if m.mcpCatalog.Path == "" {
		m.status = m.translator.Text(i18n.AddMCPUnavailable)
		return m, nil
	}
	active := &activeForm{kind: formAddMCP}
	form := huh.NewForm(huh.NewGroup(
		huh.NewText().
			Title(m.translator.Text(i18n.MCPFormTitle)).
			Placeholder(`{"mcpServers":{"name":{"command":"npx"}}}`).
			Lines(3).
			Value(&active.json).
			Validate(func(value string) error {
				_, err := mcp.ParseServers([]byte(value))
				return err
			}),
	))
	return m.startForm(active, form)
}

func (m Model) openMCPNameForm(servers []mcp.Server) (tea.Model, tea.Cmd) {
	active := &activeForm{kind: formAddMCPName, servers: servers}
	form := huh.NewForm(huh.NewGroup(
		huh.NewInput().
			Title(m.translator.Text(i18n.MCPNamePromptTitle)).
			Value(&active.name).
			Validate(func(value string) error {
				value = strings.TrimSpace(value)
				if value == "" {
					return errors.New(m.translator.Text(i18n.MCPNameRequired))
				}
				if _, exists := m.mcpCatalog.Server(value); exists {
					return errors.New(m.translator.Text(i18n.MCPServerExists, value))
				}
				return nil
			}),
	))
	return m.startForm(active, form)
}

// updateForm forwards every message to the active huh form and reacts once the
// user completes or aborts it.
func (m Model) updateForm(message tea.Msg) (tea.Model, tea.Cmd) {
	model, command := m.active.form.Update(message)
	if form, ok := model.(*huh.Form); ok {
		m.active.form = form
	}
	switch m.active.form.State {
	case huh.StateCompleted:
		return m.completeForm()
	case huh.StateAborted:
		m.active = nil
		m.status = m.translator.Text(i18n.Ready)
		return m, nil
	}
	return m, command
}

func (m Model) completeForm() (tea.Model, tea.Cmd) {
	active := m.active
	switch active.kind {
	case formAddMenu:
		m.active = nil
		switch active.choice {
		case "repo":
			return m.openRepoForm()
		case "create":
			return m.openCreateForm()
		}
		return m, nil
	case formAddRepo:
		return m.commitRepo(active.url)
	case formCreateSkill:
		return m.commitCreateSkill(active.name, active.desc)
	case formAddMCP:
		return m.commitMCPJSON(active.json)
	case formAddMCPName:
		return m.commitMCPNamed(active.servers, active.name)
	}
	m.active = nil
	return m, nil
}

func (m Model) commitRepo(raw string) (tea.Model, tea.Cmd) {
	m.active = nil
	ref, err := source.ParseSourceRef(raw)
	if err != nil {
		m.err = err
		m.status = m.translator.Text(i18n.NoChangesApplied)
		return m, nil
	}
	if m.updater == nil {
		m.status = m.translator.Text(i18n.AddRepoUnavailable)
		return m, nil
	}
	request := source.AddRequest{Name: ref.Name, URL: ref.CloneURL, Branch: ref.Branch, Scope: "shared"}
	if ref.Subpath != "" {
		request.SkillPaths = []string{ref.Subpath}
	}
	m.updating = true
	m.err = nil
	m.status = m.translator.Text(i18n.AddingRepo, ref.Name)
	updater := *m.updater
	ctx := m.context
	return m, func() tea.Msg {
		return addFinishedMsg{label: ref.Name, err: updater.Add(ctx, request)}
	}
}

func (m Model) commitCreateSkill(name, description string) (tea.Model, tea.Cmd) {
	m.active = nil
	skillDir, err := catalog.ScaffoldLocalSkill(m.catalog.Root, "shared", "", strings.TrimSpace(name), strings.TrimSpace(description))
	if err != nil {
		m.err = err
		m.status = m.translator.Text(i18n.NoChangesApplied)
		return m, nil
	}
	if err := m.reloadCatalog(); err != nil {
		m.err = err
		m.status = m.translator.Text(i18n.UpdateReloadFailed)
		return m, nil
	}
	m.err = nil
	m.status = m.translator.Text(i18n.SkillCreated, skillDir)
	return m, nil
}

func (m Model) commitMCPJSON(jsonText string) (tea.Model, tea.Cmd) {
	servers, err := mcp.ParseServers([]byte(jsonText))
	if err != nil {
		m.active = nil
		m.err = err
		m.status = m.translator.Text(i18n.NoChangesApplied)
		return m, nil
	}
	for _, server := range servers {
		if server.Name == "" {
			return m.openMCPNameForm(servers)
		}
	}
	m.active = nil
	return m.addMCPServers(servers)
}

func (m Model) commitMCPNamed(servers []mcp.Server, name string) (tea.Model, tea.Cmd) {
	m.active = nil
	name = strings.TrimSpace(name)
	for index := range servers {
		if servers[index].Name == "" {
			servers[index].Name = name
		}
	}
	return m.addMCPServers(servers)
}

func (m Model) addMCPServers(servers []mcp.Server) (tea.Model, tea.Cmd) {
	// Preflight names so a mid-list conflict never leaves a partial import.
	for _, server := range servers {
		if _, exists := m.mcpCatalog.Server(server.Name); exists {
			m.err = errors.New(m.translator.Text(i18n.MCPServerExists, server.Name))
			m.status = m.translator.Text(i18n.NoChangesApplied)
			return m, nil
		}
	}
	for _, server := range servers {
		if err := mcp.AddServer(m.mcpCatalog.Path, server); err != nil {
			m.err = err
			m.status = m.translator.Text(i18n.NoChangesApplied)
			return m, nil
		}
	}
	if err := m.reloadCatalog(); err != nil {
		m.err = err
		m.status = m.translator.Text(i18n.UpdateReloadFailed)
		return m, nil
	}
	m.err = nil
	if len(servers) == 1 {
		m.status = m.translator.Text(i18n.MCPServerAdded, servers[0].Name)
	} else {
		m.status = m.translator.Text(i18n.MCPServersAdded, len(servers))
	}
	return m, nil
}
