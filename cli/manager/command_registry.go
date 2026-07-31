package manager

import (
	"github.com/wago-org/wago/cli/internal/command"
	"github.com/wago-org/wago/cli/internal/handoff"
	authcmd "github.com/wago-org/wago/cli/manager/commands/auth"
	initcmd "github.com/wago-org/wago/cli/manager/commands/init"
	plugincmd "github.com/wago-org/wago/cli/manager/commands/plugin"
	pluginadd "github.com/wago-org/wago/cli/manager/commands/plugin/add"
	"github.com/wago-org/wago/cli/manager/commands/plugin/deprecate"
	"github.com/wago-org/wago/cli/manager/commands/plugin/grant"
	"github.com/wago-org/wago/cli/manager/commands/plugin/publish"
	pluginremove "github.com/wago-org/wago/cli/manager/commands/plugin/remove"
	"github.com/wago-org/wago/cli/manager/commands/plugin/unpublish"
	pluginupdate "github.com/wago-org/wago/cli/manager/commands/plugin/update"
	selfcmd "github.com/wago-org/wago/cli/manager/commands/self"
	versioncmd "github.com/wago-org/wago/cli/manager/commands/version"
)

func buildCommandRegistry() *command.Cmd {
	environment := commandEnvironment{}
	return &command.Cmd{
		Name: "wago",
		Children: []*command.Cmd{
			versioncmd.Command(environment),
			authcmd.Command(environment),
			initcmd.Command(),
			topLevelAddCommand(environment),
			topLevelRemoveCommand(environment),
			managerPluginCommand(environment),
			selfcmd.Command(environment),
		},
	}
}

func topLevelAddCommand(environment commandEnvironment) *command.Cmd {
	return pluginadd.Command(environment)
}

func topLevelRemoveCommand(environment commandEnvironment) *command.Cmd {
	cmd := pluginremove.Command(environment)
	cmd.Name = "rm"
	cmd.Aliases = nil
	return cmd
}

func managerPluginCommand(environment commandEnvironment) *command.Cmd {
	return plugincmd.Command("add, remove, inspect, update, and publish plugins", []*command.Cmd{
		handoff.PluginListCommand(),
		handoff.PluginInspectCommand(),
		pluginadd.Command(environment),
		pluginremove.Command(environment),
		grant.Command(environment),
		pluginupdate.Command(environment),
		publish.Command(environment),
		unpublish.Command(environment),
		deprecate.Command(environment),
	})
}
