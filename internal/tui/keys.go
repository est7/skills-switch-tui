package tui

import (
	"charm.land/bubbles/v2/key"
	"github.com/est7/skills-switch-tui/internal/i18n"
)

type keyMap struct {
	Navigate key.Binding
	Client   key.Binding
	Toggle   key.Binding
	Expand   key.Binding
	Search   key.Binding
	Filter   key.Binding
	Update   key.Binding
	Help     key.Binding
	Quit     key.Binding
}

func defaultKeyMap(translator i18n.Translator) keyMap {
	return keyMap{
		Navigate: key.NewBinding(key.WithKeys("up", "down", "j", "k"), key.WithHelp("↑/↓", translator.Text(i18n.HelpNavigate))),
		Client:   key.NewBinding(key.WithKeys("left", "right", "h", "l"), key.WithHelp("←/→", translator.Text(i18n.HelpClient))),
		Toggle:   key.NewBinding(key.WithKeys(" "), key.WithHelp("space", translator.Text(i18n.HelpToggle))),
		Expand:   key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", translator.Text(i18n.HelpExpand))),
		Search:   key.NewBinding(key.WithKeys("/"), key.WithHelp("/", translator.Text(i18n.HelpSearch))),
		Filter:   key.NewBinding(key.WithKeys("tab"), key.WithHelp("tab", translator.Text(i18n.HelpFilter))),
		Update:   key.NewBinding(key.WithKeys("u"), key.WithHelp("u", translator.Text(i18n.HelpUpdate))),
		Help:     key.NewBinding(key.WithKeys("?"), key.WithHelp("?", translator.Text(i18n.HelpMore))),
		Quit:     key.NewBinding(key.WithKeys("q", "ctrl+c"), key.WithHelp("q", translator.Text(i18n.HelpQuit))),
	}
}

func (k keyMap) ShortHelp() []key.Binding {
	return []key.Binding{k.Navigate, k.Client, k.Toggle, k.Search, k.Help, k.Quit}
}

func (k keyMap) FullHelp() [][]key.Binding {
	return [][]key.Binding{
		{k.Navigate, k.Client, k.Toggle, k.Expand},
		{k.Search, k.Filter, k.Update},
		{k.Help, k.Quit},
	}
}
