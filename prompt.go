package main

import (
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

// A prompt is a line typed in the footer (search, a comment): it takes every
// key until Enter or Esc. A new one is a prompt value set on m.asking.
type prompt struct {
	label  string
	in     lineInput
	note   func() string // dim text after the input, or nil
	change func(string)  // after each edit, or nil
	done   func(string) tea.Cmd
	cancel func() // Esc, or nil
}

func (m *model) promptKey(msg tea.KeyPressMsg) tea.Cmd {
	p := m.asking
	switch k := msg.String(); k {
	case "esc":
		m.asking = nil
		if p.cancel != nil {
			p.cancel()
		}
	case "enter":
		m.asking = nil
		return p.done(p.in.String())
	default:
		if p.in.key(k, msg.Text) && p.change != nil {
			p.change(p.in.String())
		}
	}
	return nil
}

func (m *model) promptLine() string {
	p := m.asking
	t := m.theme
	note := ""
	if p.note != nil {
		note = p.note()
	}
	label := " " + p.label
	room := max(1, m.width-ansi.StringWidth(label)-ansi.StringWidth(note)-1)
	return lipgloss.NewStyle().Foreground(t.Accent).Render(label) + p.in.view(room) +
		lipgloss.NewStyle().Foreground(t.Dim).Render(note)
}
