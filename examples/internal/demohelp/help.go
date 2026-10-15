// Package demohelp connects example help keys to native and browser controls.
package demohelp

import (
	"fmt"
	"os"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
	booba "github.com/NimbleMarkets/go-booba"
	"github.com/NimbleMarkets/go-booba/actions"
	"github.com/NimbleMarkets/go-booba/actions/bubblehelp"
)

// Demo is implemented by interactive examples that publish key bindings and
// a status summary to both the terminal footer and the browser action bar.
type Demo interface {
	tea.Model
	Help() []key.Binding
	Summary() string
}

type helpKeys []key.Binding

func (k helpKeys) ShortHelp() []key.Binding  { return k }
func (k helpKeys) FullHelp() [][]key.Binding { return [][]key.Binding{k} }

// Key constructs a key.Binding with the first key shown in the help label.
func Key(label string, keys ...string) key.Binding {
	return key.NewBinding(key.WithKeys(keys...), key.WithHelp(keys[0], label))
}

// Quit reports whether msg is a standard quit key press (q or ctrl+c).
func Quit(msg tea.KeyMsg) bool {
	return msg.String() == "q" || msg.String() == "ctrl+c"
}

// Main wraps m with bubblehelp action publishing and runs it via booba.Run.
func Main(m Demo) {
	wrapped, err := bubblehelp.NewModel(m, func(inner tea.Model) bubblehelp.Description {
		d := inner.(Demo)
		return bubblehelp.Description{
			Context: "ntpdf-demo",
			Keys:    helpKeys(d.Help()),
			Status:  actions.ProgramStatus{App: "ntpdf", Summary: d.Summary()},
		}
	}, bubblehelp.ModelOptions{
		Adapter: bubblehelp.Options{
			Customize: func(_ key.Binding, a *actions.Action) error {
				first := 0
				a.Preferred = &first
				return nil
			},
		},
	})
	if err == nil {
		err = booba.Run(wrapped)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
