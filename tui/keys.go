package tui

import "github.com/charmbracelet/bubbles/key"

type keymap struct {
	NextTab key.Binding
	PrevTab key.Binding
	Up      key.Binding
	Down    key.Binding
	Enter   key.Binding
	Search  key.Binding
	Refresh key.Binding
	Help    key.Binding
	Quit    key.Binding
	Group   key.Binding
	Stats   key.Binding
	OpenDir key.Binding
}

var keys = keymap{
	NextTab: key.NewBinding(key.WithKeys("tab"), key.WithHelp("tab", "next tab")),
	PrevTab: key.NewBinding(key.WithKeys("shift+tab"), key.WithHelp("shift+tab", "prev tab")),
	Up:      key.NewBinding(key.WithKeys("k", "up"), key.WithHelp("k", "up")),
	Down:    key.NewBinding(key.WithKeys("j", "down"), key.WithHelp("j", "down")),
	Enter:   key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", "resume")),
	Search:  key.NewBinding(key.WithKeys("/", "f"), key.WithHelp("/", "search")),
	Refresh: key.NewBinding(key.WithKeys("r"), key.WithHelp("r", "refresh")),
	Help:    key.NewBinding(key.WithKeys("?"), key.WithHelp("?", "help")),
	Quit:    key.NewBinding(key.WithKeys("q", "esc"), key.WithHelp("q", "quit")),
	Group:   key.NewBinding(key.WithKeys("g"), key.WithHelp("g", "group toggle")),
	Stats:   key.NewBinding(key.WithKeys("s"), key.WithHelp("s", "stats toggle")),
	OpenDir: key.NewBinding(key.WithKeys("ctrl+o"), key.WithHelp("ctrl+o", "open dir")),
}

func keyMatches(msgKey string, b key.Binding) bool {
	for _, k := range b.Keys() {
		if msgKey == k {
			return true
		}
	}
	return false
}
