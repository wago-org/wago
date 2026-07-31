// Package version defines the wago version command tree.
package version

import (
	"github.com/wago-org/wago/cli/internal/command"
	"github.com/wago-org/wago/cli/manager/commands/version/current"
	"github.com/wago-org/wago/cli/manager/commands/version/install"
	listcmd "github.com/wago-org/wago/cli/manager/commands/version/list"
	switchcmd "github.com/wago-org/wago/cli/manager/commands/version/switch"
	"github.com/wago-org/wago/cli/manager/commands/version/uninstall"
	updatecmd "github.com/wago-org/wago/cli/manager/commands/version/update"
	"github.com/wago-org/wago/cli/manager/commands/version/which"
)

type Environment interface {
	listcmd.Environment
	current.Environment
	which.Environment
	switchcmd.Environment
	install.Environment
	updatecmd.Environment
	uninstall.Environment
}

func Command(environment Environment) *command.Cmd {
	return &command.Cmd{
		Name:    "version",
		Summary: "install, select, update, and remove Wago runtimes",
		Children: []*command.Cmd{
			listcmd.Command(environment),
			current.Command(environment),
			which.Command(environment),
			switchcmd.Command(environment),
			install.Command(environment),
			updatecmd.Command(environment),
			uninstall.Command(environment),
		},
	}
}
