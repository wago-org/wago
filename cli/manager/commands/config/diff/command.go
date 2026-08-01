// Package diff defines wago config diff.
package diff

import (
	"github.com/wago-org/wago/cli/internal/command"
	"github.com/wago-org/wago/cli/internal/ui"
	"github.com/wago-org/wago/cli/manager/commands/config/options"
)

func Command(environment options.Environment) *command.Cmd {
	return &command.Cmd{
		Name: "diff", Summary: "show overrides from the inherited configuration", Automation: command.JSONOutput,
		Flags: options.ScopeFlags(),
		Run: func(ctx *command.Ctx) {
			if len(ctx.Args) != 0 {
				ui.Usage("config diff: accepts no arguments")
			}
			request := options.Request{Action: options.Diff}
			if err := options.ApplyScope(ctx, &request); err != nil {
				ui.Usage("config diff: %v", err)
			}
			environment.Configure(request)
		},
	}
}
