package list

import (
	"github.com/wago-org/wago/cli/internal/command"
	"github.com/wago-org/wago/cli/internal/ui"
	"github.com/wago-org/wago/cli/manager/commands/config/options"
)

func Command(environment options.Environment) *command.Cmd {
	return &command.Cmd{
		Name: "list", Aliases: []string{"ls"}, Summary: "list configured defaults",
		Automation: command.JSONOutput,
		Flags:      []command.Flag{{Name: "experimental", Short: "x", Bool: true, Help: "include experimental previews"}},
		Run: func(ctx *command.Ctx) {
			if len(ctx.Args) != 0 {
				ui.Usage("config list: accepts no arguments")
			}
			environment.Configure(options.Request{Action: options.List, Experimental: ctx.Bool("experimental")})
		},
	}
}
