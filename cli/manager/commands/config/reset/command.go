package reset

import (
	"github.com/wago-org/wago/cli/internal/command"
	"github.com/wago-org/wago/cli/internal/ui"
	"github.com/wago-org/wago/cli/manager/commands/config/options"
)

func Command(environment options.Environment) *command.Cmd {
	return &command.Cmd{
		Name: "reset", Summary: "restore built-in defaults", Args: "[setting]", Automation: command.JSONOutput | command.DryRun,
		Flags: []command.Flag{{Name: "all", Short: "a", Bool: true, Help: "reset every setting"}},
		Run: func(ctx *command.Ctx) {
			key := ctx.Optional("[setting]")
			if key == "" && !ctx.Bool("all") {
				ui.Usage("config reset: provide <setting> or --all")
			}
			if key != "" && ctx.Bool("all") {
				ui.Usage("config reset: choose <setting> or --all")
			}
			environment.Configure(options.Request{Action: options.Reset, Key: key, All: ctx.Bool("all")})
		},
	}
}
