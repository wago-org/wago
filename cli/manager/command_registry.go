package manager

import (
	"context"

	"github.com/wago-org/wago/cli/internal/command"
	"github.com/wago-org/wago/cli/internal/handoff"
	authcmd "github.com/wago-org/wago/cli/manager/commands/auth"
	cachecmd "github.com/wago-org/wago/cli/manager/commands/cache"
	compilecmd "github.com/wago-org/wago/cli/manager/commands/compile"
	configcmd "github.com/wago-org/wago/cli/manager/commands/config"
	initcmd "github.com/wago-org/wago/cli/manager/commands/init"
	plugincmd "github.com/wago-org/wago/cli/manager/commands/plugin"
	pluginadd "github.com/wago-org/wago/cli/manager/commands/plugin/add"
	"github.com/wago-org/wago/cli/manager/commands/plugin/deprecate"
	"github.com/wago-org/wago/cli/manager/commands/plugin/grant"
	"github.com/wago-org/wago/cli/manager/commands/plugin/outdated"
	"github.com/wago-org/wago/cli/manager/commands/plugin/publish"
	"github.com/wago-org/wago/cli/manager/commands/plugin/rebuild"
	pluginremove "github.com/wago-org/wago/cli/manager/commands/plugin/remove"
	"github.com/wago-org/wago/cli/manager/commands/plugin/tree"
	"github.com/wago-org/wago/cli/manager/commands/plugin/unpublish"
	pluginupdate "github.com/wago-org/wago/cli/manager/commands/plugin/update"
	selfcmd "github.com/wago-org/wago/cli/manager/commands/self"
	statuscmd "github.com/wago-org/wago/cli/manager/commands/status"
	updatecmd "github.com/wago-org/wago/cli/manager/commands/update"
	versioncmd "github.com/wago-org/wago/cli/manager/commands/version"
)

func buildCommandRegistry() *command.Cmd {
	return buildCommandRegistryContext(context.Background())
}

func buildCommandRegistryContext(ctx context.Context) *command.Cmd {
	environment := commandEnvironment{ctx: ctx}
	return &command.Cmd{
		Name: "wago",
		Children: []*command.Cmd{
			statuscmd.Command(environment),
			compilecmd.Command(environment),
			updatecmd.Command(environment),
			versioncmd.Command(environment),
			authcmd.Command(environment),
			initcmd.Command(),
			topLevelAddCommand(environment),
			topLevelRemoveCommand(environment),
			managerPluginCommand(environment),
			selfcmd.Command(environment),
			cachecmd.Command(environment),
			configcmd.Command(environment),
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
	return plugincmd.Command("install, update, and publish plugins", []*command.Cmd{
		handoff.PluginListCommand(),
		handoff.PluginInspectCommand(),
		pluginadd.Command(environment),
		pluginremove.Command(environment),
		grant.Command(environment),
		pluginupdate.Command(environment),
		outdated.Command(environment),
		tree.Command(environment),
		rebuild.Command(environment),
		publish.Command(environment),
		unpublish.Command(environment),
		deprecate.Command(environment),
	})
}
