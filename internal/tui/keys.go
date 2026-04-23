package tui

import "github.com/charmbracelet/bubbles/key"

type keyMap struct {
	Quit      key.Binding
	Left      key.Binding
	Right     key.Binding
	Up        key.Binding
	Down      key.Binding
	Enter     key.Binding
	Esc       key.Binding
	Tab       key.Binding
	Add       key.Binding
	Status    key.Binding
	Assign    key.Binding
	Help      key.Binding
	Zero      key.Binding
	One       key.Binding
	Two       key.Binding
	Three     key.Binding
	Four      key.Binding
	Delete    key.Binding
	MoveLeft  key.Binding
	MoveRight key.Binding
	Edit      key.Binding
	Zoom      key.Binding
	Unzoom    key.Binding
	PanelNext key.Binding
	PanelPrev key.Binding
	Archive   key.Binding
	Layout    key.Binding
}

var keys = keyMap{
	Quit:      key.NewBinding(key.WithKeys("q", "ctrl+c"), key.WithHelp("q", "quit")),
	Left:      key.NewBinding(key.WithKeys("h", "left"), key.WithHelp("h/l", "navigate")),
	Right:     key.NewBinding(key.WithKeys("l", "right"), key.WithHelp("l", "right")),
	Up:        key.NewBinding(key.WithKeys("k", "up"), key.WithHelp("j/k", "up/down")),
	Down:      key.NewBinding(key.WithKeys("j", "down"), key.WithHelp("j", "down")),
	Enter:     key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", "select")),
	Esc:       key.NewBinding(key.WithKeys("esc"), key.WithHelp("esc", "back")),
	Tab:       key.NewBinding(key.WithKeys("tab"), key.WithHelp("tab", "next field")),
	Add:       key.NewBinding(key.WithKeys("a"), key.WithHelp("a", "add")),
	Status:    key.NewBinding(key.WithKeys("s"), key.WithHelp("s", "status")),
	Assign:    key.NewBinding(key.WithKeys("A"), key.WithHelp("A", "assign")),
	Help:      key.NewBinding(key.WithKeys("?"), key.WithHelp("?", "help")),
	Zero:      key.NewBinding(key.WithKeys("0"), key.WithHelp("0", "Backlog")),
	One:       key.NewBinding(key.WithKeys("1"), key.WithHelp("1", "Todo")),
	Two:       key.NewBinding(key.WithKeys("2"), key.WithHelp("2", "Doing")),
	Three:     key.NewBinding(key.WithKeys("3"), key.WithHelp("3", "Done")),
	Four:      key.NewBinding(key.WithKeys("4"), key.WithHelp("4", "Hold")),
	Delete:    key.NewBinding(key.WithKeys("d"), key.WithHelp("d", "delete")),
	MoveLeft:  key.NewBinding(key.WithKeys("H"), key.WithHelp("H", "move left")),
	MoveRight: key.NewBinding(key.WithKeys("L"), key.WithHelp("L", "move right")),
	Edit:      key.NewBinding(key.WithKeys("e"), key.WithHelp("e", "edit")),
	Zoom:      key.NewBinding(key.WithKeys("+"), key.WithHelp("+", "zoom in")),
	Unzoom:    key.NewBinding(key.WithKeys("-"), key.WithHelp("-", "zoom out")),
	PanelNext: key.NewBinding(key.WithKeys("]"), key.WithHelp("]", "next panel")),
	PanelPrev: key.NewBinding(key.WithKeys("["), key.WithHelp("[", "prev panel")),
	Archive:   key.NewBinding(key.WithKeys("x"), key.WithHelp("x", "archive")),
	Layout:    key.NewBinding(key.WithKeys("v"), key.WithHelp("v", "toggle layout")),
}
