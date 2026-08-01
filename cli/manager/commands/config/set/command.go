package set

import (
	"github.com/wago-org/wago/cli/internal/command"
	"github.com/wago-org/wago/cli/internal/ui"
	"github.com/wago-org/wago/cli/manager/commands/config/options"
)

func Command(environment options.Environment) *command.Cmd {
	return &command.Cmd{
		Name: "set", Summary: "change one default", Args: "<setting> <value>", Automation: command.JSONOutput | command.DryRun,
		Run: func(ctx *command.Ctx) {
			if len(ctx.Args) != 2 {
				ui.Usage("config set: need <setting> <value>")
			}
			environment.Configure(options.Request{Action: options.Set, Key: ctx.Args[0], Value: ctx.Args[1]})
		},
	}
}
