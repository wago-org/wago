package get

import (
	"github.com/wago-org/wago/cli/internal/command"
	"github.com/wago-org/wago/cli/manager/commands/config/options"
)

func Command(environment options.Environment) *command.Cmd {
	return &command.Cmd{
		Name: "get", Summary: "print one configured default", Args: "<setting>", Automation: command.JSONOutput,
		Run: func(ctx *command.Ctx) {
			environment.Configure(options.Request{Action: options.Get, Key: ctx.One("<setting>")})
		},
	}
}
