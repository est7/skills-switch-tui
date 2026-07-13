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
	canvas := choose(lipgloss.Color("#F6F3FA"), lipgloss.Color("#100D17"))
	foreground := choose(lipgloss.Color("#201D29"), lipgloss.Color("#F6F3FA"))
	muted := choose(lipgloss.Color("#625D6B"), lipgloss.Color("#B4ADBF"))
	accent := choose(lipgloss.Color("#5B34C4"), lipgloss.Color("#C4B0FF"))
	accentStrong := choose(lipgloss.Color("#5B32B4"), lipgloss.Color("#7048C8"))
	accentContrast := lipgloss.Color("#FFFFFF")
	accentSoft := choose(lipgloss.Color("#ECE5FF"), lipgloss.Color("#322746"))
	panel := choose(lipgloss.Color("#FCFBFE"), lipgloss.Color("#18151F"))
	panelRaised := choose(lipgloss.Color("#F0ECF5"), lipgloss.Color("#27222F"))
	border := choose(lipgloss.Color("#BEB6C8"), lipgloss.Color("#5A5065"))
	green := choose(lipgloss.Color("#08783F"), lipgloss.Color("#6DE0A5"))
	red := choose(lipgloss.Color("#B4233A"), lipgloss.Color("#FF8999"))

	return styles{
		canvas:       canvas,
		brandMark:    lipgloss.NewStyle().Foreground(accent).Bold(true),
		title:        lipgloss.NewStyle().Foreground(foreground).Bold(true),
		subtitle:     lipgloss.NewStyle().Foreground(muted),
		subtle:       lipgloss.NewStyle().Foreground(muted),
		accent:       lipgloss.NewStyle().Foreground(accent).Bold(true),
		scopeLabel:   lipgloss.NewStyle().Foreground(accent).Bold(true),
		tab:          lipgloss.NewStyle().Foreground(muted).Background(panelRaised).Padding(0, 1),
		activeTab:    lipgloss.NewStyle().Foreground(accentContrast).Background(accentStrong).Bold(true).Padding(0, 1),
		filter:       lipgloss.NewStyle().Foreground(muted).Padding(0, 1),
		activeFilter: lipgloss.NewStyle().Foreground(accent).Background(accentSoft).Bold(true).Padding(0, 1),
		panel:        lipgloss.NewStyle().Background(panel).Border(lipgloss.RoundedBorder()).BorderForeground(border),
		tableHeader:  lipgloss.NewStyle().Foreground(muted).Background(panelRaised).Bold(true),
		group:        lipgloss.NewStyle().Foreground(foreground).Bold(true),
		child:        lipgloss.NewStyle().Foreground(foreground),
		selected:     lipgloss.NewStyle().Foreground(foreground).Background(accentSoft),
		selectedCell: lipgloss.NewStyle().Foreground(accentContrast).Background(accentStrong).Bold(true),
		enabled:      lipgloss.NewStyle().Foreground(green).Bold(true),
		disabled:     lipgloss.NewStyle().Foreground(muted),
		incompatible: lipgloss.NewStyle().Foreground(muted),
		issue:        lipgloss.NewStyle().Foreground(red).Bold(true),
		detail: lipgloss.NewStyle().Foreground(foreground).
			Border(lipgloss.ThickBorder(), false, false, false, true).
			BorderForeground(accent).
			PaddingLeft(1),
		statusBar: lipgloss.NewStyle().Background(panelRaised).Padding(0, 1),
		status:    lipgloss.NewStyle().Foreground(foreground),
		error:     lipgloss.NewStyle().Foreground(red).Bold(true),
		helpKey:   lipgloss.NewStyle().Foreground(accent).Bold(true),
		helpDesc:  lipgloss.NewStyle().Foreground(muted),
		helpSep:   lipgloss.NewStyle().Foreground(border),
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
