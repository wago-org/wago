package update

import (
	"errors"

	"github.com/wago-org/wago/cli/internal/tui"
)

const (
	managerSelection = "Manager"
	runtimeSelection = "Runtime"
	pluginsSelection = "Plugins"
)

func ComponentPicker() *tui.MultiSelect {
	return &tui.MultiSelect{
		Title:  "Choose what to update",
		Prompt: "↑/↓ move · space toggle · a toggle all · enter/→ update · esc cancel",
		Items: []tui.SelectItem{
			{Label: managerSelection, Description: "Wago CLI", On: true},
			{Label: runtimeSelection, Description: "Active Wago runtime", On: true},
			{Label: pluginsSelection, Description: "Enabled packages", On: true},
		},
	}
}

func SelectComponents() (Components, bool, error) {
	if !tui.StdinIsTTY() {
		return Components{}, false, errors.New("interactive selection needs a terminal; choose manager, runtime, plugins, or --all")
	}
	picker := ComponentPicker()
	submitted, cancelled := tui.Run(picker)
	if !submitted || cancelled {
		return Components{}, false, nil
	}
	components := Components{}
	for _, item := range picker.Items {
		if !item.On {
			continue
		}
		switch item.Label {
		case managerSelection:
			components.Manager = true
		case runtimeSelection:
			components.Runtime = true
		case pluginsSelection:
			components.Plugins = true
		}
	}
	return components, true, nil
}
