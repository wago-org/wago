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
		Flags: append([]command.Flag{
			{Name: "experimental", Short: "x", Bool: true, Help: "include experimental previews"},
		}, options.ScopeFlags()...),
		Run: func(ctx *command.Ctx) {
			if len(ctx.Args) != 0 {
				ui.Usage("config list: accepts no arguments")
			}
			request := options.Request{Action: options.List, Experimental: ctx.Bool("experimental")}
			if err := options.ApplyScope(ctx, &request); err != nil {
				ui.Usage("config list: %v", err)
			}
			environment.Configure(request)
		},
	}
}
