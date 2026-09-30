//go:build wago_minimal

package runtime

import (
	"github.com/wago-org/wago/cli/internal/command"
	"github.com/wago-org/wago/cli/internal/profiling"
	runtimecommands "github.com/wago-org/wago/cli/runtime/commands"
	runcmd "github.com/wago-org/wago/cli/runtime/commands/run"
)

func buildCommandRegistry() *command.Cmd {
	return profiling.Append(runtimecommands.Registry(runcmd.Command(commandEnvironment{})))
}
