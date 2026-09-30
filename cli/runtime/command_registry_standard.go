//go:build !wago_minimal

package runtime

import (
	"github.com/wago-org/wago/cli/internal/command"
	"github.com/wago-org/wago/cli/internal/profiling"
	runtimecommands "github.com/wago-org/wago/cli/runtime/commands"
	buildcmd "github.com/wago-org/wago/cli/runtime/commands/build"
	modulecmd "github.com/wago-org/wago/cli/runtime/commands/module"
	plugincmd "github.com/wago-org/wago/cli/runtime/commands/plugin"
	runcmd "github.com/wago-org/wago/cli/runtime/commands/run"
	validatecmd "github.com/wago-org/wago/cli/runtime/commands/validate"
)

func buildCommandRegistry() *command.Cmd {
	environment := commandEnvironment{}
	return profiling.Append(runtimecommands.Registry(
		runcmd.Command(environment),
		plugincmd.Command(environment),
		modulecmd.Command(),
		buildcmd.Command(environment),
		validatecmd.Command(),
	))
}
