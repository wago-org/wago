//go:build wago_minimal

package runtime

import (
	"github.com/wago-org/wago/cli/internal/command"
	runtimecommands "github.com/wago-org/wago/cli/runtime/commands"
	runcmd "github.com/wago-org/wago/cli/runtime/commands/run"
)

func buildCommandRegistry() *command.Cmd {
	return runtimecommands.Registry(runcmd.Command(commandEnvironment{}))
}
