package tui

import (
	"image/color"

	"charm.land/bubbles/v2/help"
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
	enabled      lipgloss.Style
	disabled     lipgloss.Style
	incompatible lipgloss.Style
	issue        lipgloss.Style
	detail       lipgloss.Style
	statusBar    lipgloss.Style
	status       lipgloss.Style
	error        lipgloss.Style
	helpKey      lipgloss.Style
	helpDesc     lipgloss.Style
	helpSep      lipgloss.Style
}

func newStyles(isDark bool) styles {
	choose := lipgloss.LightDark(isDark)
	canvas := choose(lipgloss.Color("#F5F7F8"), lipgloss.Color("#0D1117"))
	foreground := choose(lipgloss.Color("#1F2933"), lipgloss.Color("#E6EDF3"))
	muted := choose(lipgloss.Color("#566574"), lipgloss.Color("#A7B2BE"))
	accent := choose(lipgloss.Color("#0B746E"), lipgloss.Color("#5ED6C9"))
	accentStrong := choose(lipgloss.Color("#0B746E"), lipgloss.Color("#117C74"))
	accentContrast := lipgloss.Color("#FFFFFF")
	accentSoft := choose(lipgloss.Color("#DFF4F1"), lipgloss.Color("#123B38"))
	panel := canvas
	panelRaised := choose(lipgloss.Color("#E7EDF0"), lipgloss.Color("#1A232D"))
	border := choose(lipgloss.Color("#A9B6C0"), lipgloss.Color("#3D4D5C"))
	green := choose(lipgloss.Color("#08783F"), lipgloss.Color("#6DE0A5"))
	red := choose(lipgloss.Color("#B4233A"), lipgloss.Color("#FF8999"))

	return styles{
		canvas:       canvas,
		brandMark:    lipgloss.NewStyle().Foreground(accent).Background(canvas).Bold(true),
		title:        lipgloss.NewStyle().Foreground(foreground).Background(canvas).Bold(true),
		subtitle:     lipgloss.NewStyle().Foreground(muted).Background(canvas),
		subtle:       lipgloss.NewStyle().Foreground(muted).Background(canvas),
		accent:       lipgloss.NewStyle().Foreground(accent).Background(canvas).Bold(true),
		scopeLabel:   lipgloss.NewStyle().Foreground(accent).Background(canvas).Bold(true),
		tab:          lipgloss.NewStyle().Foreground(muted).Background(panelRaised).Padding(0, 1),
		activeTab:    lipgloss.NewStyle().Foreground(accentContrast).Background(accentStrong).Bold(true).Padding(0, 1),
		filter:       lipgloss.NewStyle().Foreground(muted).Background(canvas).Padding(0, 1),
		activeFilter: lipgloss.NewStyle().Foreground(accent).Background(accentSoft).Bold(true).Padding(0, 1),
		panel:        lipgloss.NewStyle().Background(panel).Border(lipgloss.RoundedBorder()).BorderForeground(border),
		tableHeader:  lipgloss.NewStyle().Foreground(muted).Background(panelRaised).Bold(true),
		group:        lipgloss.NewStyle().Foreground(foreground).Background(canvas).Bold(true),
		child:        lipgloss.NewStyle().Foreground(foreground).Background(canvas),
		selected:     lipgloss.NewStyle().Foreground(foreground).Background(accentSoft),
		selectedCell: lipgloss.NewStyle().Foreground(accentContrast).Background(accentStrong).Bold(true),
		enabled:      lipgloss.NewStyle().Foreground(green).Background(canvas).Bold(true),
		disabled:     lipgloss.NewStyle().Foreground(muted).Background(canvas),
		incompatible: lipgloss.NewStyle().Foreground(muted).Background(canvas),
		issue:        lipgloss.NewStyle().Foreground(red).Background(canvas).Bold(true),
		detail: lipgloss.NewStyle().Foreground(foreground).Background(canvas).
			Border(lipgloss.ThickBorder(), false, false, false, true).
			BorderForeground(accent).
			PaddingLeft(1),
		statusBar: lipgloss.NewStyle().Background(panelRaised).Padding(0, 1),
		status:    lipgloss.NewStyle().Foreground(foreground).Background(panelRaised),
		error:     lipgloss.NewStyle().Foreground(red).Background(panelRaised).Bold(true),
		helpKey:   lipgloss.NewStyle().Foreground(accent).Background(canvas).Bold(true),
		helpDesc:  lipgloss.NewStyle().Foreground(muted).Background(canvas),
		helpSep:   lipgloss.NewStyle().Foreground(border).Background(canvas),
	}
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
