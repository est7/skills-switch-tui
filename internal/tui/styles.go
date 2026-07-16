package tui

import (
	"image/color"

	"charm.land/bubbles/v2/help"
	"charm.land/bubbles/v2/textinput"
	"charm.land/lipgloss/v2"
)

type styles struct {
	canvas       color.Color
	brandMark    lipgloss.Style
	title        lipgloss.Style
	subtitle     lipgloss.Style
	subtle       lipgloss.Style
	accent       lipgloss.Style
	scopeLabel   lipgloss.Style
	badge        lipgloss.Style
	tab          lipgloss.Style
	activeTab    lipgloss.Style
	filter       lipgloss.Style
	activeFilter lipgloss.Style
	panel        lipgloss.Style
	tableHeader  lipgloss.Style
	group        lipgloss.Style
	child        lipgloss.Style
	selected     lipgloss.Style
	selectedCell lipgloss.Style
	activeColumn lipgloss.Style
	enabled      lipgloss.Style
	disabled     lipgloss.Style
	incompatible lipgloss.Style
	issue        lipgloss.Style
	detail       lipgloss.Style
	statusBar    lipgloss.Style
	dangerBar    lipgloss.Style
	status       lipgloss.Style
	error        lipgloss.Style
	helpKey      lipgloss.Style
	helpDesc     lipgloss.Style
	helpSep      lipgloss.Style
}

func newStyles(isDark bool) styles {
	choose := lipgloss.LightDark(isDark)
	canvas := choose(lipgloss.Color("#F2F5F6"), lipgloss.Color("#182126"))
	foreground := choose(lipgloss.Color("#18272E"), lipgloss.Color("#F1F5F6"))
	muted := choose(lipgloss.Color("#50626B"), lipgloss.Color("#B4C0C5"))
	accent := choose(lipgloss.Color("#006D65"), lipgloss.Color("#5EE0D2"))
	accentStrong := choose(lipgloss.Color("#006D65"), lipgloss.Color("#087F76"))
	accentContrast := lipgloss.Color("#FFFFFF")
	accentSoft := choose(lipgloss.Color("#D4ECE9"), lipgloss.Color("#21413E"))
	panel := canvas
	panelRaised := choose(lipgloss.Color("#E2E8EB"), lipgloss.Color("#263238"))
	border := choose(lipgloss.Color("#8799A2"), lipgloss.Color("#62737B"))
	green := choose(lipgloss.Color("#08703B"), lipgloss.Color("#88E8B5"))
	red := choose(lipgloss.Color("#B4233A"), lipgloss.Color("#FFABB6"))
	redSoft := choose(lipgloss.Color("#F6DDE1"), lipgloss.Color("#482B32"))

	return styles{
		canvas:       canvas,
		brandMark:    lipgloss.NewStyle().Foreground(accent).Background(canvas).Bold(true),
		title:        lipgloss.NewStyle().Foreground(foreground).Background(canvas).Bold(true),
		subtitle:     lipgloss.NewStyle().Foreground(muted).Background(canvas),
		subtle:       lipgloss.NewStyle().Foreground(muted).Background(canvas),
		accent:       lipgloss.NewStyle().Foreground(accent).Background(canvas).Bold(true),
		scopeLabel:   lipgloss.NewStyle().Foreground(accent).Background(canvas).Bold(true),
		badge:        lipgloss.NewStyle().Foreground(accent).Background(accentSoft).Bold(true).Padding(0, 1),
		tab:          lipgloss.NewStyle().Foreground(muted).Background(canvas).Padding(0, 1),
		activeTab:    lipgloss.NewStyle().Foreground(accent).Background(accentSoft).Bold(true).Padding(0, 1),
		filter:       lipgloss.NewStyle().Foreground(muted).Background(canvas).Padding(0, 1),
		activeFilter: lipgloss.NewStyle().Foreground(accent).Background(accentSoft).Bold(true).Padding(0, 1),
		panel:        lipgloss.NewStyle().Background(panel).Border(lipgloss.RoundedBorder()).BorderForeground(border),
		tableHeader:  lipgloss.NewStyle().Foreground(muted).Background(panelRaised).Bold(true),
		group:        lipgloss.NewStyle().Foreground(foreground).Background(canvas).Bold(true),
		child:        lipgloss.NewStyle().Foreground(foreground).Background(canvas),
		selected:     lipgloss.NewStyle().Foreground(foreground).Background(accentSoft),
		selectedCell: lipgloss.NewStyle().Foreground(accentContrast).Background(accentStrong).Bold(true),
		activeColumn: lipgloss.NewStyle().Foreground(accent).Background(panelRaised).Bold(true),
		enabled:      lipgloss.NewStyle().Foreground(green).Background(canvas).Bold(true),
		disabled:     lipgloss.NewStyle().Foreground(muted).Background(canvas),
		incompatible: lipgloss.NewStyle().Foreground(muted).Background(canvas),
		issue:        lipgloss.NewStyle().Foreground(red).Background(canvas).Bold(true),
		detail: lipgloss.NewStyle().Foreground(foreground).Background(canvas).
			Border(lipgloss.ThickBorder(), false, false, false, true).
			BorderForeground(accent).
			PaddingLeft(1),
		statusBar: lipgloss.NewStyle().Background(panelRaised).Padding(0, 1),
		dangerBar: lipgloss.NewStyle().Background(redSoft).Padding(0, 1),
		status:    lipgloss.NewStyle().Foreground(foreground).Background(panelRaised),
		error:     lipgloss.NewStyle().Foreground(red).Background(redSoft).Bold(true),
		helpKey:   lipgloss.NewStyle().Foreground(accent).Background(canvas).Bold(true),
		helpDesc:  lipgloss.NewStyle().Foreground(muted).Background(canvas),
		helpSep:   lipgloss.NewStyle().Foreground(border).Background(canvas),
	}
}

func applySearchStyles(model *textinput.Model, theme styles, isDark bool) {
	inputStyles := textinput.DefaultStyles(isDark)
	inputStyles.Focused.Text = lipgloss.NewStyle().Foreground(theme.title.GetForeground()).Background(theme.activeFilter.GetBackground())
	inputStyles.Focused.Placeholder = lipgloss.NewStyle().Foreground(theme.subtle.GetForeground()).Background(theme.activeFilter.GetBackground())
	inputStyles.Focused.Suggestion = inputStyles.Focused.Placeholder
	inputStyles.Focused.Prompt = lipgloss.NewStyle().Foreground(theme.accent.GetForeground()).Background(theme.activeFilter.GetBackground()).Bold(true)
	inputStyles.Blurred.Text = lipgloss.NewStyle().Foreground(theme.title.GetForeground()).Background(theme.canvas)
	inputStyles.Blurred.Placeholder = lipgloss.NewStyle().Foreground(theme.subtle.GetForeground()).Background(theme.canvas)
	inputStyles.Blurred.Suggestion = inputStyles.Blurred.Placeholder
	inputStyles.Blurred.Prompt = lipgloss.NewStyle().Foreground(theme.accent.GetForeground()).Background(theme.canvas).Bold(true)
	inputStyles.Cursor.Color = theme.accent.GetForeground()
	model.SetStyles(inputStyles)
}

func applyHelpStyles(model *help.Model, theme styles) {
	model.Styles.ShortKey = theme.helpKey
	model.Styles.ShortDesc = theme.helpDesc
	model.Styles.ShortSeparator = theme.helpSep
	model.Styles.FullKey = theme.helpKey
	model.Styles.FullDesc = theme.helpDesc
	model.Styles.FullSeparator = theme.helpSep
	model.Styles.Ellipsis = theme.helpDesc
}
