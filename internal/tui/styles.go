package tui

import (
	"charm.land/lipgloss/v2"
)

type styles struct {
	title        lipgloss.Style
	subtle       lipgloss.Style
	accent       lipgloss.Style
	filter       lipgloss.Style
	activeFilter lipgloss.Style
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
	status       lipgloss.Style
	error        lipgloss.Style
	help         lipgloss.Style
}

func newStyles(isDark bool) styles {
	choose := lipgloss.LightDark(isDark)
	foreground := choose(lipgloss.Color("#20212B"), lipgloss.Color("#E7E5F2"))
	muted := choose(lipgloss.Color("#6F7080"), lipgloss.Color("#8B8A9C"))
	accent := choose(lipgloss.Color("#5B4FD8"), lipgloss.Color("#B4A0FF"))
	accentSoft := choose(lipgloss.Color("#ECE9FF"), lipgloss.Color("#2C2647"))
	border := choose(lipgloss.Color("#D8D5E2"), lipgloss.Color("#3B3947"))
	green := choose(lipgloss.Color("#177245"), lipgloss.Color("#63D69A"))
	red := choose(lipgloss.Color("#B42318"), lipgloss.Color("#FF8A80"))

	return styles{
		title:        lipgloss.NewStyle().Bold(true).Foreground(accent),
		subtle:       lipgloss.NewStyle().Foreground(muted),
		accent:       lipgloss.NewStyle().Foreground(accent).Bold(true),
		filter:       lipgloss.NewStyle().Foreground(muted).Padding(0, 1),
		activeFilter: lipgloss.NewStyle().Foreground(accent).Background(accentSoft).Bold(true).Padding(0, 1),
		tableHeader:  lipgloss.NewStyle().Foreground(muted).Bold(true).BorderBottom(true).BorderForeground(border),
		group:        lipgloss.NewStyle().Foreground(foreground).Bold(true),
		child:        lipgloss.NewStyle().Foreground(foreground),
		selected:     lipgloss.NewStyle().Foreground(foreground).Background(accentSoft),
		selectedCell: lipgloss.NewStyle().Foreground(accent).Background(accentSoft).Bold(true),
		enabled:      lipgloss.NewStyle().Foreground(green).Bold(true),
		disabled:     lipgloss.NewStyle().Foreground(muted),
		incompatible: lipgloss.NewStyle().Foreground(muted).Faint(true),
		issue:        lipgloss.NewStyle().Foreground(red).Bold(true),
		detail:       lipgloss.NewStyle().Foreground(foreground).BorderTop(true).BorderForeground(border).PaddingTop(1),
		status:       lipgloss.NewStyle().Foreground(muted),
		error:        lipgloss.NewStyle().Foreground(red),
		help:         lipgloss.NewStyle().Foreground(muted),
	}
}
