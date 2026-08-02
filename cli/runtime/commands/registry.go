// Package commands assembles the runtime-owned command registry.
package commands

import "github.com/wago-org/wago/cli/internal/command"

// Registry returns a runtime root with commands in help-display order.
func Registry(children ...*command.Cmd) *command.Cmd {
	return &command.Cmd{Name: "wago", Children: children}
}
