package tui

import "github.com/charmbracelet/bubbles/key"

type keymap struct {
	NextTab key.Binding
	PrevTab key.Binding
	Up      key.Binding
	Down    key.Binding
	Top     key.Binding
	Bottom  key.Binding
	PageUp  key.Binding
	PageDn  key.Binding
	Enter   key.Binding
	Search  key.Binding
	Next    key.Binding
	Prev    key.Binding
	Refresh key.Binding
	Help    key.Binding
	Quit    key.Binding
	Group   key.Binding
	Stats   key.Binding
	OpenDir key.Binding
	Export  key.Binding
	Tab1    key.Binding
	Tab2    key.Binding
	Tab3    key.Binding
	Tab4    key.Binding
	Tab5    key.Binding
	Tab6    key.Binding
	Tab7    key.Binding
	Tab8    key.Binding
}

var keys = keymap{
	NextTab: key.NewBinding(key.WithKeys("tab"), key.WithHelp("tab", "next tab")),
	PrevTab: key.NewBinding(key.WithKeys("shift+tab"), key.WithHelp("shift+tab", "prev tab")),
	Up:      key.NewBinding(key.WithKeys("k", "up"), key.WithHelp("k", "up")),
	Down:    key.NewBinding(key.WithKeys("j", "down"), key.WithHelp("j", "down")),
	Top:     key.NewBinding(key.WithKeys("home"), key.WithHelp("gg/home", "top")),
	Bottom:  key.NewBinding(key.WithKeys("G", "end"), key.WithHelp("G/end", "bottom")),
	PageUp:  key.NewBinding(key.WithKeys("pgup", "ctrl+b"), key.WithHelp("pgup", "page up")),
	PageDn:  key.NewBinding(key.WithKeys("pgdown", "ctrl+f"), key.WithHelp("pgdn", "page dn")),
	Enter:   key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", "resume")),
	Search:  key.NewBinding(key.WithKeys("/", "f"), key.WithHelp("/", "search")),
	Next:    key.NewBinding(key.WithKeys("n"), key.WithHelp("n", "next match")),
	Prev:    key.NewBinding(key.WithKeys("N"), key.WithHelp("N", "prev match")),
	Refresh: key.NewBinding(key.WithKeys("r"), key.WithHelp("r", "refresh")),
	Help:    key.NewBinding(key.WithKeys("?"), key.WithHelp("?", "help")),
	Quit:    key.NewBinding(key.WithKeys("q", "esc"), key.WithHelp("q", "quit")),
	Group:   key.NewBinding(key.WithKeys("g"), key.WithHelp("g", "group toggle")),
	Stats:   key.NewBinding(key.WithKeys("s"), key.WithHelp("s", "stats toggle")),
	OpenDir: key.NewBinding(key.WithKeys("ctrl+o"), key.WithHelp("ctrl+o", "open dir")),
	Export:  key.NewBinding(key.WithKeys("ctrl+e"), key.WithHelp("ctrl+e", "export json")),
	Tab1:    key.NewBinding(key.WithKeys("1"), key.WithHelp("1", "→ Search")),
	Tab2:    key.NewBinding(key.WithKeys("2"), key.WithHelp("2", "→ Recent")),
	Tab3:    key.NewBinding(key.WithKeys("3"), key.WithHelp("3", "→ Stats")),
	Tab4:    key.NewBinding(key.WithKeys("4"), key.WithHelp("4", "→ Costs")),
	Tab5:    key.NewBinding(key.WithKeys("5"), key.WithHelp("5", "→ Timeline")),
	Tab6:    key.NewBinding(key.WithKeys("6"), key.WithHelp("6", "→ Tools")),
	Tab7:    key.NewBinding(key.WithKeys("7"), key.WithHelp("7", "→ Behavior")),
	Tab8:    key.NewBinding(key.WithKeys("8"), key.WithHelp("8", "→ AI")),
}

func keyMatches(msgKey string, b key.Binding) bool {
	for _, k := range b.Keys() {
		if msgKey == k {
			return true
		}
	}
	return false
}
