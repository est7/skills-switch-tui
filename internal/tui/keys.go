package tui

import (
	"charm.land/bubbles/v2/key"
	"github.com/est7/skills-switch-tui/internal/i18n"
)

type keyMap struct {
	Navigate  key.Binding
	Resource  key.Binding
	Client    key.Binding
	Toggle    key.Binding
	ToggleAll key.Binding
	Expand    key.Binding
	Search    key.Binding
	Filter    key.Binding
	Update    key.Binding
	UpdateAll key.Binding
	Delete    key.Binding
	AddMCP    key.Binding
	Language  key.Binding
	Help      key.Binding
	Quit      key.Binding
}

func defaultKeyMap(translator i18n.Translator) keyMap {
	return keyMap{
		Navigate:  key.NewBinding(key.WithKeys("up", "down", "j", "k"), key.WithHelp("↑/↓", translator.Text(i18n.HelpNavigate))),
		Resource:  key.NewBinding(key.WithKeys("tab", "shift+tab"), key.WithHelp("tab", translator.Text(i18n.HelpResource))),
		Client:    key.NewBinding(key.WithKeys("left", "right", "h", "l"), key.WithHelp("←/→", translator.Text(i18n.HelpClient))),
		Toggle:    key.NewBinding(key.WithKeys(" "), key.WithHelp("space", translator.Text(i18n.HelpToggle))),
		ToggleAll: key.NewBinding(key.WithKeys("a"), key.WithHelp("a", translator.Text(i18n.HelpToggleAll))),
		Expand:    key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", translator.Text(i18n.HelpExpand))),
		Search:    key.NewBinding(key.WithKeys("/"), key.WithHelp("/", translator.Text(i18n.HelpSearch))),
		Filter:    key.NewBinding(key.WithKeys("f"), key.WithHelp("f", translator.Text(i18n.HelpFilter))),
		Update:    key.NewBinding(key.WithKeys("u"), key.WithHelp("u", translator.Text(i18n.HelpUpdate))),
		UpdateAll: key.NewBinding(key.WithKeys("U"), key.WithHelp("U", translator.Text(i18n.HelpUpdateAll))),
		Delete:    key.NewBinding(key.WithKeys("d"), key.WithHelp("d", translator.Text(i18n.HelpDelete))),
		AddMCP:    key.NewBinding(key.WithKeys("n"), key.WithHelp("n", translator.Text(i18n.HelpAddMCP))),
		Language:  key.NewBinding(key.WithKeys("L"), key.WithHelp("L", translator.Text(i18n.HelpLanguage))),
		Help:      key.NewBinding(key.WithKeys("?"), key.WithHelp("?", translator.Text(i18n.HelpMore))),
		Quit:      key.NewBinding(key.WithKeys("q", "ctrl+c"), key.WithHelp("q", translator.Text(i18n.HelpQuit))),
	}
}

func (k keyMap) ShortHelp() []key.Binding {
	return []key.Binding{k.Toggle, k.ToggleAll, k.Navigate, k.Client, k.Resource, k.Search, k.Help, k.Quit}
}

func (k keyMap) FullHelp() [][]key.Binding {
	return [][]key.Binding{
		{k.Navigate, k.Resource, k.Client, k.Toggle, k.ToggleAll, k.Expand},
		{k.Search, k.Filter, k.Update, k.UpdateAll, k.Delete, k.AddMCP, k.Language},
		{k.Help, k.Quit},
	}
}
