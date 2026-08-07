// Package module defines the wago module command tree.
package module

import (
	"github.com/wago-org/wago/cli/internal/command"
	"github.com/wago-org/wago/cli/runtime/commands/module/capabilities"
	"github.com/wago-org/wago/cli/runtime/commands/module/exports"
	"github.com/wago-org/wago/cli/runtime/commands/module/imports"
)

func Command() *command.Cmd {
	return &command.Cmd{
		Name: "module", Aliases: []string{"mod"},
		Summary: "inspect a module's imports, exports, and required capabilities",
		Children: []*command.Cmd{
			imports.Command(),
			exports.Command(),
			capabilities.Command(),
		},
	}
}
